package backend

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestArchive builds a synthetic backend archive with the given files
// and returns its path, byte length, and digest.
func writeTestArchive(t *testing.T, name string, files map[string]string) (string, int64, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(out)
	for entryName, content := range files {
		entry, err := writer.Create(entryName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return path, int64(len(raw)), hex.EncodeToString(sum[:])
}

func testRecord(name string, length int64, digest string) LockRecord {
	return LockRecord{Filename: name, Version: "0.16.0", ByteLength: length, SHA256: digest}
}

func TestVerifyArchiveAcceptsExactRecord(t *testing.T) {
	path, length, digest := writeTestArchive(t, "zig-test.zip", map[string]string{"zig.exe": "exe", "lib/libc.h": "h"})
	if err := VerifyArchive(path, testRecord("zig-test.zip", length, digest)); err != nil {
		t.Fatalf("exact record rejected: %v", err)
	}
}

func TestVerifyArchiveRejectsMismatch(t *testing.T) {
	path, length, digest := writeTestArchive(t, "zig-test.zip", map[string]string{"zig.exe": "exe"})
	for _, record := range []LockRecord{
		testRecord("other.zip", length, digest),
		testRecord("zig-test.zip", length+1, digest),
		testRecord("zig-test.zip", length, strings.Repeat("0", 64)),
	} {
		if err := VerifyArchive(path, record); err == nil {
			t.Fatalf("mismatched record accepted: %+v", record)
		}
	}
	if err := VerifyArchive(filepath.Join(t.TempDir(), "missing.zip"), testRecord("missing.zip", 0, "")); err == nil {
		t.Fatal("missing archive accepted")
	}
}

func TestStageVerifiedExtractsAndMarks(t *testing.T) {
	path, length, digest := writeTestArchive(t, "zig-test.zip", map[string]string{"zig.exe": "exe", "lib/libc.h": "h"})
	staging := filepath.Join(t.TempDir(), "staged")
	if err := StageVerified(path, staging, testRecord("zig-test.zip", length, digest)); err != nil {
		t.Fatalf("staging failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "zig.exe")); err != nil {
		t.Fatalf("extracted exe missing: %v", err)
	}
	marker, err := ReadMarker(staging)
	if err != nil {
		t.Fatalf("marker unreadable: %v", err)
	}
	if marker.Version != "0.16.0" || marker.SHA256 != digest {
		t.Fatalf("marker identity wrong: %+v", marker)
	}
}

func TestStageVerifiedRejectsZipSlip(t *testing.T) {
	path, length, digest := writeTestArchive(t, "zig-test.zip", map[string]string{"../escape.txt": "x"})
	staging := filepath.Join(t.TempDir(), "staged")
	if err := StageVerified(path, staging, testRecord("zig-test.zip", length, digest)); err == nil {
		t.Fatal("escaping archive entry accepted")
	}
}

func TestReadMarkerRejectsMissingAndCorrupt(t *testing.T) {
	if _, err := ReadMarker(t.TempDir()); err == nil {
		t.Fatal("missing marker accepted")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, markerName), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadMarker(dir); err == nil {
		t.Fatal("corrupt marker accepted")
	}
}

func TestLibDirPatternMatchesRealZigEnv(t *testing.T) {
	raw, err := os.ReadFile("testdata/zig-env.txt")
	if err != nil {
		t.Fatal(err)
	}
	match := libDirPattern.FindStringSubmatch(string(raw))
	if match == nil || !strings.HasSuffix(match[1], `lib`) {
		t.Fatalf("lib_dir not extracted: %q", match)
	}
}

func TestClangVersionPatternMatchesRealOutput(t *testing.T) {
	raw, err := os.ReadFile("testdata/zig-cc-v.txt")
	if err != nil {
		t.Fatal(err)
	}
	match := clangVersionPattern.FindStringSubmatch(string(raw))
	if match == nil || match[1] != "21.1.0" {
		t.Fatalf("clang version not extracted: %q", match)
	}
}

func TestFetchArchiveRoundTrip(t *testing.T) {
	payload := "archive-bytes"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, payload)
	}))
	defer server.Close()
	destination := filepath.Join(t.TempDir(), "archive.zip")
	if err := FetchArchive(server.URL, destination); err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	raw, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != payload {
		t.Fatalf("fetched %q, want %q", raw, payload)
	}
}

func TestPinnedRecordIsComplete(t *testing.T) {
	record := PinnedZigWindows()
	if record.Filename == "" || record.Version == "" || record.ByteLength <= 0 || len(record.SHA256) != 64 {
		t.Fatalf("pinned record incomplete: %+v", record)
	}
}
