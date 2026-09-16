package parser

import (
	"strings"
	"testing"
)

// A foreign block parses every declaration form, both header forms, and the
// optional exact C spelling attributes.
func TestParseExternBlockForms(t *testing.T) {
	source := "extern c from <raylib.h> do\n" +
		"    type TraceLevel is Int32\n" +
		"    type Vector2 is struct\n" +
		"        mut x: Float32,\n" +
		"        mut y: Float32,\n" +
		"    end\n" +
		"    type Window as \"struct Window\" is opaque\n" +
		"    fun init_window as \"InitWindow\"(\n" +
		"        width: Int32 as \"int\",\n" +
		"        height: Int32 as \"int\",\n" +
		"        title: Ptr<Byte> | Nil as \"const char *\",\n" +
		"    )\n" +
		"    fun window_should_close as \"WindowShouldClose\"(): Bool\n" +
		"    constant log_info as \"LOG_INFO\": TraceLevel\n" +
		"    global mut counter: Int32\n" +
		"end\n"
	program, err := Parse(mustLex(t, source))
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", source, err)
	}
	if len(program.Externs) != 1 {
		t.Fatalf("Externs = %d, want 1", len(program.Externs))
	}
	block := program.Externs[0]
	if block.Header.CHeader != "raylib.h" || !block.Header.System {
		t.Fatalf("header = %+v, want system raylib.h", block.Header)
	}
	if len(block.Declarations) != 7 {
		t.Fatalf("declarations = %d, want 7", len(block.Declarations))
	}
	alias, ok := block.Declarations[0].(ExternType)
	if !ok || alias.Name.Lexeme != "TraceLevel" || alias.Alias == nil {
		t.Fatalf("declaration 0 = %#v, want the TraceLevel alias", block.Declarations[0])
	}
	record, ok := block.Declarations[1].(ExternType)
	if !ok || len(record.Members) != 2 || !record.Members[0].Mutable {
		t.Fatalf("declaration 1 = %#v, want the complete Vector2 record", block.Declarations[1])
	}
	opaque, ok := block.Declarations[2].(ExternType)
	if !ok || !opaque.Opaque || opaque.CName == nil {
		t.Fatalf("declaration 2 = %#v, want the opaque Window record", block.Declarations[2])
	}
	function, ok := block.Declarations[3].(ExternFunction)
	if !ok || function.CName == nil || len(function.Parameters) != 3 || function.Parameters[2].CType == nil {
		t.Fatalf("declaration 3 = %#v, want the init_window foreign function", block.Declarations[3])
	}
	global, ok := block.Declarations[6].(ExternGlobal)
	if !ok || !global.Mutable || global.Name.Lexeme != "counter" {
		t.Fatalf("declaration 6 = %#v, want the mutable counter global", block.Declarations[6])
	}
}

// Quoted headers parse as the quoted form, and multiple leading blocks work.
func TestParseExternBlockQuotedAndMultiple(t *testing.T) {
	source := "extern c from \"vendor/widget.h\" do\n    type Widget as \"struct widget\" is opaque\nend\n" +
		"extern c from <stdio.h> do\n    fun c_puts as \"puts\"(text: Ptr<Byte> | Nil as \"const char *\"): Int32 as \"int\"\nend\n"
	program, err := Parse(mustLex(t, source))
	if err != nil {
		t.Fatalf("Parse error = %v", err)
	}
	if len(program.Externs) != 2 {
		t.Fatalf("Externs = %d, want 2", len(program.Externs))
	}
	if program.Externs[0].Header.System || program.Externs[0].Header.CHeader != "vendor/widget.h" {
		t.Fatalf("first header = %+v, want quoted vendor/widget.h", program.Externs[0].Header)
	}
	if !program.Externs[1].Header.System {
		t.Fatalf("second header = %+v, want system stdio.h", program.Externs[1].Header)
	}
}

func TestParseExternBlockRejections(t *testing.T) {
	for _, testCase := range []struct{ source, want string }{
		{"value: Int32 := 1\nextern c from <x.h> do\nend\n", "extern blocks must precede ordinary top-level items"},
		{"extern c from <x.h> do\n    fun f() do\n    end\nend\n", "unsupported foreign declaration"},
		{"extern c from <x.h> do\n    fun f(x: Int32 as \"int[4]\")\nend\n", "invalid C spelling int[4]"},
		{"extern c from x.h do\nend\n", "foreign declaration requires a C header"},
		{"extern c from <x.h>\nend\n", "expected 'do' after the foreign header"},
	} {
		_, parseErr := Parse(mustLex(t, testCase.source))
		if parseErr == nil || !strings.Contains(parseErr.Error(), testCase.want) {
			t.Errorf("Parse(%q) error = %v, want containing %q", testCase.source, parseErr, testCase.want)
		}
	}
}
