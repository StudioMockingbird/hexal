package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// Classification matrix: identity for one canonical type, direct for the
// proven non-trapping families (integer/Rune to float, Float32 widening,
// domain-fitted integer widening), checked for everything else, with Size
// never classified direct except identity and Size-to-float.
func TestClassifyConversionMatrix(t *testing.T) {
	scalars := []compilerTypes.Type{
		compilerTypes.Int8, compilerTypes.Int16, compilerTypes.Int32, compilerTypes.Int64,
		compilerTypes.UInt8, compilerTypes.UInt16, compilerTypes.UInt32, compilerTypes.UInt64,
		compilerTypes.Rune, compilerTypes.Float32, compilerTypes.Float64, compilerTypes.SizeType,
	}
	// Expected kind per row (source) across the targets in scalars order:
	// I = identity, D = direct, C = checked.
	expected := map[compilerTypes.Type]string{
		// Int8:   I D D D C C C C C D D C
		compilerTypes.Int8: "IDDDCCCCCDDC",
		// Int16:  C I D D C C C C C D D C
		compilerTypes.Int16: "CIDDCCCCCDDC",
		// Int32:  C C I D C C C C C D D C
		compilerTypes.Int32: "CCIDCCCCCDDC",
		// Int64:  C C C I C C C C C D D C
		compilerTypes.Int64: "CCCICCCCCDDC",
		// UInt8:  C D D D I D D D C D D C
		compilerTypes.UInt8: "CDDDIDDDCDDC",
		// UInt16: C C D D C I D D C D D C
		compilerTypes.UInt16: "CCDDCIDDCDDC",
		// UInt32: C C C D C C I D C D D C
		compilerTypes.UInt32: "CCCDCCIDCDDC",
		// UInt64: C C C C C C C I C D D C
		compilerTypes.UInt64: "CCCCCCCICDDC",
		// Rune:   C C C D C C D D I D D C
		compilerTypes.Rune: "CCCDCCDDIDDC",
		// Float32:C C C C C C C C C I D C
		compilerTypes.Float32: "CCCCCCCCCIDC",
		// Float64:C C C C C C C C C C I C
		compilerTypes.Float64: "CCCCCCCCCCIC",
		// Size:   C C C C C C C C C D D I
		compilerTypes.SizeType: "CCCCCCCCCDDI",
	}
	for _, source := range scalars {
		wantRow, ok := expected[source]
		if !ok {
			t.Fatalf("missing expected row for %s", source.Name)
		}
		for index, target := range scalars {
			wantKind := conversionKind(0)
			switch wantRow[index] {
			case 'I':
				wantKind = conversionIdentity
			case 'D':
				wantKind = conversionDirect
			case 'C':
				wantKind = conversionChecked
			default:
				t.Fatalf("bad expectation %q for %s", wantRow, source.Name)
			}
			if got := classifyConversion(source, target); got != wantKind {
				t.Errorf("classifyConversion(%s, %s) = %v, want %v", source.Name, target.Name, got, wantKind)
			}
		}
	}
}

// Identity and direct conversions stay in the checked program but never
// enter the helper set; only checked pairs are collected, deduplicated by
// concrete pair.
func TestDiscoverGeneratedConversionsCollectsOnlyChecked(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    value: UInt8 = 12\n    wide: Float64 = value.to<Float64>()\n    big: Int64 = 9000000000\n    narrow: Int8 = big.to<Int8>()\n    again: Int8 = big.to<Int8>()\n    same: Int64 = big.to<Int64>()\nend")
	specs, _, err := discoverGeneratedConversions(program)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 {
		t.Fatalf("specs = %#v, want exactly the checked Int64-to-Int8 pair", specs)
	}
	if !compilerTypes.Equal(specs[0].source, compilerTypes.Int64) || !compilerTypes.Equal(specs[0].target, compilerTypes.Int8) {
		t.Fatalf("spec = %#v, want Int64 to Int8", specs[0])
	}
}

func TestDiscoverGeneratedConversionsIdentityNotCollected(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    value: Int32 = 12\n    same: Int32 = value.to<Int32>()\nend")
	specs, _, err := discoverGeneratedConversions(program)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 0 {
		t.Fatalf("specs = %#v, want no helpers for an identity conversion", specs)
	}
}

// Dynamic Float-to-integer helpers truncate exactly once into a temporary of
// the source floating type, check exact power-of-two bounds (inclusive lower,
// exclusive upper), and cast only the temporary. Size uses a target-derived
// threshold from SIZE_MAX converted to the source floating type before adding
// floating one.
func TestWriteFloatToIntegerConversionShapes(t *testing.T) {
	testCases := []struct {
		source compilerTypes.Type
		target compilerTypes.Type
		body   string
	}{
		{
			source: compilerTypes.Float64,
			target: compilerTypes.Int64,
			body: "    if (isnan(value) || isinf(value)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    double truncated = trunc(value);\n" +
				"    if (!(truncated >= -0x1p63 && truncated < 0x1p63)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    return (int64_t)truncated;\n",
		},
		{
			source: compilerTypes.Float32,
			target: compilerTypes.Int8,
			body: "    if (isnan(value) || isinf(value)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    float truncated = truncf(value);\n" +
				"    if (!(truncated >= -0x1p7 && truncated < 0x1p7)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    return (int8_t)truncated;\n",
		},
		{
			source: compilerTypes.Float64,
			target: compilerTypes.UInt64,
			body: "    if (isnan(value) || isinf(value)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    double truncated = trunc(value);\n" +
				"    if (!(truncated >= 0.0 && truncated < 0x1p64)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    return (uint64_t)truncated;\n",
		},
		{
			source: compilerTypes.Float32,
			target: compilerTypes.UInt16,
			body: "    if (isnan(value) || isinf(value)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    float truncated = truncf(value);\n" +
				"    if (!(truncated >= 0.0 && truncated < 0x1p16)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    return (uint16_t)truncated;\n",
		},
		{
			source: compilerTypes.Float64,
			target: compilerTypes.SizeType,
			body: "    if (isnan(value) || isinf(value)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    double truncated = trunc(value);\n" +
				"    if (!(truncated >= 0.0 && truncated < (double)SIZE_MAX + 1.0)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    return (size_t)truncated;\n",
		},
		{
			source: compilerTypes.Float32,
			target: compilerTypes.SizeType,
			body: "    if (isnan(value) || isinf(value)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    float truncated = truncf(value);\n" +
				"    if (!(truncated >= 0.0 && truncated < (float)SIZE_MAX + 1.0f)) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n" +
				"    return (size_t)truncated;\n",
		},
	}
	for _, testCase := range testCases {
		got := writeFloatToIntegerConversion(testCase.source, testCase.target)
		if got != testCase.body {
			t.Errorf("writeFloatToIntegerConversion(%s, %s) = %q, want %q", testCase.source.Name, testCase.target.Name, got, testCase.body)
		}
		if strings.Contains(got, "fromfp") || strings.Contains(got, "ufromfp") || strings.Contains(got, "errno") {
			t.Errorf("writeFloatToIntegerConversion(%s, %s) uses fromfp/ufromfp/errno: %q", testCase.source.Name, testCase.target.Name, got)
		}
	}
}

func TestWriteFloatToIntegerConversionNeverConvertsLimitMacros(t *testing.T) {
	for _, source := range []compilerTypes.Type{compilerTypes.Float32, compilerTypes.Float64} {
		for _, target := range []compilerTypes.Type{
			compilerTypes.Int8, compilerTypes.Int16, compilerTypes.Int32, compilerTypes.Int64,
			compilerTypes.UInt8, compilerTypes.UInt16, compilerTypes.UInt32, compilerTypes.UInt64, compilerTypes.SizeType,
		} {
			body := writeFloatToIntegerConversion(source, target)
			for _, forbidden := range []string{"INT64_MAX", "UINT64_MAX", "INT32_MAX", "<= SIZE_MAX"} {
				if strings.Contains(body, forbidden) {
					t.Errorf("writeFloatToIntegerConversion(%s, %s) compares %q: %q", source.Name, target.Name, forbidden, body)
				}
			}
			// Float32 sources truncate with truncf, Float64 with trunc, and
			// the temporary is declared and cast.
			truncate := "trunc("
			if compilerTypes.Equal(source, compilerTypes.Float32) {
				truncate = "truncf("
			}
			if strings.Count(body, truncate) != 1 || strings.Count(body, "truncated") != 4 {
				t.Errorf("writeFloatToIntegerConversion(%s, %s) must truncate exactly once and cast the temporary: %q", source.Name, target.Name, body)
			}
		}
	}
}

// Rendering dispatches by classification; the operand renders exactly once
// and a non-atomic operand is parenthesized inside a direct cast.
func TestRenderConversionClassification(t *testing.T) {
	testCases := []struct {
		name string
		node checker.Expression
		want string
	}{
		{
			name: "identity",
			node: checker.Expression{
				Kind:        checker.ConversionExpression,
				Operand:     expressionPointer(variableNode("value")),
				OperandType: compilerTypes.Int32,
				ResultType:  compilerTypes.Int32,
			},
			want: "hex_v_value",
		},
		{
			name: "direct UInt8 to Float64",
			node: checker.Expression{
				Kind:        checker.ConversionExpression,
				Operand:     expressionPointer(variableNode("value")),
				OperandType: compilerTypes.UInt8,
				ResultType:  compilerTypes.Float64,
			},
			want: "(double)hex_v_value",
		},
		{
			name: "direct Rune to UInt32",
			node: checker.Expression{
				Kind:        checker.ConversionExpression,
				Operand:     expressionPointer(variableNode("letter")),
				OperandType: compilerTypes.Rune,
				ResultType:  compilerTypes.UInt32,
			},
			want: "(uint32_t)hex_v_letter",
		},
		{
			name: "direct Float32 to Float64",
			node: checker.Expression{
				Kind:        checker.ConversionExpression,
				Operand:     expressionPointer(variableNode("value")),
				OperandType: compilerTypes.Float32,
				ResultType:  compilerTypes.Float64,
			},
			want: "(double)hex_v_value",
		},
		{
			name: "checked Int64 to Int8",
			node: checker.Expression{
				Kind:        checker.ConversionExpression,
				Operand:     expressionPointer(variableNode("value")),
				OperandType: compilerTypes.Int64,
				ResultType:  compilerTypes.Int8,
			},
			want: "hex_convert_int64_t_int8_t(hex_v_value)",
		},
		{
			name: "checked Float64 to Int32",
			node: checker.Expression{
				Kind:        checker.ConversionExpression,
				Operand:     expressionPointer(variableNode("value")),
				OperandType: compilerTypes.Float64,
				ResultType:  compilerTypes.Int32,
			},
			want: "hex_convert_double_int32_t(hex_v_value)",
		},
		{
			name: "non-atomic operand appears once in a direct cast",
			node: checker.Expression{
				Kind:        checker.ConversionExpression,
				Operand:     expressionPointer(binaryExpression(checker.AddOperator, compilerTypes.Float32, compilerTypes.Float32, variableNode("left"), variableNode("right"))),
				OperandType: compilerTypes.Float32,
				ResultType:  compilerTypes.Float64,
			},
			want: "(double)((hex_v_left + hex_v_right))",
		},
		{
			name: "non-atomic operand appears once in a checked call",
			node: checker.Expression{
				Kind:        checker.ConversionExpression,
				Operand:     expressionPointer(binaryExpression(checker.AddOperator, compilerTypes.Float64, compilerTypes.Float64, variableNode("left"), variableNode("right"))),
				OperandType: compilerTypes.Float64,
				ResultType:  compilerTypes.Int32,
			},
			want: "hex_convert_double_int32_t(((hex_v_left + hex_v_right)))",
		},
	}
	for _, testCase := range testCases {
		got, err := renderExpression(testCase.node)
		if err != nil {
			t.Fatalf("%s: renderExpression() error = %v", testCase.name, err)
		}
		if got != testCase.want {
			t.Errorf("%s = %q, want %q", testCase.name, got, testCase.want)
		}
		if strings.Count(got, "hex_v_left") > 1 || strings.Count(got, "hex_v_right") > 1 {
			t.Errorf("%s renders its operands more than once: %q", testCase.name, got)
		}
	}
}

// A direct-only program emits no conversion helper, no runtime trap, and no
// trap-owned headers; a checked program emits one deduplicated helper per
// concrete pair and the shared trap.
func TestGenerateDirectConversionEmitsCastOnly(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    value: UInt8 = 12\n    wide: Float64 = value.to<Float64>()\nend")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatal(err)
	}
	rootC, rootH, header := files["modules/app.c"], files["modules/app.h"], files["hexal.h"]
	if !strings.Contains(rootC, "(double)hex_v_value") {
		t.Fatalf("modules/app.c = %q, want a direct cast", rootC)
	}
	for _, content := range []string{rootC, rootH} {
		if strings.Contains(content, "hex_convert") {
			t.Fatalf("generated %q, direct conversion must not emit a helper", content)
		}
	}
	for _, forbidden := range []string{"hex_runtime_trap", "<stdio.h>", "<stdlib.h>", "<math.h>"} {
		if strings.Contains(header, forbidden) {
			t.Fatalf("hexal.h = %q, safe-only program must not select %q", header, forbidden)
		}
	}
}

func TestGenerateCheckedConversionSelectsHelperAndTrap(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    value: Float64 = 3.75\n    whole: Int32 = value.to<Int32>()\nend")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatal(err)
	}
	rootC, rootH, header := files["modules/app.c"], files["modules/app.h"], files["hexal.h"]
	for _, want := range []string{
		"static inline int32_t hex_convert_double_int32_t(double value) {",
		"if (isnan(value) || isinf(value)) {",
		"double truncated = trunc(value);",
		"if (!(truncated >= -0x1p31 && truncated < 0x1p31)) {",
		"return (int32_t)truncated;",
	} {
		if !strings.Contains(rootH, want) {
			t.Fatalf("modules/app.h = %q, want %q", rootH, want)
		}
	}
	if !strings.Contains(header, "[[noreturn]] void hex_runtime_trap(const char *message);") || !strings.Contains(header, "#include <math.h>") {
		t.Fatalf("hexal.h = %q, want the trap declaration and <math.h>", header)
	}
	if !strings.Contains(files["hexal/runtime.c"], "[[noreturn]] void hex_runtime_trap(const char *message) {") {
		t.Fatalf("hexal/runtime.c = %q, want the trap definition", files["hexal/runtime.c"])
	}
	if strings.Contains(rootC, "hex_runtime_trap(const char *message) {") {
		t.Fatalf("modules/app.c = %q, the trap definition moved to hexal/runtime.c", rootC)
	}
}

func TestGenerateRepeatedCheckedPairEmitsOneHelper(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    big: Int64 = 9000000000\n    a: Int8 = big.to<Int8>()\n    b: Int8 = big.to<Int8>()\nend")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatal(err)
	}
	rootH := files["modules/app.h"]
	rootC := files["modules/app.c"]
	if strings.Count(rootH, "static inline int8_t hex_convert_int64_t_int8_t") != 1 {
		t.Fatalf("modules/app.h = %q, want one deduplicated helper", rootH)
	}
	if strings.Count(rootC, "hex_convert_int64_t_int8_t(hex_v_big)") != 2 {
		t.Fatalf("modules/app.c = %q, want both call sites routed through the helper", rootC)
	}
}

func TestGenerateMixedSafeAndCheckedEmitsOnlyCheckedHelpers(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    value: UInt8 = 12\n    wide: Float64 = value.to<Float64>()\n    big: Int64 = 9000000000\n    narrow: Int8 = big.to<Int8>()\n    other: Float64 = value.to<Float64>()\nend")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatal(err)
	}
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if strings.Count(rootC, "(double)hex_v_value") != 2 {
		t.Fatalf("modules/app.c = %q, want both safe casts inline", rootC)
	}
	if strings.Contains(rootH, "hex_convert_uint8_t_double") || strings.Contains(rootH, "hex_convert_uint8_t_double") {
		t.Fatalf("modules/app.h = %q, safe pair must not enter the helper set", rootH)
	}
	if !strings.Contains(rootH, "static inline int8_t hex_convert_int64_t_int8_t") {
		t.Fatalf("modules/app.h = %q, want the checked helper", rootH)
	}
}
