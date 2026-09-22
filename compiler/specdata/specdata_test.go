package specdata

import "testing"

// The registry is validated from a test, never from init, so a bad record fails
// this suite instead of panicking every consumer of the compiler.
func TestValidateAcceptsTheRegistry(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}
