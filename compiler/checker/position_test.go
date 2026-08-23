package checker

import "testing"

// Position eligibility composition: function parameters, function results,
// spawn arguments, and heap allocation go through the shared position model,
// so Atomic and Atomic-containing aggregates are rejected from every
// copy-requiring position.

func TestPositionEligibilityRejectsAtomicInCopyPositions(t *testing.T) {
	requireDiagnostic(t, "fun f(counter: Atomic<Int32>): Int32 do\nreturn 0\nend\n",
		"function parameter Atomic<Int32> is not shallow-copyable")
	requireDiagnostic(t, "type Shared = { count: Atomic<Int32>, }\nfun f(shared: Shared): Int32 do\nreturn 0\nend\n",
		"function parameter Shared is not shallow-copyable")
	requireDiagnostic(t, "fun f(): Atomic<Int32> do\nreturn Atomic<Int32>.new(0)\nend\n",
		"function result Atomic<Int32> is not shallow-copyable")
	requireDiagnostic(t, "type Shared = { count: Atomic<Int32>, }\nfun f(): Shared do\nreturn Shared { count = Atomic<Int32>.new(0) }\nend\n",
		"function result Shared is not shallow-copyable")
	requireDiagnostic(t, "type Shared = { count: Atomic<Int32>, }\nh: Heap := Heap.new()\np: MutPtr<Shared> := h.allocate<Shared>(Shared { count = Atomic<Int32>.new(0) })\n",
		"allocation requires a complete finite type")
}

func TestPositionEligibilityRejectsFunInSpawnArguments(t *testing.T) {
	// Fun<...> is now eligible as both a function parameter and a task-entry
	// argument now accepts Fun values. The spawn check still rejects non-copyable types
	// like Atomic, but not Fun.
	requireAccepted(t, "fun f(x: Int32): Int32 do\nreturn x\nend\nfun apply(callback: Fun<(Int32) : Int32>): Int32 do\nreturn callback(1)\nend\nfun run(): Int32 | Error do\nidentity: Fun<(Int32) : Int32> := f\ntask: Task<Int32> := try spawn apply(identity)\nreturn 0\nend\n")
}

func TestPositionEligibilityRechecksGenericSpecializations(t *testing.T) {
	// A generic function's parameter is an open declaration; its
	// specialization must recheck eligibility with the concrete argument.
	requireDiagnostic(t, "fun identity<T>(value: T): T do\nreturn value\nend\nfun run(): Int32 do\ncounter: Atomic<Int32> := Atomic<Int32>.new(0)\nreturn identity(counter)\nend\n",
		"function parameter Atomic<Int32> is not shallow-copyable")
	requireDiagnostic(t, "type Box<T> = { value: T }\nfun f(box: Box<Atomic<Int32>>): Int32 do\nreturn 0\nend\n",
		"function parameter Box<Atomic<Int32>> is not shallow-copyable")
}
