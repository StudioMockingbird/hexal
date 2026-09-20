package integration

// Discarded result-producing calls whose arguments construct an ADT.

import "testing"

// A discarded result-producing call whose argument constructs an ADT compiles.
// It once panicked the generator: the preflight rendered the call with no
// program-wide tag registry.
func TestDiscardedCallWithADTArgument(t *testing.T) {
	assertCompiles(t, "type Shape is union | A as x: Int32 end | B as y: Int32 end end\nfun f(s: Shape): Int32 do\n    return 1\nend\nf(Shape.A(x = 1))\n")
}
