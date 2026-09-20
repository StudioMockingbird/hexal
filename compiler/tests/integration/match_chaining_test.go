package integration

// Match arm chaining: a trailing else must not overwrite an earlier arm.

import (
	"strings"
	"testing"
)

// A match with two or more explicit arms and a final else chains its arms with
// else-if, so the first matching arm wins.
func TestMatchArmsChainWithElse(t *testing.T) {
	result := assertCompiles(t, "type Shape is union | A as x: Int32 end | B as y: Int32 end | C as z: Int32 end end\nfun f(s: Shape): Int32 do\n    return match s is\n    | Shape.A then 1\n    | Shape.B then 2\n    | else then 3\n    end\nend\nlet out: Int32 = f(Shape.A(x = 1))\n")
	body := rootC(t, result)
	if !strings.Contains(body, "else if (hex_match_scrutinee_1.tag == hex_tag_m3_app_Shape_B)") {
		t.Fatalf("multi-arm match did not chain with else-if:\n%s", body)
	}
}

// A type-mode union match with two or more members and a final else also
// chains its arms with else-if.
func TestTypeModeUnionArmsChainWithElse(t *testing.T) {
	result := assertCompiles(t, "let value: Int32 | Float64 | Nil = 1\nlet r: Int32 = match value is\n| Int32 then 1\n| Float64 then 2\n| else then 3\nend\n")
	if !strings.Contains(rootC(t, result), "else if") {
		t.Fatalf("type-mode union match did not chain with else-if:\n%s", rootC(t, result))
	}
}

// An ErrorKind match with two or more explicit variants and a final else also
// chains its arms with else-if.
func TestErrorKindArmsChainWithElse(t *testing.T) {
	result := assertCompiles(t, "let err: Error = Error(ErrorKind.Other(header = \"x\"), \"y\")\nlet kind: ErrorKind = err.kind\nlet r: Int32 = match kind is\n| ErrorKind.Other then 1\n| ErrorKind.NotFound then 2\n| else then 3\nend\n")
	if !strings.Contains(rootC(t, result), "else if") {
		t.Fatalf("ErrorKind match did not chain with else-if:\n%s", rootC(t, result))
	}
}

// A match with no else has mutually exclusive arms and keeps its separate
// plain ifs.
func TestMatchWithoutElseKeepsPlainIfs(t *testing.T) {
	result := assertCompiles(t, "let value: Int32 | Float64 = 1\nlet r: Int32 = match value is\n| Int32 then 1\n| Float64 then 0\nend\n")
	if strings.Contains(rootC(t, result), "else if") {
		t.Fatalf("match without else gained an else-if:\n%s", rootC(t, result))
	}
}
