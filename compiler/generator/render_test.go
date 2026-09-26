package generator

// Rendering of checked expressions: operand and constant spelling,
// arithmetic and ring helpers, float constants, place and receiver
// rejection, operation metadata, and truthiness and logical rendering.

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"
	"strings"
	"testing"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func TestRenderExpressionOperand(t *testing.T) {
	source := checker.Operand{
		Kind: checker.ExpressionOperand,
		Type: compilerTypes.Int32,
		Node: variableNode("value"),
	}
	got, err := renderOperand(source, newLiteralRegistry())
	if err != nil {
		t.Fatalf("renderOperand() error = %v", err)
	}
	if got != "hex_v_value" {
		t.Fatalf("rendered expression operand = %q, want %q", got, "hex_v_value")
	}
}

func TestRenderConstantExpression(t *testing.T) {
	source := intSource(compilerTypes.Int32, 13, "13")
	node := checker.Expression{
		Kind:       checker.ConstantExpression,
		Constant:   &source,
		ResultType: compilerTypes.Int32,
	}
	got, err := renderExpression(node, newLiteralRegistry())
	if err != nil {
		t.Fatalf("renderExpression() error = %v", err)
	}
	if got != "13" {
		t.Fatalf("rendered constant expression = %q, want %q", got, "13")
	}
}

func TestRenderSignedInt8Addition(t *testing.T) {
	node := binaryExpression(checker.AddOperator, compilerTypes.Int8, compilerTypes.Int8, variableNode("left"), variableNode("right"))
	got, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int8, Node: node}, newLiteralRegistry())
	if err != nil {
		t.Fatalf("renderOperand() error = %v", err)
	}
	want := "hex_wrap_add_int8_t(hex_v_left, hex_v_right)"
	if got != want {
		t.Fatalf("signed Int8 addition = %q, want %q", got, want)
	}
}

// Signed wrapping +, -, *, and unary - lower through ckd_* helpers; no
// unsigned intermediate or reconstruction ternary remains.
func TestRenderSignedArithmeticUsesWrapHelpers(t *testing.T) {
	testCases := []struct {
		typ      compilerTypes.Type
		helper   string
		operator checker.Operator
	}{
		{compilerTypes.Int8, "hex_wrap_add_int8_t", checker.AddOperator},
		{compilerTypes.Int16, "hex_wrap_sub_int16_t", checker.SubtractOperator},
		{compilerTypes.Int32, "hex_wrap_mul_int32_t", checker.MultiplyOperator},
		{compilerTypes.Int64, "hex_wrap_add_int64_t", checker.AddOperator},
	}
	for _, testCase := range testCases {
		node := binaryExpression(testCase.operator, testCase.typ, testCase.typ, variableNode("left"), variableNode("right"))
		got, err := renderExpression(node, newLiteralRegistry())
		if err != nil {
			t.Fatalf("renderExpression(%s, %s) error = %v", testCase.typ.Name, testCase.operator, err)
		}
		want := testCase.helper + "(hex_v_left, hex_v_right)"
		if got != want {
			t.Errorf("signed %s = %q, want %q", testCase.typ.Name, got, want)
		}
	}

	for _, typ := range []compilerTypes.Type{compilerTypes.Int8, compilerTypes.Int16, compilerTypes.Int32, compilerTypes.Int64} {
		node := unaryExpression(checker.NegateOperator, typ, typ, variableNode("value"))
		got, err := renderExpression(node, newLiteralRegistry())
		if err != nil {
			t.Fatalf("renderExpression(%s unary -) error = %v", typ.Name, err)
		}
		want := "hex_wrap_neg_" + typ.CName + "(hex_v_value)"
		if got != want {
			t.Errorf("signed %s negation = %q, want %q", typ.Name, got, want)
		}
	}
}

// Every covered unsigned type lowers one binary operation to one uintmax_t
// seed on the left operand and one narrowing cast at the result boundary:
// no per-node widening/narrowing pair, and no width-picked
// intermediate.
func TestRenderUnsignedRingOperationSeedsOnceAndNarrowsOnce(t *testing.T) {
	for _, typ := range unsignedRingTypes() {
		for operator, text := range map[checker.Operator]string{
			checker.AddOperator: "+", checker.SubtractOperator: "-", checker.MultiplyOperator: "*",
		} {
			node := binaryExpression(operator, typ, typ, variableNode("left"), variableNode("right"))
			got, err := renderExpression(node, newLiteralRegistry())
			if err != nil {
				t.Fatalf("renderExpression(%s %s) error = %v", typ.Name, text, err)
			}
			want := fmt.Sprintf("(%s)((uintmax_t)hex_v_left %s hex_v_right)", unsignedRingCName(typ), text)
			if got != want {
				t.Errorf("unsigned %s %s = %q, want %q", typ.Name, text, got, want)
			}
		}
	}
}

// A left-associated chain is one maximal ring tree: one seed at its leftmost
// operand, one narrowing at its root, and nothing in between.
func TestRenderUnsignedRingChainSeedsOnlyItsLeftmostOperand(t *testing.T) {
	for _, typ := range unsignedRingTypes() {
		inner := binaryExpression(checker.AddOperator, typ, typ, variableNode("a"), variableNode("b"))
		outer := binaryExpression(checker.AddOperator, typ, typ, inner, variableNode("c"))
		node := binaryExpression(checker.AddOperator, typ, typ, outer, variableNode("d"))
		got, err := renderExpression(node, newLiteralRegistry())
		if err != nil {
			t.Fatalf("renderExpression(%s chain) error = %v", typ.Name, err)
		}
		want := fmt.Sprintf("(%s)((uintmax_t)hex_v_a + hex_v_b + hex_v_c + hex_v_d)", unsignedRingCName(typ))
		if got != want {
			t.Errorf("unsigned %s chain = %q, want %q", typ.Name, got, want)
		}
		if strings.Count(got, "uintmax_t") != 1 {
			t.Errorf("unsigned %s chain = %q, want exactly one uintmax_t seed", typ.Name, got)
		}
	}
}

// A right-nested ring subtree evaluates before its parent converts anything,
// so it carries its own seed.
func TestRenderUnsignedRingRightSubtreeCarriesItsOwnSeed(t *testing.T) {
	typ := compilerTypes.UInt32
	inner := binaryExpression(checker.MultiplyOperator, typ, typ, variableNode("b"), variableNode("c"))
	node := binaryExpression(checker.AddOperator, typ, typ, variableNode("a"), inner)
	got, err := renderExpression(node, newLiteralRegistry())
	if err != nil {
		t.Fatalf("renderExpression(right subtree) error = %v", err)
	}
	want := "(uint32_t)((uintmax_t)hex_v_a + (uintmax_t)hex_v_b * hex_v_c)"
	if got != want {
		t.Errorf("right-nested ring subtree = %q, want %q", got, want)
	}
}

// Mixed +, -, and * trees keep their AST grouping. Each case states the C
// text that must parse the same way the checked tree is shaped; the
// regrouping the construction must not permit is named beside it.
func TestRenderUnsignedRingTreesPreserveGrouping(t *testing.T) {
	typ := compilerTypes.UInt32
	ring := func(operator checker.Operator, left, right checker.Expression) checker.Expression {
		return binaryExpression(operator, typ, typ, left, right)
	}
	a, b, c := variableNode("a"), variableNode("b"), variableNode("c")
	for _, testCase := range []struct {
		name     string
		node     checker.Expression
		want     string
		regroups string
	}{
		{
			name: "ring subtree on the right",
			node: ring(checker.MultiplyOperator, a, ring(checker.SubtractOperator, b, c)),
			want: "(uint32_t)((uintmax_t)hex_v_a * ((uintmax_t)hex_v_b - hex_v_c))", regroups: "(a*b)-c",
		},
		{
			name: "ring subtree on the left",
			node: ring(checker.MultiplyOperator, ring(checker.AddOperator, a, b), c),
			want: "(uint32_t)(((uintmax_t)hex_v_a + hex_v_b) * hex_v_c)", regroups: "a+(b*c)",
		},
		{
			name: "higher-precedence ring subtree on the right needs no grouping",
			node: ring(checker.AddOperator, a, ring(checker.MultiplyOperator, b, c)),
			want: "(uint32_t)((uintmax_t)hex_v_a + (uintmax_t)hex_v_b * hex_v_c)", regroups: "",
		},
		{
			name: "equal-precedence ring subtree on the right keeps grouping",
			node: ring(checker.SubtractOperator, a, ring(checker.SubtractOperator, b, c)),
			want: "(uint32_t)((uintmax_t)hex_v_a - ((uintmax_t)hex_v_b - hex_v_c))", regroups: "(a-b)-c",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := renderExpression(testCase.node, newLiteralRegistry())
			if err != nil {
				t.Fatalf("renderExpression error = %v", err)
			}
			if got != testCase.want {
				t.Errorf("got %q, want %q (regrouping to avoid: %s)", got, testCase.want, testCase.regroups)
			}
		})
	}
}

// unsignedRingTypes lists every unsigned type the ring lowering covers.
// Byte is UInt8's alias
// and Size is the only one whose C name is not exact-width.
func unsignedRingTypes() []compilerTypes.Type {
	return []compilerTypes.Type{
		compilerTypes.UInt8, compilerTypes.UInt16, compilerTypes.UInt32,
		compilerTypes.UInt64, compilerTypes.SizeType,
	}
}

func unsignedRingCName(typ compilerTypes.Type) string {
	name, ok := unsignedCName(typ)
	if !ok {
		return typ.CName
	}
	return name
}

// Generated C contains no generic target-profile probes. Toolchain
// qualification (8-bit bytes, exact-width integers, IEC float representations)
// is a supported-toolchain contract owned outside generated source.
func TestNoTargetProfileProbesEmitted(t *testing.T) {
	header, err := hexalHeader(hexalHeaderInput{})
	if err != nil {
		t.Fatalf("hexalHeader() error = %v", err)
	}
	for _, forbidden := range []string{
		"CHAR_BIT",
		"static_assert(sizeof(uint8_t)",
		"static_assert(sizeof(int32_t)",
		"FLT_RADIX",
		"FLT_MANT_DIG",
		"DBL_MANT_DIG",
		"FLT_IS_IEC_60559",
		"DBL_IS_IEC_60559",
		"#include <stdbool.h>",
		"#include <limits.h>",
		"#include <float.h>",
		"hex_eos",
	} {
		if strings.Contains(header, forbidden) {
			t.Fatalf("hexal.h = %q, target profile probe %q must not be emitted", header, forbidden)
		}
	}
	if strings.Contains(header, "static_assert") {
		t.Fatalf("hexal.h = %q, no source-dependent Size assertion is present", header)
	}
}

func TestRenderEveryOperationOperator(t *testing.T) {
	left := variableNode("left")
	right := variableNode("right")
	testCases := []struct {
		name string
		node checker.Expression
		want string
	}{
		{"negate", unaryExpression(checker.NegateOperator, compilerTypes.Int32, compilerTypes.Int32, left), "hex_wrap_neg_int32_t(hex_v_left)"},
		{"logical not", unaryExpression(checker.LogicalNotOperator, compilerTypes.Bool, compilerTypes.Bool, left), "(!hex_v_left)"},
		{"add", binaryExpression(checker.AddOperator, compilerTypes.Float64, compilerTypes.Float64, left, right), "(hex_v_left + hex_v_right)"},
		{"subtract", binaryExpression(checker.SubtractOperator, compilerTypes.Float64, compilerTypes.Float64, left, right), "(hex_v_left - hex_v_right)"},
		{"multiply", binaryExpression(checker.MultiplyOperator, compilerTypes.Float64, compilerTypes.Float64, left, right), "(hex_v_left * hex_v_right)"},
		{"divide", binaryExpression(checker.DivideOperator, compilerTypes.Int32, compilerTypes.Int32, left, right), "hex_div_int32_t(hex_v_left, hex_v_right)"},
		{"remainder", binaryExpression(checker.RemainderOperator, compilerTypes.Int32, compilerTypes.Int32, left, right), "hex_rem_int32_t(hex_v_left, hex_v_right)"},
		{"equal", binaryExpression(checker.EqualOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left == hex_v_right)"},
		{"not equal", binaryExpression(checker.NotEqualOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left != hex_v_right)"},
		{"less", binaryExpression(checker.LessOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left < hex_v_right)"},
		{"less equal", binaryExpression(checker.LessEqualOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left <= hex_v_right)"},
		{"greater", binaryExpression(checker.GreaterOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left > hex_v_right)"},
		{"greater equal", binaryExpression(checker.GreaterEqualOperator, compilerTypes.Int32, compilerTypes.Bool, left, right), "(hex_v_left >= hex_v_right)"},
		{"logical and", binaryExpression(checker.LogicalAndOperator, compilerTypes.Bool, compilerTypes.Bool, left, right), "(hex_v_left && hex_v_right)"},
		{"logical or", binaryExpression(checker.LogicalOrOperator, compilerTypes.Bool, compilerTypes.Bool, left, right), "(hex_v_left || hex_v_right)"},
	}
	for _, testCase := range testCases {
		got, err := renderExpression(testCase.node, newLiteralRegistry())
		if err != nil {
			t.Errorf("%s error = %v", testCase.name, err)
			continue
		}
		if got != testCase.want {
			t.Errorf("%s = %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

func TestRenderOperationsAlwaysParenthesizeNestedExpressions(t *testing.T) {
	inner := binaryExpression(checker.AddOperator, compilerTypes.Float64, compilerTypes.Float64, variableNode("left"), variableNode("right"))
	outer := binaryExpression(checker.MultiplyOperator, compilerTypes.Float64, compilerTypes.Float64, inner, variableNode("scale"))
	got, err := renderExpression(outer, newLiteralRegistry())
	if err != nil {
		t.Fatalf("renderExpression() error = %v", err)
	}
	want := "((hex_v_left + hex_v_right) * hex_v_scale)"
	if got != want {
		t.Fatalf("nested operation = %q, want %q", got, want)
	}
}

func TestRenderOperationsRejectMismatchedNestedChildTypes(t *testing.T) {
	inner := binaryExpression(checker.AddOperator, compilerTypes.Int32, compilerTypes.Int32, variableNode("left"), variableNode("right"))
	testCases := []checker.Expression{
		unaryExpression(checker.NegateOperator, compilerTypes.Int64, compilerTypes.Int64, inner),
		binaryExpression(checker.AddOperator, compilerTypes.Int64, compilerTypes.Int64, inner, variableNode("right")),
		binaryExpression(checker.EqualOperator, compilerTypes.Int64, compilerTypes.Bool, inner, variableNode("right")),
	}
	for index, node := range testCases {
		_, err := renderExpression(node, newLiteralRegistry())
		diagnostic, ok := err.(compilerTypes.Diagnostic)
		if !ok {
			t.Errorf("case %d error = %T %v, want compilerTypes.Diagnostic", index, err, err)
			continue
		}
		if diagnostic.Message.Category() != compilerTypes.UnknownError || diagnostic.Message.Stage() != "generator" {
			t.Errorf("case %d diagnostic = %#v, want generator Unknown Error", index, diagnostic)
		}
	}
}

func TestRenderOperandRejectsMismatchedRootExpressionType(t *testing.T) {
	node := binaryExpression(checker.AddOperator, compilerTypes.Int32, compilerTypes.Int32, variableNode("left"), variableNode("right"))
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int64, Node: node}, newLiteralRegistry())
	diagnostic, ok := err.(compilerTypes.Diagnostic)
	if !ok {
		t.Fatalf("error = %T %v, want compilerTypes.Diagnostic", err, err)
	}
	if diagnostic.Message.Category() != compilerTypes.UnknownError || diagnostic.Message.Stage() != "generator" {
		t.Fatalf("diagnostic = %#v, want generator Unknown Error", diagnostic)
	}
}

func TestRenderMalformedIntegerConstantsFailsClosed(t *testing.T) {
	tooLarge := constant.MakeFromLiteral("18446744073709551616", gotoken.INT, 0)
	testCases := []struct {
		name   string
		source checker.Operand
	}{
		{name: "missing", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Int32}},
		{name: "wrong constant kind", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Int32, Constant: constant.MakeString("not an integer")}},
		{name: "outside target range", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Int8, Constant: constant.MakeInt64(128)}},
		{name: "outside uint64", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.UInt64, Constant: tooLarge}},
		{name: "negative unsigned", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.UInt8, Constant: constant.MakeInt64(-1)}},
		{name: "invalid radix", source: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Int32, Constant: constant.MakeInt64(1), Radix: checker.LiteralRadix(255)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := renderOperand(testCase.source, newLiteralRegistry())
			diagnostic, ok := err.(compilerTypes.Diagnostic)
			if !ok {
				t.Fatalf("error = %T %v, want compilerTypes.Diagnostic", err, err)
			}
			if diagnostic.Message.Category() != compilerTypes.UnknownError || diagnostic.Message.Stage() != "generator" {
				t.Fatalf("diagnostic = %#v, want generator Unknown Error", diagnostic)
			}
		})
	}
}

func TestRenderRejectsInconsistentIntegerMetadata(t *testing.T) {
	positiveWithNegativeFlag := intSource(compilerTypes.Int32, 1, "1")
	positiveWithNegativeFlag.Negative = true
	negativeWithoutNegativeFlag := intSource(compilerTypes.Int32, -1, "1")
	negativeLiteralMismatch := intSource(compilerTypes.Int32, -2, "1")
	negativeLiteralMismatch.Negative = true
	positiveLiteralMismatch := intSource(compilerTypes.Int32, 2, "1")
	testCases := []struct {
		name   string
		source checker.Operand
	}{
		{name: "positive value marked negative", source: positiveWithNegativeFlag},
		{name: "negative value missing negative flag", source: negativeWithoutNegativeFlag},
		{name: "negative literal mismatch", source: negativeLiteralMismatch},
		{name: "positive literal mismatch", source: positiveLiteralMismatch},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := renderOperand(testCase.source, newLiteralRegistry())
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestRenderRejectsMalformedFloatConstants(t *testing.T) {
	testCases := []struct {
		name   string
		source checker.Operand
	}{
		{
			name: "float32 bits exceed width",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float32,
				Constant:  constant.MakeFloat64(1.5),
				Literal:   "1.5",
				FloatBits: uint64(math.Float32bits(1.5)) | uint64(1)<<32,
			},
		},
		{
			name: "float64 bits disagree with constant",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeFloat64(1.5),
				Literal:   "1.5",
				FloatBits: math.Float64bits(2.5),
			},
		},
		{
			name: "float sign metadata disagrees with bits",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeFloat64(1.5),
				Literal:   "1.5",
				Negative:  true,
				FloatBits: math.Float64bits(1.5),
			},
		},
		{
			name: "float literal disagrees with constant",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeFloat64(1.5),
				Literal:   "1.25",
				FloatBits: math.Float64bits(1.5),
			},
		},
		{
			name: "non numeric constant",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeString("1.5"),
				Literal:   "1.5",
				FloatBits: math.Float64bits(1.5),
			},
		},
		{
			name: "NaN literal metadata",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeUnknown(),
				Literal:   "NaN",
				FloatBits: math.Float64bits(math.NaN()),
			},
		},
		{
			name: "infinity literal metadata",
			source: checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      compilerTypes.Float64,
				Constant:  constant.MakeUnknown(),
				Literal:   "Inf",
				FloatBits: math.Float64bits(math.Inf(1)),
			},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := renderOperand(testCase.source, newLiteralRegistry())
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestRenderSupportsValidFoldedFloatSpecialValues(t *testing.T) {
	for _, testCase := range []struct {
		name string
		typ  compilerTypes.Type
		bits uint64
		want string
	}{
		{name: "float32 infinity", typ: compilerTypes.Float32, bits: uint64(math.Float32bits(float32(math.Inf(1)))), want: "INFINITY"},
		{name: "float64 negative infinity", typ: compilerTypes.Float64, bits: math.Float64bits(math.Inf(-1)), want: "-INFINITY"},
		{name: "float64 NaN", typ: compilerTypes.Float64, bits: math.Float64bits(math.NaN()), want: "NAN"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := renderOperand(checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      testCase.typ,
				Constant:  constant.MakeUnknown(),
				FloatBits: testCase.bits,
				Negative:  testCase.want == "-INFINITY",
			}, newLiteralRegistry())
			if err != nil {
				t.Fatalf("renderOperand() error = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("rendered special value = %q, want %q", got, testCase.want)
			}
		})
	}
}

// Every finite float literal renders as the shortest readable decimal C
// literal that reparses to the exact checked IEEE bits, with the f suffix
// for Float32, a fractional point on integral mantissas, and the retained
// negative-zero sign.
func TestRenderFiniteFloatLiteralsRoundTripDecimal(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		typ      compilerTypes.Type
		bits     uint64
		negative bool
		value    constant.Value
		want     string
	}{
		{name: "float32 three quarters", typ: compilerTypes.Float32, bits: uint64(math.Float32bits(0.75)), value: constant.MakeFloat64(0.75), want: "0.75f"},
		{name: "float64 three quarters", typ: compilerTypes.Float64, bits: math.Float64bits(0.75), value: constant.MakeFloat64(0.75), want: "0.75"},
		{name: "float32 whole", typ: compilerTypes.Float32, bits: uint64(math.Float32bits(3)), value: constant.MakeFloat64(3), want: "3.0f"},
		{name: "float64 whole", typ: compilerTypes.Float64, bits: math.Float64bits(3), value: constant.MakeFloat64(3), want: "3.0"},
		{name: "float64 negative zero", typ: compilerTypes.Float64, bits: math.Float64bits(math.Copysign(0, -1)), negative: true, value: constant.MakeFloat64(0), want: "-0.0"},
		{name: "float32 positive zero", typ: compilerTypes.Float32, bits: 0, value: constant.MakeFloat64(0), want: "0.0f"},
		{name: "float64 min subnormal", typ: compilerTypes.Float64, bits: 1, value: constant.MakeFloat64(math.Float64frombits(1)), want: "5e-324"},
		{name: "float64 max finite", typ: compilerTypes.Float64, bits: math.Float64bits(math.MaxFloat64), value: constant.MakeFloat64(math.MaxFloat64), want: "1.7976931348623157e+308"},
		{name: "float32 max finite", typ: compilerTypes.Float32, bits: uint64(math.Float32bits(math.MaxFloat32)), value: constant.MakeFloat64(float64(math.MaxFloat32)), want: "3.4028235e+38f"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := renderOperand(checker.Operand{
				Kind:      checker.ConstantOperand,
				Type:      testCase.typ,
				Constant:  testCase.value,
				FloatBits: testCase.bits,
				Negative:  testCase.negative,
			}, newLiteralRegistry())
			if err != nil {
				t.Fatalf("renderOperand() error = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("rendered finite value = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestRenderRejectsInvalidAddressDereferenceChildren(t *testing.T) {
	constantSource := intSource(compilerTypes.Int32, 1, "1")
	constantChild := checker.Expression{Kind: checker.ConstantExpression, Constant: &constantSource, ResultType: compilerTypes.Int32}
	invalidAddress := checker.Operand{
		Kind: checker.ExpressionOperand,
		Type: compilerTypes.Int32,
		Node: addressNode("value"),
	}
	invalidAddressChild := checker.Operand{
		Kind: checker.ExpressionOperand,
		Type: compilerTypes.MutPtrType(compilerTypes.Int32),
		Node: checker.Expression{Kind: checker.AddressOfExpression, Operand: &constantChild},
	}
	invalidDereference := checker.Operand{
		Kind: checker.ExpressionOperand,
		Type: compilerTypes.Int32,
		Node: checker.Expression{Kind: checker.DereferenceExpression, Operand: &constantChild},
	}
	invalidDereferenceMetadata := checker.Operand{
		Kind: checker.ExpressionOperand,
		Type: compilerTypes.Int32,
		Node: checker.Expression{Kind: checker.DereferenceExpression, Operand: expressionPointer(variableNode("value")), OperandType: compilerTypes.Int32},
	}
	for _, testCase := range []struct {
		name   string
		source checker.Operand
	}{
		{name: "address-of scalar result", source: invalidAddress},
		{name: "address-of non-place child", source: invalidAddressChild},
		{name: "dereference scalar child", source: invalidDereference},
		{name: "dereference scalar metadata", source: invalidDereferenceMetadata},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := renderOperand(testCase.source, newLiteralRegistry())
			assertGeneratorUnknownError(t, err)
		})
	}
}

func TestRenderRejectsNestedMalformedOperation(t *testing.T) {
	inner := unaryExpression(checker.NegateOperator, compilerTypes.Int32, compilerTypes.Int32, addressNode("value"))
	outer := binaryExpression(checker.AddOperator, compilerTypes.Int32, compilerTypes.Int32, inner, variableNode("other"))
	_, err := renderOperand(checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: outer}, newLiteralRegistry())
	assertGeneratorUnknownError(t, err)
}

func TestTypeRequirementsTraverseOperationNodes(t *testing.T) {
	float32Comparison := binaryExpression(checker.EqualOperator, compilerTypes.Float32, compilerTypes.Bool, variableNode("a"), variableNode("b"))
	float64Comparison := binaryExpression(checker.EqualOperator, compilerTypes.Float64, compilerTypes.Bool, variableNode("c"), variableNode("d"))
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "one", Type: compilerTypes.Bool, Source: checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Bool, Node: float32Comparison}},
		checker.Declaration{Name: "two", Type: compilerTypes.Bool, Source: checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Bool, Node: float64Comparison}},
	}}
	requirements := &cHeaderRequirements{}
	collectTypeRequirements(program, requirements)
	// Float comparisons emit no headers: the representation facts are a
	// toolchain contract, not program facts.
	if len(requirements.headers) != 0 {
		t.Fatalf("collectTypeRequirements() = %v, want no headers for float-only operations", requirements.headers)
	}
}

func TestRenderMalformedOperationFailsClosed(t *testing.T) {
	testCases := []checker.Expression{
		{Kind: checker.UnaryOperationExpression, Operator: checker.NegateOperator, OperandType: compilerTypes.Int32, ResultType: compilerTypes.Int32},
		{Kind: checker.BinaryOperationExpression, Operator: checker.AddOperator, OperandType: compilerTypes.Int32, ResultType: compilerTypes.Int32},
		binaryExpression(checker.InvalidOperator, compilerTypes.Int32, compilerTypes.Int32, variableNode("left"), variableNode("right")),
	}
	for index, node := range testCases {
		_, err := renderExpression(node, newLiteralRegistry())
		if err == nil || !strings.Contains(err.Error(), "[Unknown Error ") {
			t.Errorf("malformed operation %d error = %v, want structured Unknown Error", index, err)
		}
	}
}

func TestRenderBinaryOperationRejectsInvalidMetadata(t *testing.T) {
	invalidScalar := compilerTypes.Type{Name: "Invalid", CName: "invalid_t", ScalarKind: compilerTypes.ScalarUnsignedInteger, Bits: 7}
	testCases := []struct {
		name string
		node checker.Expression
	}{
		{
			name: "arithmetic result type",
			node: binaryExpression(checker.AddOperator, compilerTypes.Int32, compilerTypes.UInt32, variableNode("left"), variableNode("right")),
		},
		{
			name: "unsupported operand type",
			node: binaryExpression(checker.EqualOperator, invalidScalar, compilerTypes.Bool, variableNode("left"), variableNode("right")),
		},
		{
			name: "unsupported operation",
			node: binaryExpression(checker.Operator(255), compilerTypes.Int32, compilerTypes.Int32, variableNode("left"), variableNode("right")),
		},
	}
	for _, testCase := range testCases {
		_, err := renderExpression(testCase.node, newLiteralRegistry())
		if err == nil || !strings.Contains(err.Error(), "[Unknown Error ") {
			t.Errorf("%s error = %v, want structured Unknown Error", testCase.name, err)
		}
	}
}

func TestRenderOperationsRejectMalformedScalarMetadata(t *testing.T) {
	fakeFloat := compilerTypes.Float64
	fakeFloat.Name = "FakeFloat64"
	fakeFloat.CName = "fake_float_t"
	testCases := []struct {
		name string
		node checker.Expression
	}{
		{
			name: "comparison result must be Bool",
			node: binaryExpression(checker.EqualOperator, compilerTypes.Int32, compilerTypes.Int32, variableNode("left"), variableNode("right")),
		},
		// A non-Bool logical operand is valid (its truthiness is rendered),
		// so only a non-Bool result stays malformed.
		{
			name: "logical result must be Bool",
			node: binaryExpression(checker.LogicalAndOperator, compilerTypes.Bool, compilerTypes.Int32, variableNode("left"), variableNode("right")),
		},
		{
			name: "remainder rejects Float64",
			node: binaryExpression(checker.RemainderOperator, compilerTypes.Float64, compilerTypes.Float64, variableNode("left"), variableNode("right")),
		},
		{
			name: "ordering rejects Bool",
			node: binaryExpression(checker.LessOperator, compilerTypes.Bool, compilerTypes.Bool, variableNode("left"), variableNode("right")),
		},
		{
			name: "unary fake scalar",
			node: unaryExpression(checker.NegateOperator, fakeFloat, compilerTypes.Float64, variableNode("value")),
		},
		{
			name: "binary fake scalar",
			node: binaryExpression(checker.AddOperator, compilerTypes.Float64, fakeFloat, variableNode("left"), variableNode("right")),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := renderExpression(testCase.node, newLiteralRegistry())
			diagnostic, ok := err.(compilerTypes.Diagnostic)
			if !ok {
				t.Fatalf("error = %T %v, want compilerTypes.Diagnostic", err, err)
			}
			if diagnostic.Message.Category() != compilerTypes.UnknownError {
				t.Errorf("diagnostic category = %q, want %q", diagnostic.Message.Category(), compilerTypes.UnknownError)
			}
			if diagnostic.Message.Stage() != "generator" {
				t.Errorf("diagnostic stage = %q, want generator", diagnostic.Message.Stage())
			}
		})
	}
}

// Conditions render through truthiness: nil as false, a nullable as a null
// test, and an always-truthy value as a comma evaluation.
func TestRenderTruthinessConditions(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	nullable := environment.NullableType(environment.PtrType(compilerTypes.Int32))

	for _, testCase := range []struct {
		name      string
		condition checker.Operand
		want      string
	}{
		{
			name:      "nil",
			condition: checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Nil, Node: checker.Expression{Kind: checker.NilExpression, ResultType: compilerTypes.Nil}},
			want:      "if (false) {",
		},
		{
			name: "always truthy",
			condition: checker.Operand{
				Kind:     checker.ConstantOperand,
				Type:     compilerTypes.Int32,
				Constant: constant.MakeInt64(0),
				Literal:  "0",
				Node:     checker.Expression{Kind: checker.ConstantExpression, ResultType: compilerTypes.Int32},
			},
			want: "if ((void)(0), true) {",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var body strings.Builder
			state := newExpressionValidation()
			state.pushScope()
			err := writeStatementsAt(&body, []checker.Statement{checker.IfStatement{Condition: testCase.condition, Then: []checker.Statement{}}}, state, statementFrame{}, "")
			if err != nil {
				t.Fatalf("writeStatementsAt() error = %v", err)
			}
			if !strings.Contains(body.String(), testCase.want) {
				t.Fatalf("body = %q, want %q", body.String(), testCase.want)
			}
		})
	}

	// A nullable binding renders as a null test; the binding must be
	// registered so the variable's type and name resolve. Production always
	// pushes its root scope first; the test does the same.
	state := newExpressionValidation()
	state.pushScope()
	if _, err := state.allocateBinding(1, "maybe", nullable, true); err != nil {
		t.Fatalf("allocateBinding() error = %v", err)
	}
	condition := checker.Operand{
		Kind: checker.VariableOperand,
		Type: nullable,
		Node: checker.Expression{Kind: checker.VariableExpression, Name: "maybe", Binding: 1, ResultType: nullable},
	}
	var body strings.Builder
	err := writeStatementsAt(&body, []checker.Statement{checker.IfStatement{Condition: condition, Then: []checker.Statement{}}}, state, statementFrame{}, "")
	if err != nil {
		t.Fatalf("writeStatementsAt() error = %v", err)
	}
	if !strings.Contains(body.String(), "if (hex_v_maybe != nullptr) {") {
		t.Fatalf("body = %q, want a nullable null-test condition", body.String())
	}
}

// Logical operations render mixed and non-Bool operands through their
// truthiness; constant operands need no bindings.
func TestRenderLogicalOperationWithMixedOperands(t *testing.T) {
	boolConstant := func(value bool) checker.Expression {
		literal := "false"
		if value {
			literal = "true"
		}
		return constantExpression(checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Bool, Constant: constant.MakeBool(value), Literal: literal})
	}
	for _, testCase := range []struct {
		name string
		node checker.Expression
		want string
	}{
		{
			name: "int and bool",
			node: binaryExpression(checker.LogicalAndOperator, compilerTypes.Int32, compilerTypes.Bool,
				constantExpression(intSource(compilerTypes.Int32, 1, "1")), boolConstant(true)),
			want: "(((void)(1), true) && true)",
		},
		{
			name: "nil or bool",
			node: binaryExpression(checker.LogicalOrOperator, compilerTypes.Nil, compilerTypes.Bool,
				checker.Expression{Kind: checker.NilExpression, ResultType: compilerTypes.Nil}, boolConstant(false)),
			want: "(false || false)",
		},
		{
			name: "not int",
			node: unaryExpression(checker.LogicalNotOperator, compilerTypes.Int32, compilerTypes.Bool,
				constantExpression(intSource(compilerTypes.Int32, 0, "0"))),
			want: "(!((void)(0), true))",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := renderExpression(testCase.node, newLiteralRegistry())
			if err != nil {
				t.Fatalf("renderExpression() error = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("rendered = %q, want %q", got, testCase.want)
			}
		})
	}
}
