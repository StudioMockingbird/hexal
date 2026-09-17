package compiler

import "testing"

// RuntimeABIVersion is the ABI the checked-in runtime packs declare. A change
// to it is a deliberate re-qualification of every pack, never incidental.
func TestRuntimeABIVersionIsOne(t *testing.T) {
	if RuntimeABIVersion != 1 {
		t.Fatalf("RuntimeABIVersion = %d, want 1", RuntimeABIVersion)
	}
}
