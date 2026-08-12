//go:build c23

package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// RFC 0040 smoke tests: a File program generates C that gcc accepts, and a
// compiled run round-trips text and bytes through a real host file. stdio
// needs no C23 threads, so these run on every gcc toolchain.

func TestGeneratedFileIORuns(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not installed")
	}
	source := "fun write_report(path: String, report: String): Nil | Error\n    file: File = try File.open(path, FileMode.Write)\n    defer file.close()\n    result: Nil | Error = try file.write_text(report)\n    flushed: Nil | Error = try file.flush()\n    return nil\nend\nfun read_report(h: Heap, path: String): String | Error\n    file: File = try File.open(path, FileMode.Read)\n    defer file.close()\n    text: String = try file.read_text(h)\n    return text\nend\nfun append_bytes(path: String, packet: View<Byte>): Nil | Error\n    file: File = try File.open(path, FileMode.Append)\n    defer file.close()\n    written: Nil | Error = try file.write(packet)\n    return nil\nend\nfun demo(h: Heap, path: String): Bool | Error\n    written: Nil | Error = try write_report(path, \"hello world\\n\")\n    text: String = try read_report(h, path)\n    matches: Bool = text == \"hello world\\n\"\n    trailer: Array<UInt8, 3> = [33, 34, 35]\n    view: View<UInt8> = trailer.slice(0, 3)\n    appended: Nil | Error = try append_bytes(path, view)\n    all: String = try read_report(h, path)\n    complete: Bool = all == \"hello world\\n!\\\"#\"\n    return matches and complete\nend\nfun print_demo(path: String): Bool | Error\n    ok: Bool = try demo(Heap.new(), path)\n    return ok\nend\nfun run(path: String): Bool | Error\n    result: Bool = try print_demo(path)\n    return result\nend\nfun final(path: String): Bool\n    outcome: Bool | Error = run(path)\n    if outcome is Bool\n        return outcome\n    end\n    return false\nend\nprint(final(\"%s\"))\n"
	dir := t.TempDir()
	target := filepath.ToSlash(filepath.Join(dir, "sw_io_target.txt"))
	source = strings.Replace(source, "%s", target, 1)
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	mainC := filepath.Join(dir, "main.c")
	mainH := filepath.Join(dir, "main.h")
	if err := os.WriteFile(mainC, []byte(result.MainC), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainH, []byte(result.MainH), 0644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "main.exe")
	command := exec.Command("gcc", "-std=c23", "-Wall", "-Wextra", mainC, "-o", exe)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc rejected generated C: %v\n%s", err, output)
	}
	run, err := exec.Command(exe).CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, run)
	}
	normalized := strings.ReplaceAll(string(run), "\r\n", "\n")
	if normalized != "true" {
		t.Fatalf("program output = %q, want %q", normalized, "true")
	}
}
