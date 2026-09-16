package integration

// The embedded source standard library: std/ascii compiles from the compiler
// binary with its own stdlib artifact path, source provenance, and symbol
// owner encoding.

import (
	"strings"
	"testing"

	"hexal/compiler"
	"hexal/stdlib"
)

func TestAsciiSourceModuleCompiles(t *testing.T) {
	source := "import\n  Ascii from std.ascii\nend\n" +
		"digit: Bool := Ascii.is_digit(48)\n" +
		"lower: Byte := Ascii.to_lower(65)\n"
	result := assertCompiles(t, source)
	header := moduleFile(t, result, "stdlib/ascii.h")
	for _, want := range []string{
		"#ifndef HEX_MODULE_s5_ascii_H",
		"bool hex_f_s5_ascii_is_digit(uint8_t);",
		"bool hex_f_s5_ascii_is_alpha(uint8_t);",
		"bool hex_f_s5_ascii_is_space(uint8_t);",
		"uint8_t hex_f_s5_ascii_to_lower(uint8_t);",
		"uint8_t hex_f_s5_ascii_to_upper(uint8_t);",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("stdlib/ascii.h lacks %q:\n%s", want, header)
		}
	}
	body := moduleFile(t, result, "stdlib/ascii.c")
	if !strings.Contains(body, "#include \"stdlib/ascii.h\"") {
		t.Errorf("stdlib/ascii.c must include its own header:\n%s", body)
	}
	if !strings.Contains(body, "#line 4 \"stdlib/std/ascii.hex\"") {
		t.Errorf("stdlib/ascii.c must map #line to its stdlib source key:\n%s", body)
	}
	if !strings.Contains(rootH(t, result), "bool hex_f_s5_ascii_is_digit(uint8_t);") {
		t.Errorf("the importing module header lacks the foreign prototype:\n%s", rootH(t, result))
	}
}

func TestAsciiNamedFunctionsMatchSurface(t *testing.T) {
	source := "import\n  Ascii from std.ascii\nend\n" +
		"d: Bool := Ascii.is_digit(48)\n" +
		"a: Bool := Ascii.is_alpha(65)\n" +
		"s: Bool := Ascii.is_space(32)\n" +
		"l: Byte := Ascii.to_lower(65)\n" +
		"u: Byte := Ascii.to_upper(97)\n"
	assertCompiles(t, source)
	// Unknown operations stay fail-closed.
	assertRejects(t, "import\n  Ascii from std.ascii\nend\nx := Ascii.is_upper(65)\n", "is_upper")
}

// The std/ logical-key prefix is reserved, so a user module can never claim a
// stdlib canonical identity.
func TestStdKeyPrefixReserved(t *testing.T) {
	const want = `the "std" path prefix is reserved for the standard library`
	entry := compiler.Compile(map[string]string{"std/fs.hex": "value: Int32 := 1\n"}, "std/fs.hex", compiler.Project{})
	if entry.ExitCode != compiler.ExitFailure || len(entry.Stderr) == 0 || !strings.Contains(entry.Stderr[0], want) {
		t.Fatalf("std/fs.hex entrypoint = %#v, want the reserved-prefix diagnostic", entry.Stderr)
	}
	imported := compiler.Compile(map[string]string{
		"app.hex":       "import\n    S from \"./std/thing\"\nend\nvalue: Int32 := 1\n",
		"std/thing.hex": "value: Int32 := 1\nexport\n    value\nend\n",
	}, "app.hex", compiler.Project{})
	if imported.ExitCode != compiler.ExitFailure || len(imported.Stderr) == 0 || !strings.Contains(strings.Join(imported.Stderr, "\n"), want) {
		t.Fatalf("relative std/ import = %#v, want the reserved-prefix diagnostic", imported.Stderr)
	}
}

// stdlib.Sources returns a fresh copy; mutating one does not change a later
// compilation's stdlib.
func TestStdlibSourcesAreFresh(t *testing.T) {
	first := stdlib.Sources()
	first["stdlib/std/ascii.hex"] = "corrupted"
	second := stdlib.Sources()
	if second["stdlib/std/ascii.hex"] == "corrupted" {
		t.Fatal("mutating a Sources() copy changed a later call's stdlib")
	}
	assertCompiles(t, "import\n  Ascii from std.ascii\nend\nd: Bool := Ascii.is_digit(48)\n")
}
