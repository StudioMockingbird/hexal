//go:build c23

package tests

import (
	"path/filepath"
	"strings"
	"testing"
)

// RFC 0040 smoke tests: a File program generates C that gcc accepts, and a
// compiled run round-trips text and bytes through a real host file. stdio
// needs no C23 threads, so these run on every gcc toolchain.

func TestGeneratedFileIORuns(t *testing.T) {
	source := "fun write_report(path: String, report: String): Nil | Error\n    file: File = try File.open(path, FileMode.Write)\n    defer file.close()\n    result: Nil | Error = try file.write_text(report)\n    flushed: Nil | Error = try file.flush()\n    return nil\nend\nfun read_report(h: Heap, path: String): String | Error\n    file: File = try File.open(path, FileMode.Read)\n    defer file.close()\n    text: String = try file.read_text(h)\n    return text\nend\nfun append_bytes(path: String, packet: View<Byte>): Nil | Error\n    file: File = try File.open(path, FileMode.Append)\n    defer file.close()\n    written: Nil | Error = try file.write(packet)\n    return nil\nend\nfun demo(h: Heap, path: String): Bool | Error\n    written: Nil | Error = try write_report(path, \"hello world\\n\")\n    text: String = try read_report(h, path)\n    matches: Bool = text == \"hello world\\n\"\n    trailer: Array<UInt8, 3> = [33, 34, 35]\n    view: View<UInt8> = trailer.slice(0, 3)\n    appended: Nil | Error = try append_bytes(path, view)\n    all: String = try read_report(h, path)\n    complete: Bool = all == \"hello world\\n!\\\"#\"\n    return matches and complete\nend\nfun print_demo(path: String): Bool | Error\n    ok: Bool = try demo(Heap.new(), path)\n    return ok\nend\nfun run(path: String): Bool | Error\n    result: Bool = try print_demo(path)\n    return result\nend\nfun final(path: String): Bool\n    outcome: Bool | Error = run(path)\n    if outcome is Bool\n        return outcome\n    end\n    return false\nend\nprint(final(\"%s\"))\n"
	target := filepath.ToSlash(filepath.Join(t.TempDir(), "hex_io_target.txt"))
	source = strings.Replace(source, "%s", target, 1)
	normalized := runGeneratedC(t, assertCompiles(t, source))
	if normalized != "true" {
		t.Fatalf("program output = %q, want %q", normalized, "true")
	}
}

// RFC 0040 runtime conformance: opening a missing file and using a File in
// the wrong mode surface as Error, never as a trap or silent success.
func TestGeneratedFileErrorPathsRun(t *testing.T) {
	target := filepath.ToSlash(filepath.Join(t.TempDir(), "hex_io_missing.txt"))
	missing := "fun demo(path: String): Bool\n    outcome: File | Error = File.open(path, FileMode.Read)\n    if outcome is File\n        return false\n    end\n    return true\nend\nprint(demo(\"%s\"))\n"
	normalized := runGeneratedC(t, assertCompiles(t, strings.Replace(missing, "%s", target, 1)))
	if normalized != "true" {
		t.Fatalf("program output = %q, want %q", normalized, "true")
	}
	modes := "fun read_from_write(path: String): Bool | Error\n    file: File = try File.open(path, FileMode.Write)\n    defer file.close()\n    outcome: String | Error = file.read_text(Heap.new())\n    if outcome is String\n        return false\n    end\n    return true\nend\nfun write_to_read(path: String): Bool | Error\n    file: File = try File.open(path, FileMode.Read)\n    defer file.close()\n    outcome: Nil | Error = file.write_text(\"x\")\n    if outcome == nil\n        return false\n    end\n    return true\nend\nfun demo(path: String): Bool\n    first: Bool | Error = read_from_write(path)\n    second: Bool | Error = write_to_read(path)\n    a: Bool = match first is\n    | Bool then\n        first\n    | Error then\n        false\n    end\n    b: Bool = match second is\n    | Bool then\n        second\n    | Error then\n        false\n    end\n    return a and b\nend\nprint(demo(\"%s\"))\n"
	normalized = runGeneratedC(t, assertCompiles(t, strings.Replace(modes, "%s", target, 1)))
	if normalized != "true" {
		t.Fatalf("program output = %q, want %q", normalized, "true")
	}
}
