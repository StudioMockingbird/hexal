package checker

import (
	"testing"

	"hexal/compiler/specdata"
)

// The bare-constructor dispatch must read the registry rather than restate which
// types are constructible. With the real registry the program below compiles;
// replacing the lookup with an empty registry must drop the construction out of
// the accepted programs.
func TestBareConstructorDispatchReadsTheRegistry(t *testing.T) {
	source := "let h: Heap = Heap()\nlet values: List<Int32> = List<Int32>(h)\n"
	requireAccepted(t, source)

	original := builtinConstructible
	builtinConstructible = func(string) bool { return false }
	defer func() { builtinConstructible = original }()

	if _, err := checkSource(t, source); err == nil {
		t.Fatal("Check accepted List<Int32>(h) with an empty constructor registry")
	}
}

// TestBareConstructibleMatchesTheDispatch pins the registry's bare-constructor
// set to the names the checker actually dispatches, and to the type names that
// are deliberately constructed through other syntax.
func TestBareConstructibleMatchesTheDispatch(t *testing.T) {
	for _, name := range []string{"Heap", "Stash", "Pool", "List", "Dict", "Channel", "Mutex", "Atomic", "Error"} {
		if !specdata.BareConstructible(name) {
			t.Errorf("BareConstructible(%q) = false, want true for a dispatched canonical constructor", name)
		}
	}
	for _, name := range []string{"String", "Array", "Slice", "Task", "InlineString"} {
		if specdata.BareConstructible(name) {
			t.Errorf("BareConstructible(%q) = true, want false for a type constructed through another syntax", name)
		}
	}
}
