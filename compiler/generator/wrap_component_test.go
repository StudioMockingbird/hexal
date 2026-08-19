package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// The wrap.h template renders each selected helper exactly once, in the
// existing discovery order, under the HEXAL_WRAP_H guard with hexal.h
// included directly. Helper bodies stay byte-identical to the pre-split
// hexal.h forms: same names, signatures, ckd_* calls, and emission order.
func TestWrapHeaderRendersSelectedHelpersInDiscoveryOrder(t *testing.T) {
	state := &generatedWrapState{seen: make(map[string]bool)}
	state.order = []wrapOperation{
		{name: "add", typ: compilerTypes.Int32},
		{name: "sub", typ: compilerTypes.Int16},
		{name: "mul", typ: compilerTypes.Int64},
		{name: "neg", typ: compilerTypes.Int8},
	}
	artifacts, err := renderComponentArtifacts(&programEmission{wrapState: state}, Config{})
	if err != nil {
		t.Fatalf("renderComponentArtifacts() error = %v", err)
	}
	header, exists := artifacts["hexal/wrap.h"]
	if !exists {
		t.Fatalf("wrapping program emitted no hexal/wrap.h: %v", artifacts)
	}
	if !strings.HasPrefix(header, "#ifndef HEXAL_WRAP_H\n#define HEXAL_WRAP_H\n#include \"hexal.h\"\n") {
		t.Fatalf("hexal/wrap.h lost its guard or its hexal.h include: %q", header)
	}
	if !strings.HasSuffix(header, "\n#endif\n") || strings.HasSuffix(header, "\n\n") {
		t.Fatalf("hexal/wrap.h must end with #endif and exactly one trailing newline: %q", header)
	}
	helpers := []string{
		"static inline int32_t hex_wrap_add_int32_t(int32_t a, int32_t b) {\n    int32_t r;\n    ckd_add(&r, a, b);\n    return r;\n}",
		"static inline int16_t hex_wrap_sub_int16_t(int16_t a, int16_t b) {\n    int16_t r;\n    ckd_sub(&r, a, b);\n    return r;\n}",
		"static inline int64_t hex_wrap_mul_int64_t(int64_t a, int64_t b) {\n    int64_t r;\n    ckd_mul(&r, a, b);\n    return r;\n}",
		"static inline int8_t hex_wrap_neg_int8_t(int8_t a) {\n    int8_t r;\n    ckd_sub(&r, 0, a);\n    return r;\n}",
	}
	positions := make([]int, len(helpers))
	for index, helper := range helpers {
		if count := strings.Count(header, helper); count != 1 {
			t.Fatalf("hexal/wrap.h contains %q %d times, want exactly once: %q", helper, count, header)
		}
		positions[index] = strings.Index(header, helper)
	}
	for index := 1; index < len(positions); index++ {
		if positions[index-1] > positions[index] {
			t.Fatalf("helpers rendered out of discovery order: %q", header)
		}
	}
	for _, forbidden := range []string{"RFC", "ADR", "C23", "WG14", "0071"} {
		if strings.Contains(header, forbidden) {
			t.Fatalf("hexal/wrap.h references %q, spec provenance must not leak into generated C", forbidden)
		}
	}
}

// The helpers leave hexal.h: a program with signed wrapping emits
// hexal/wrap.h, hexal.h spells no hex_wrap name, and the using module
// includes the component header after hexal.h.
func TestWrapHelpersLeaveHexalHeader(t *testing.T) {
	program := checkedGeneratorSource(t, "mut value: Int8 := 127 wrapped: Int8 := value + 1\n")
	files := generateOne(t, program)
	if strings.Contains(files["hexal.h"], "hex_wrap") {
		t.Fatalf("hexal.h = %q, wrapping helpers must leave hexal.h", files["hexal.h"])
	}
	wrapH, exists := files["hexal/wrap.h"]
	if !exists {
		t.Fatalf("wrapping program emitted no hexal/wrap.h: %v", files)
	}
	if !strings.Contains(wrapH, "static inline int8_t hex_wrap_add_int8_t(int8_t a, int8_t b)") {
		t.Fatalf("hexal/wrap.h = %q, want the selected Int8 add helper", wrapH)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/wrap.h\"") {
		t.Fatalf("modules/app.h = %q, want the wrap component include", files["modules/app.h"])
	}
}

// A scalar-only program selects no wrap helpers: no hexal/wrap.h artifact is
// emitted, and no module requires the component.
func TestWrapComponentAbsentWithoutSelection(t *testing.T) {
	program := checkedGeneratorSource(t, "x: Int32 := 13\n")
	files := generateOne(t, program)
	if _, exists := files["hexal/wrap.h"]; exists {
		t.Fatalf("scalar-only program emitted hexal/wrap.h: %v", files)
	}
	artifacts, err := renderComponentArtifacts(&programEmission{wrapState: &generatedWrapState{seen: make(map[string]bool)}}, Config{})
	if err != nil {
		t.Fatalf("renderComponentArtifacts() error = %v", err)
	}
	if _, exists := artifacts["hexal/wrap.h"]; exists {
		t.Fatalf("empty wrap selection emitted hexal/wrap.h: %v", artifacts)
	}
	if components := moduleWrapComponent(&moduleEmission{wrapState: &generatedWrapState{seen: make(map[string]bool)}}); components != nil {
		t.Fatalf("module without wrapping selected components %v", components)
	}
}

// Template execution is deterministic for equivalent wrap render models.
func TestWrapHeaderRenderDeterministic(t *testing.T) {
	state := &generatedWrapState{seen: make(map[string]bool)}
	state.order = []wrapOperation{
		{name: "add", typ: compilerTypes.Int32},
		{name: "neg", typ: compilerTypes.Int8},
	}
	first, err := renderComponent(componentArtifact{key: "hexal/wrap.h", template: "wrap.h", model: wrapHeaderModelFor(state)})
	if err != nil {
		t.Fatalf("render error = %v", err)
	}
	second, err := renderComponent(componentArtifact{key: "hexal/wrap.h", template: "wrap.h", model: wrapHeaderModelFor(state)})
	if err != nil {
		t.Fatalf("render error = %v", err)
	}
	if first != second {
		t.Fatal("equivalent wrap models rendered differently")
	}
}

// A render model missing the Helpers field fails closed instead of emitting
// an empty header.
func TestWrapHeaderRenderMissingFieldFailsClosed(t *testing.T) {
	_, err := renderComponent(componentArtifact{key: "hexal/wrap.h", template: "wrap.h", model: struct{}{}})
	if err == nil {
		t.Fatal("render with a model missing the template field must fail")
	}
}

func TestGenerateSignedWrappingBoundaries(t *testing.T) {
	minimum := intSource(compilerTypes.Int8, -128, "128")
	minimum.Negative = true
	typ := compilerTypes.Int8
	program := checker.Program{Statements: []checker.Statement{
		checker.Declaration{Name: "value", Type: typ, Mutable: true, Source: intSource(typ, 127, "127")},
		checker.Declaration{
			Name: "wrapped",
			Type: typ,
			Source: checker.Operand{
				Kind: checker.ExpressionOperand,
				Type: typ,
				Node: binaryExpression(checker.AddOperator, typ, typ, variableNode("value"), constantExpression(intSource(typ, 1, "1"))),
			},
		},
		checker.Declaration{Name: "minimum", Type: typ, Mutable: true, Source: minimum},
		checker.Declaration{
			Name: "underflow",
			Type: typ,
			Source: checker.Operand{
				Kind: checker.ExpressionOperand,
				Type: typ,
				Node: binaryExpression(checker.SubtractOperator, typ, typ, variableNode("minimum"), constantExpression(intSource(typ, 1, "1"))),
			},
		},
		checker.Declaration{
			Name: "negated",
			Type: typ,
			Source: checker.Operand{
				Kind: checker.ExpressionOperand,
				Type: typ,
				Node: unaryExpression(checker.NegateOperator, typ, typ, variableNode("minimum")),
			},
		},
	}}
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	wantWrapped := "hex_wrap_add_int8_t(hex_v_value, 1)"
	if !strings.Contains(rootC, wantWrapped) {
		t.Fatalf("modules/app.c = %q, want the ckd_* wrap helper for Int8 127 + 1", rootC)
	}
	// No unsigned intermediate or reconstruction ternary remains for
	// wrapping arithmetic.
	for _, forbidden := range []string{
		"((uint64_t)(uint8_t)((uint64_t)hex_v_value + (uint64_t)1) <= (uint64_t)INT8_MAX",
		"hex_v_negated = (int8_t)(uint64_t)((uint64_t)0 - (uint64_t)hex_v_minimum);",
	} {
		if strings.Contains(rootC, forbidden) {
			t.Fatalf("modules/app.c contains the removed unsigned-intermediate wrap form %q", forbidden)
		}
	}
	// The program-wide wrap helpers are selected in hexal/wrap.h; hexal.h
	// keeps the <stdckdint.h> prerequisite umbrella but spells no helper.
	wrapH, exists := files["hexal/wrap.h"]
	if !exists {
		t.Fatalf("wrapping program emitted no hexal/wrap.h: %v", files)
	}
	if !strings.Contains(wrapH, "static inline int8_t hex_wrap_add_int8_t(int8_t a, int8_t b)") ||
		!strings.Contains(wrapH, "ckd_add(&r, a, b)") ||
		!strings.Contains(wrapH, "static inline int8_t hex_wrap_neg_int8_t(int8_t a)") {
		t.Fatalf("hexal/wrap.h = %q, want the selected wrap helpers", wrapH)
	}
	hexalH := files["hexal.h"]
	if strings.Contains(hexalH, "hex_wrap") {
		t.Fatalf("hexal.h = %q, wrapping helpers must leave hexal.h", hexalH)
	}
	if !strings.Contains(hexalH, "#include <stdckdint.h>") {
		t.Fatalf("hexal.h = %q, want <stdckdint.h> for the wrap helpers", hexalH)
	}
	if !strings.Contains(rootH, "#include \"hexal/wrap.h\"") {
		t.Fatalf("modules/app.h = %q, want the wrap component include", rootH)
	}
}
