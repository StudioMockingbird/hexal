package modules

import "testing"

func TestEmbeddedModuleMatchesManifest(t *testing.T) {
	if err := Verify(); err != nil {
		t.Fatal(err)
	}
}
