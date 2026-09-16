package generator

import (
	"strings"
	"testing"
)

// The handle registry is the common generation-checked foundation File
// resolves through. Its concurrency invariants are exercised for real by the
// tagged C23 suite through File's own operations; the properties below are
// awkward or impossible to drive to failure from a real program (a kind
// mismatch needs a second capability kind that does not exist yet, and
// generation exhaustion needs 2^64 reuse cycles), so they are checked
// structurally instead, matching how this package already verifies the
// Task park/commit/wake protocol it cannot fully exercise at runtime.

func handleComponentSource(t *testing.T) string {
	t.Helper()
	program := checkedGeneratorSource(t, "import\n    Fs from \"std/fs\"\nend\nfun f(): Nil | Error do\n    x := try Fs.open(\"a\", Fs.FileMode.Read())\n    try x.close()\n    return nil\nend\nr: Nil | Error := f()\n")
	files := generateOne(t, program)
	source, ok := files["hexal/handle.c"]
	if !ok {
		t.Fatalf("File program did not emit hexal/handle.c: %v", files)
	}
	return source
}

// A resolve or close_begin whose slot kind does not match the requested kind
// must fail before touching the control-block pointer: a private second-kind
// fixture would prove this at runtime, but no second capability kind exists
// yet, so this checks the kind comparison precedes every path that reads
// slot->control.
func TestHandleResolveChecksKindBeforeReadingControl(t *testing.T) {
	source := handleComponentSource(t)
	for _, function := range []string{
		"hex_handle_lease hex_handle_resolve(hex_handle handle, hex_handle_kind kind) {",
		"void *hex_handle_close_begin(hex_handle handle, hex_handle_kind kind) {",
	} {
		start := strings.Index(source, function)
		if start < 0 {
			t.Fatalf("hexal/handle.c lacks %q:\n%s", function, source)
		}
		body := source[start:]
		end := strings.Index(body, "\n}")
		if end < 0 {
			t.Fatalf("could not find the end of %q:\n%s", function, source)
		}
		body = body[:end]
		kindCheck := strings.Index(body, "slot->kind != kind")
		controlRead := strings.Index(body, "slot->control")
		if kindCheck < 0 {
			t.Fatalf("%q does not check the capability kind:\n%s", function, body)
		}
		if controlRead >= 0 && controlRead < kindCheck {
			t.Fatalf("%q reads slot->control before checking the capability kind:\n%s", function, body)
		}
	}
}

// Generation exhaustion retires a slot instead of wrapping its counter back
// to a value a stale copy could still match.
func TestHandleRecycleRetiresOnGenerationExhaustion(t *testing.T) {
	source := handleComponentSource(t)
	if !strings.Contains(source, "slot->generation == UINT64_MAX") || !strings.Contains(source, "HEX_HANDLE_RETIRED") {
		t.Fatalf("hexal/handle.c does not retire a slot whose generation would wrap:\n%s", source)
	}
	if strings.Contains(source, "slot->generation = 0") {
		t.Fatalf("hexal/handle.c must never wrap a slot's generation back to a reusable value:\n%s", source)
	}
}

// The registry mutex is scoped to chunk growth and free-slot selection only;
// resolve and release synchronize through the resolved slot's own mutex, so
// ordinary operations never contend on one program-wide lock.
func TestHandleOrdinaryOperationsAvoidRegistryLock(t *testing.T) {
	source := handleComponentSource(t)
	for _, function := range []string{
		"hex_handle_lease hex_handle_resolve(hex_handle handle, hex_handle_kind kind) {",
		"void hex_handle_release(hex_handle_lease lease) {",
	} {
		start := strings.Index(source, function)
		if start < 0 {
			t.Fatalf("hexal/handle.c lacks %q:\n%s", function, source)
		}
		body := source[start:]
		end := strings.Index(body, "\n}")
		body = body[:end]
		if strings.Contains(body, "hex_handle_registry_mutex") {
			t.Fatalf("%q must not touch the registry lock:\n%s", function, body)
		}
	}
}

// A slot published live and later closed is only recycled once every lease
// resolved before the closing transition has released, whichever of the
// closer or a racing operation happens to release last.
func TestHandleRecycleWaitsForInFlightLeasesToDrain(t *testing.T) {
	source := handleComponentSource(t)
	if !strings.Contains(source, "slot->finish_requested && slot->in_flight == 0") {
		t.Fatalf("hexal/handle.c does not defer recycling to the last releasing lease:\n%s", source)
	}
}

func TestHandleComponentAbsentWithoutFile(t *testing.T) {
	program := checkedGeneratorSource(t, "value: Int32 := 1\n")
	files := generateOne(t, program)
	if _, ok := files["hexal/handle.c"]; ok {
		t.Fatalf("a program without File must not select the handle registry: %v", files)
	}
	if _, ok := files["hexal/handle.h"]; ok {
		t.Fatalf("a program without File must not select the handle registry header: %v", files)
	}
}
