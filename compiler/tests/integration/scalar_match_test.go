package integration

// Scalar value-mode match: literal arms over integer-like scrutinees.

import (
	"strings"
	"testing"
)

// Every admitted integer-like family accepts a literal arm and a required
// else.
func TestScalarMatchAdmittedFamilies(t *testing.T) {
	cases := []struct {
		name  string
		typ   string
		value string
		arm   string
	}{
		{"Int8", "Int8", "1", "1"},
		{"Int16", "Int16", "1", "1"},
		{"Int32", "Int32", "1", "1"},
		{"Int64", "Int64", "1", "1"},
		{"UInt8", "UInt8", "1", "1"},
		{"Byte", "Byte", "1", "b'A'"},
		{"UInt16", "UInt16", "1", "1"},
		{"UInt32", "UInt32", "1", "1"},
		{"UInt64", "UInt64", "1", "1"},
		{"Size", "Size", "1", "1"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			source := "let op: " + testCase.typ + " = " + testCase.value +
				"\nlet r: Int32 = match op\n| " + testCase.arm + " then 10\n| else then 0\nend\n"
			assertCompiles(t, source)
		})
	}
}

// A scalar match lowers to an ordered if / else-if / else chain so the first
// matching arm wins.
func TestScalarMatchLoweringOrder(t *testing.T) {
	result := assertCompiles(t, "let op: Int32 = 1\nlet r: Int32 = match op\n| 1 then 10\n| 2 then 20\n| else then 0\nend\n")
	body := rootC(t, result)
	if !strings.Contains(body, "hex_match_scrutinee_1 == 1") || !strings.Contains(body, "else if (hex_match_scrutinee_1 == 2)") {
		t.Fatalf("scalar match did not lower to an ordered chain:\n%s", body)
	}
	if !strings.Contains(body, "else {") {
		t.Fatalf("scalar match did not lower its final else:\n%s", body)
	}
	if strings.Count(body, "hex_match_scrutinee_1") < 3 {
		t.Fatalf("scalar match did not evaluate the scrutinee once:\n%s", body)
	}
}

// A duplicate constant after contextual typing is rejected, including across
// spellings.
func TestScalarMatchDuplicateConstants(t *testing.T) {
	assertRejects(t, "let op: Int32 = 1\nlet r: Int32 = match op\n| 1 then 10\n| 0x1 then 20\n| else then 0\nend\n", "duplicate or unreachable match pattern")
	assertRejects(t, "let op: Int32 = 1\nlet r: Int32 = match op\n| 1 then 10\n| 0o1 then 20\n| else then 0\nend\n", "duplicate or unreachable match pattern")
}

// An out-of-range constant is rejected at the pattern with the ordinary
// literal range diagnostic.
func TestScalarMatchOutOfRange(t *testing.T) {
	assertRejects(t, "let op: Int8 = 1\nlet r: Int32 = match op\n| 128 then 10\n| else then 0\nend\n", "given value is outside the Int8 range")
}

// An open integer-like domain always requires a final else.
func TestScalarMatchRequiresElse(t *testing.T) {
	assertRejects(t, "let op: Int32 = 1\nlet r: Int32 = match op\n| 1 then 10\n| 2 then 20\nend\n", "match on Int32 requires a final else")
}

// A negative pattern against an unsigned scrutinee is rejected by the ordinary
// literal path.
func TestScalarMatchNegativeUnsigned(t *testing.T) {
	assertRejects(t, "let op: UInt8 = 1\nlet r: Int32 = match op\n| -1 then 10\n| else then 0\nend\n", "negated integer literal requires a signed destination")
}

// EoS is a closed singleton: `eos` alone is exhaustive, and a following else
// is unreachable.
func TestScalarMatchEosSingleton(t *testing.T) {
	assertCompiles(t, "let marker: EoS = eos\nlet r: Int32 = match marker\n| eos then 0\nend\n")
	assertRejects(t, "let marker: EoS = eos\nlet r: Int32 = match marker\n| eos then 0\n| else then 1\nend\n", "duplicate or unreachable match pattern")
}

// Float and String scrutinees are rejected explicitly, not left as a parse
// error.
func TestScalarMatchRejectsFloatAndString(t *testing.T) {
	assertRejects(t, "let op: Float64 = 1.0\nlet r: Int32 = match op\n| 1 then 10\n| else then 0\nend\n", "match value mode does not support Float64 scrutinees")
	assertRejects(t, "let op: String = \"a\"\nlet r: Int32 = match op\n| 1 then 10\n| else then 0\nend\n", "match value mode does not support String scrutinees")
}

// Bool value mode is unchanged.
func TestScalarMatchBoolUnchanged(t *testing.T) {
	result := assertCompiles(t, "let flag: Bool = true\nlet r: Int32 = match flag\n| true then 10\n| false then 20\nend\n")
	body := rootC(t, result)
	if !strings.Contains(body, "if (hex_match_scrutinee_1)") || !strings.Contains(body, "if (!hex_match_scrutinee_1)") {
		t.Fatalf("Bool value mode lowering changed:\n%s", body)
	}
}

// A scalar pattern introduces no narrowing fact: the matched value keeps its
// declared scalar type inside the arm.
func TestScalarMatchNoNarrowingFact(t *testing.T) {
	result := assertCompiles(t, "let op: Int32 = 1\nlet r: Int32 = match op\n| 1 then op + 1\n| else then op\nend\n")
	if result.ExitCode != 0 {
		t.Fatalf("scalar arm did not read the scrutinee at its declared type")
	}
}

// A Rune pattern is a scalar arm of type Rune; against an Int32 scrutinee it
// is a type mismatch, not a syntax reservation.
func TestScalarMatchRunePatternIsTypeChecked(t *testing.T) {
	assertRejects(t, "let op: Int32 = 1\nlet r: Int32 = match op\n| 'A' then 10\n| else then 0\nend\n", "match pattern does not belong to the scrutinee type")
}

// A Rune scrutinee accepts Rune arms and orders them by scalar value.
func TestScalarMatchRuneScrutinee(t *testing.T) {
	assertCompiles(t, "let op: Rune = 'a'\nlet r: Int32 = match op\n| 'a' then 1\n| 'b' then 2\n| else then 0\nend\n")
}
