//go:build c23

package c23validation

import "testing"

// Smoke-check that RFC 0029 Error/try/errdefer programs generate C that gcc
// accepts.
func c23GeneratedErrorCCompiles(t *testing.T) {
	source := "fun cleanup(value: Int32) do\nend\nfun read_count(): Int32 | Error do\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(release: Bool): Int32 | Error do\n    errdefer cleanup(1)\n    defer cleanup(2)\n    mut total: Int32 = 0\n    while true do\n        count: Int32 = try read_count()\n        total = total + count\n        break\n    end\n    if release then\n        return Error.new(\"Final Error\", \"done\")\n    end\n    return total\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}

// RFC 0029 runtime conformance: a try success unwinds defer statements in
// reverse registration order while skipping errdefer, and a try failure
// unwinds both sets in reverse order before the Error propagates.
func c23GeneratedErrorControlFlowRuns(t *testing.T) {
	success := "fun cleanup(label: Int32) do\n    print(label)\nend\nfun ok_read(): Int32 | Error do\n    return 4\nend\nfun succeed(): Int32 | Error do\n    errdefer cleanup(3)\n    defer cleanup(2)\n    defer cleanup(1)\n    count: Int32 = try ok_read()\n    return count\nend\nfun report(): Bool do\n    outcome: Int32 | Error = succeed()\n    result: Bool = match outcome is\n    | Int32 then\n        outcome == 4\n    | Error then\n        false\n    end\n    return result\nend\nprint(report())\n"
	if got := runGeneratedC(t, assertCompiles(t, success)); got != "12true" {
		t.Fatalf("success-path output = %q, want %q", got, "12true")
	}
	failure := "fun cleanup(label: Int32) do\n    print(label)\nend\nfun read_count(): Int32 | Error do\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun swallow(): Bool do\n    print(\"!\")\n    return false\nend\nfun inner(): Int32 | Error do\n    errdefer cleanup(3)\n    defer cleanup(2)\n    defer cleanup(1)\n    count: Int32 = try read_count()\n    return count\nend\nfun outer(): Bool do\n    outcome: Int32 | Error = inner()\n    result: Bool = match outcome is\n    | Error then\n        swallow()\n    | Int32 then\n        outcome == 0\n    end\n    return result\nend\nprint(outer())\n"
	if got := runGeneratedC(t, assertCompiles(t, failure)); got != "123!false" {
		t.Fatalf("failure-path output = %q, want %q", got, "123!false")
	}
}
