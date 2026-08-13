//go:build c23

package compiler

import "testing"

// Smoke-check that RFC 0029 Error/try/errdefer programs generate C that gcc
// accepts.
func TestGeneratedErrorCCompiles(t *testing.T) {
	source := "fun cleanup(value: Int32)\nend\nfun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(release: Bool): Int32 | Error\n    errdefer cleanup(1)\n    defer cleanup(2)\n    mut total: Int32 = 0\n    while true do\n        count: Int32 = try read_count()\n        total = total + count\n        break\n    end\n    if release\n        return Error.new(\"Final Error\", \"done\")\n    end\n    return total\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}
