package integration

// Union widening over the pointer-null niche.

import "testing"

// Widening a nullable pointer to a nullable Ptr<Unknown> is the identity over
// the niche representation; it once failed in generation because the niche
// source union has no tag registry entry.
func TestNullablePointerWidensToUnknown(t *testing.T) {
	assertCompiles(t, "let mut b: Byte = 1\nlet p: Ptr<Byte> | Nil = @b\nlet q: Ptr<Unknown> | Nil = p\n")
}
