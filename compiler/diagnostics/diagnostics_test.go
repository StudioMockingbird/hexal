package diagnostics

import "testing"

func TestUnknownVariableRendersStableIdentityAndLocation(t *testing.T) {
	message := UnknownVariable("score")
	if got, want := Render(message, "app.hex", 3, 7), "[Name Error name.unknown-variable] unknown variable score at app.hex:3:7"; got != want {
		t.Fatalf("rendered diagnostic = %q, want %q", got, want)
	}
}

func TestLocationlessUnknownCompilerHasNoLocation(t *testing.T) {
	if got, want := Render(UnknownCompiler(), "", 0, 0), "[Unknown Error internal.compiler-error] internal compiler error"; got != want {
		t.Fatalf("rendered diagnostic = %q, want %q", got, want)
	}
}

func TestRegisteredDiagnosticsHaveValidRecords(t *testing.T) {
	_ = UnknownVariable("value")
	_ = UnknownCompiler()
	if err := Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryRecordsReachMessagesUnchanged(t *testing.T) {
	for id, registered := range declaredRecords {
		message := message(id, registered.category, registered.stage, "test")
		if message.ID() != id || message.Category() != registered.category || message.Stage() != registered.stage {
			t.Errorf("record %q = (%q, %q), message = (%q, %q, %q)", id, registered.category, registered.stage, message.ID(), message.Category(), message.Stage())
		}
	}
}
