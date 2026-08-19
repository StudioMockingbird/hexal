package checker

import "testing"

// try requires an enclosing function whose result accepts Error, a union
// operand with exactly one Error member, and valid statement or expression
// placement. Parser and integration coverage live in parser/try_test.go and
// compiler/error_test.go; this file proves the checker-level contract
// directly.

func TestCheckerAcceptsTryInErrorResultFunction(t *testing.T) {
	requireAccepted(t, "fun read(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun demo(): Int32 | Error do\n    count: Int32 := try read()\n    return count\nend\n")
	requireAccepted(t, "fun read(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun demo(): Int32 | Error do\n    try read()\n    return 1\nend\n")
	requireAccepted(t, "fun read(): Nil | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun demo(): Int32 | Error do\n    try read()\n    return 1\nend\n")
	requireAccepted(t, "fun read(): Int32 | Float32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun demo(): Int32 | Error do\n    value: Int32 | Float32 := try read()\n    return 1\nend\n")
}

func TestCheckerRejectsTryOutsideErrorResultFunction(t *testing.T) {
	requireDiagnostic(t, "fun read(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\ntry read()\n", "try requires an enclosing function whose result accepts Error")
	requireDiagnostic(t, "fun read(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun demo(): Int32 do\n    value: Int32 := try read()\n    return value\nend\n", "try requires an enclosing function whose result accepts Error")
}

func TestCheckerRejectsTryOnNonErrorUnion(t *testing.T) {
	requireDiagnostic(t, "fun demo(): Int32 | Error do\n    value: Int32 := 1\n    try value\nend\n", "try requires a union containing Error and a success member; got Int32")
	requireDiagnostic(t, "fun demo(): Int32 | Error do\n    try Error.new(\"x\", \"y\")\nend\n", "try requires a union containing Error and a success member; got Error")
}

func TestCheckerRejectsTryInsideCleanupAction(t *testing.T) {
	requireDiagnostic(t, "fun read(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun demo(h: Heap): Int32 | Error do\n    defer try read()\n    return 1\nend\n", "try is not permitted inside defer or errdefer")
	requireDiagnostic(t, "fun read(): Int32 | Error do\n    return Error.new(\"x\", \"y\")\nend\nfun demo(h: Heap): Int32 | Error do\n    errdefer try read()\n    return 1\nend\n", "try is not permitted inside defer or errdefer")
}
