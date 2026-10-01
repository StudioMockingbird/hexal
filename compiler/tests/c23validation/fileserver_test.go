//go:build c23

package c23validation

// Static file server fixtures. Each program runs in a directory this file
// builds: the served files carry one fixed modification time so validators are
// exact, a directory link points outside the root, and the programs answer a
// stdin handshake so the test can change the filesystem, or inspect the
// process, at a moment the program names.

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/http_files.hex
var httpFilesBody string

//go:embed testdata/http_files.stdout
var httpFilesStdout string

//go:embed testdata/http_files_transfer.hex
var httpFilesTransferBody string

//go:embed testdata/http_files_transfer.stdout
var httpFilesTransferStdout string

// fileServerModified is the one modification time every served file carries;
// the expected ETag and Last-Modified in the stdout files derive from it.
var fileServerModified = time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)

// bigFileSize exceeds the sum of loopback send and receive buffers on every
// qualified host, so a reader that stops reading backs the transfer up.
const bigFileSize = 32 << 20

// directoryLink makes link a directory link to target: a symbolic link on
// POSIX, a junction on Windows, where unprivileged symbolic links do not
// exist. Both are reparse or link forms the file server must refuse to enter.
func directoryLink(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if err != nil {
			t.Fatalf("mklink /J failed: %v\n%s", err, output)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink failed: %v", err)
	}
}

// buildFileTree writes files (relative path to content) under dir and gives
// each the fixed modification time.
func buildFileTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, fileServerModified, fileServerModified); err != nil {
			t.Fatal(err)
		}
	}
}

// runWithHandshake runs exe in dir. When the program writes a line "ready" it
// calls onReady with the process id and the word after "ready" (empty when
// absent) while the program waits on stdin, then releases it. The returned
// stdout has the handshake lines removed and CRLF normalized; stderr must be
// empty and the exit status zero.
func runWithHandshake(t *testing.T, exe, dir string, onReady func(pid int, step string)) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runProcessTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, exe)
	command.Dir = dir
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	reader := bufio.NewReader(stdout)
	for {
		line, readErr := reader.ReadString('\n')
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "ready" {
			step := ""
			if len(fields) > 1 {
				step = fields[1]
			}
			onReady(command.Process.Pid, step)
			if _, err := io.WriteString(stdin, "go\n"); err != nil {
				t.Fatal(err)
			}
		} else {
			output.WriteString(line)
		}
		if readErr != nil {
			break
		}
	}
	waitErr := command.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("program did not exit within %s; stdout so far:\n%s", runProcessTimeout, output.String())
	}
	if waitErr != nil || stderr.Len() != 0 {
		t.Fatalf("program failed: %v\nstderr: %q\nstdout:\n%s", waitErr, stderr.String(), output.String())
	}
	return strings.ReplaceAll(output.String(), "\r\n", "\n")
}

// streamSectionsForHost adapts the expected output to the host's file names:
// a colon is an alternate-stream separator the file server refuses on Windows,
// and an ordinary byte on POSIX, where the same two requests name missing
// files.
func streamSectionsForHost(t *testing.T, want string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return want
	}
	const forbidden = "HTTP/1.1 403 Forbidden\nContent-Type: text/plain; charset=utf-8\nConnection: close\nContent-Length: 10\n\nForbidden\n"
	const missing = "HTTP/1.1 404 Not Found\nContent-Type: text/plain; charset=utf-8\nConnection: close\nContent-Length: 10\n\nNot Found\n"
	for _, label := range []string{"== alternate data stream\n", "== alternate stream default\n"} {
		if !strings.Contains(want, label+forbidden) {
			t.Fatalf("expected output lacks the %q section", label)
		}
		want = strings.Replace(want, label+forbidden, label+missing, 1)
	}
	return want
}

// expectOutput compares got with the expected text after LF normalization and
// names the first differing line.
func expectOutput(t *testing.T, got, want string) {
	t.Helper()
	want = strings.ReplaceAll(want, "\r\n", "\n")
	if got == want {
		return
	}
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
	for index := 0; index < len(gotLines) && index < len(wantLines); index++ {
		if gotLines[index] != wantLines[index] {
			t.Fatalf("output differs at line %d:\n got: %q\nwant: %q", index+1, gotLines[index], wantLines[index])
		}
	}
	t.Fatalf("output has %d lines, want %d", len(gotLines), len(wantLines))
}

// requireOnlyRootDescriptor checks, on Linux, that the process holds no
// descriptor on a served file once every transfer has ended, and exactly one
// on the served root: the retained root doubles as the control showing the
// listing sees the process. Other hosts have no descriptor listing to read.
func requireOnlyRootDescriptor(t *testing.T, pid int) {
	t.Helper()
	if runtime.GOOS != "linux" {
		return
	}
	descriptors, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		t.Errorf("listing descriptors: %v", err)
		return
	}
	roots := 0
	for _, entry := range descriptors {
		target, err := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", pid, entry.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(target, "/site/") {
			t.Errorf("descriptor %s still open on %s after every transfer ended", entry.Name(), target)
		}
		if strings.HasSuffix(target, "/site") {
			roots++
		}
	}
	if roots != 1 {
		t.Errorf("%d descriptors on the served root, want the one retained root", roots)
	}
}

func TestFileServerServesAndContains(t *testing.T) {
	result := assertCompiles(t, httpCommonSource+httpFilesBody)
	exe := buildGeneratedC(t, clangToolchain(t), result, t.TempDir())

	dir := t.TempDir()
	files := map[string]string{
		"site/index.html":       "<h1>home</h1>\n",
		"site/a.txt":            "hello world",
		"site/digits.bin":       "0123456789",
		"site/empty.txt":        "",
		"site/exact.txt":        "from disk\n",
		"site/.hidden":          "secret",
		"site/.well/ok.txt":     "dot dir file\n",
		"site/mime.d/plain":     "x",
		"site/bare/x.txt":       "x",
		"site/sub/index.html":   "sub index\n",
		"site/deep/index.html":  "outer deep\n",
		"site/deepx/index.html": "deepx index\n",
		"inner/index.html":      "inner index\n",
		"site-other/secret.txt": "sibling secret\n",
		"outside/secret.txt":    "outside secret\n",
	}
	for _, name := range []string{"x.html", "x.css", "x.js", "x.json", "x.png", "x.jpg", "x.jpeg", "x.gif", "x.svg",
		"x.woff", "x.woff2", "x.ttf", "x.mp4", "x.webm", "x.pdf", "x.zip", "x.txt", "x.xml", "x.xyz", "UPPER.HTML", "noext"} {
		files["site/mime/"+name] = "x"
	}
	buildFileTree(t, dir, files)
	directoryLink(t, filepath.Join(dir, "outside"), filepath.Join(dir, "site", "escape"))

	got := runWithHandshake(t, exe, dir, func(int, string) {
		// Replace the root's pathname with a link to a different tree while
		// the server holds the original directory open.
		if err := os.Rename(filepath.Join(dir, "site"), filepath.Join(dir, "site-moved")); err != nil {
			t.Fatalf("renaming the served root failed: %v", err)
		}
		directoryLink(t, filepath.Join(dir, "outside"), filepath.Join(dir, "site"))
	})
	expectOutput(t, got, streamSectionsForHost(t, strings.ReplaceAll(httpFilesStdout, "\r\n", "\n")))
}

func TestFileServerBoundedTransfer(t *testing.T) {
	result := assertCompiles(t, httpCommonSource+httpFilesTransferBody)
	exe := buildGeneratedC(t, clangToolchain(t), result, t.TempDir())

	dir := t.TempDir()
	// The pattern is letters so a ranged read prints as text.
	big := make([]byte, bigFileSize)
	for index := range big {
		big[index] = byte('a' + index%26)
	}
	buildFileTree(t, dir, map[string]string{"site/big.bin": string(big), "site/small.txt": "small\n"})
	// The two files changed mid-transfer only need a length.
	for _, name := range []string{"vanish.bin", "shrink.bin"} {
		path := filepath.Join(dir, "site", name)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(path, bigFileSize); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, fileServerModified, fileServerModified); err != nil {
			t.Fatal(err)
		}
	}

	got := runWithHandshake(t, exe, dir, func(pid int, step string) {
		switch step {
		case "remove":
			// A transfer in flight keeps reading the file it opened.
			if err := os.Remove(filepath.Join(dir, "site", "vanish.bin")); err != nil {
				t.Errorf("removing the served file: %v", err)
			}
		case "truncate":
			// The announced length can no longer be delivered.
			if err := os.Truncate(filepath.Join(dir, "site", "shrink.bin"), 0); err != nil {
				t.Errorf("truncating the served file: %v", err)
			}
		default:
			requireOnlyRootDescriptor(t, pid)
		}
	})
	expectOutput(t, got, httpFilesTransferStdout)
}
