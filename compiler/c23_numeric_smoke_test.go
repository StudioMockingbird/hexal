//go:build c23

package compiler

import "testing"

// Smoke-check that conversions, defined arithmetic, Size, for-in, and text
// iteration (RFC 0016/0017/0036/0028) generate C that gcc accepts.
func TestGeneratedNumericAndIterationCCompiles(t *testing.T) {
	source := "fun demo(h: Heap)\n    wide: Int64 = 9_000_000_000\n    narrowed: Int8 = wide.to<Int8>()\n    wrapped: UInt8 = (200).to<UInt8>()\n    whole: Int32 = 3.75.to<Int32>()\n    mut left: Int32 = 7\n    mut right: Int32 = 3\n    quotient: Int32 = left / right\n    remainder: Int32 = left % right\n    fixed: Array<Int32, 3> = [10, 20, 30]\n    mut total: Int32 = 0\n    for value in fixed do\n        total = total + value\n    end\n    for i, value in fixed do\n        total = total + value + i.to<Int32>()\n    end\n    view: View<Int32> = fixed.slice(0, 2)\n    for value in view do\n        total = total + value\n    end\n    text: String = \"cafe\"\n    mut runes: Int32 = 0\n    for rune in text do\n        runes = runes + 1\n    end\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    for value in values do\n        total = total + value\n    end\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    for key, value in scores do\n        total = total + key + value\n    end\n    size: Size = values.length()\nend"
	compileGeneratedC(t, assertCompiles(t, source))
}
