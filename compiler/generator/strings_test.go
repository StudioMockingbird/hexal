package generator

import "testing"

func TestLiteralRegistryInternsPayloadOnce(t *testing.T) {
	registry := newLiteralRegistry()
	first := registry.Intern("same")
	second := registry.Intern("same")
	if first != second || len(registry.All()) != 1 {
		t.Fatalf("handles = %#v/%#v, payloads = %#v", first, second, registry.All())
	}
	if got := registry.CName(first); got != "hex_lit_0" {
		t.Fatalf("CName = %q, want hex_lit_0", got)
	}
	if got, ok := registry.Lookup("same"); !ok || got != first {
		t.Fatalf("registered lookup = %#v, %v; want %#v, true", got, ok, first)
	}
	if got, ok := registry.Lookup("missing"); ok || got != (literalHandle{}) {
		t.Fatalf("missing lookup = %#v, %v", got, ok)
	}
}

func TestLiteralRegistryPreservesRegistrationOrder(t *testing.T) {
	registry := newLiteralRegistry()
	registry.Intern("first")
	registry.Intern("second")
	registry.Intern("first")
	registry.Intern("third")

	got := registry.All()
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("payloads = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("payloads = %#v, want %#v", got, want)
		}
	}
}
