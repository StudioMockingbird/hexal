//go:build c23

package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// RFC 0044 smoke tests: byte/rune literals, the RuneCursor, the Strand
// surface, and the from_bytes/from_runes constructors generate C that gcc
// accepts, and a compiled run computes the expected values.

func TestGeneratedTextConformanceRuns(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not installed")
	}
	source := "fun demo(h: Heap): Bool\n    byte: UInt8 = b'\\xFF'\n    byte_ok: Bool = byte == 255\n    letter: Rune = '\\u{00E9}'\n    crab: Rune = '\\u{1F980}'\n    rune_ok: Bool = letter == 233 and crab == 129408\n    text: String = \"caf\\u{00E9} \\u{1F980}\"\n    count: Size = text.length()\n    length_ok: Bool = count == 6\n    first: Rune = text.at(0)\n    accented: Rune = text.at(3)\n    index_ok: Bool = first == 99 and accented == 233\n    cursor: RuneCursor = text.rune_cursor()\n    mut seen: Int32 = 0\n    while cursor.has_next() do\n        value: Rune = cursor.next()\n        seen = seen + 1\n    end\n    cursor_ok: Bool = seen == 6\n    label: Strand = \"Seawitch\"\n    strand_ok: Bool = label.length() == 8 and label[0] == 83\n    runes: Array<Rune, 2> = [letter, crab]\n    view: View<Rune> = runes.slice(0, 2)\n    encoded: String = String.from_runes(h, view)\n    encoded_ok: Bool = encoded.length() == 2 and encoded.at(0) == 233\n    encoded.free(h)\n    return byte_ok and rune_ok and length_ok and index_ok and cursor_ok and strand_ok and encoded_ok\nend\nprint(demo(Heap.new()))\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	dir := t.TempDir()
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
