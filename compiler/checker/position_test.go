package checker

import "testing"

// RFC 0049 item 8.4: position eligibility composition. Function parameters,
// function results, spawn arguments, and heap allocation go through the
// shared position model, so Atomic and Atomic-containing aggregates are
// rejected from every copy-requiring position.

func TestPositionEligibilityRejectsAtomicInCopyPositions(t *testing.T) {
	requireDiagnostic(t, "fun f(counter: Atomic<Int32>): Int32 do\nreturn 0\nend\n",
		"function parameter Atomic<Int32> is not shallow-copyable")
	requireDiagnostic(t, "type Shared = { count: Atomic<Int32>, }\nfun f(shared: Shared): Int32 do\nreturn 0\nend\n",
		"function parameter Shared is not shallow-copyable")
	requireDiagnostic(t, "fun f(): Atomic<Int32> do\nreturn Atomic<Int32>.new(0)\nend\n",
		"function result Atomic<Int32> is not shallow-copyable")
	requireDiagnostic(t, "type Shared = { count: Atomic<Int32>, }\nfun f(): Shared do\nreturn Shared { count = Atomic<Int32>.new(0) }\nend\n",
		"function result Shared is not shallow-copyable")
	requireDiagnostic(t, "type Shared = { count: Atomic<Int32>, }\nh: Heap = Heap.new()\np: MutPtr<Shared> = h.allocate<Shared>(Shared { count = Atomic<Int32>.new(0) })\n",
		"allocation requires a complete finite type")
}

func TestPositionEligibilityRejectsFunInSpawnArguments(t *testing.T) {
	// Fun<...> is eligible as a function parameter but not as a task-entry
	// argument, so the spawn check must run in both positions.
	requireDiagnostic(t, "fun f(x: Int32): Int32 do\nreturn x\nend\nfun apply(callback: Fun<(Int32) : Int32>): Int32 do\nreturn callback(1)\nend\nfun run(): Int32 | Error do\nidentity: Fun<(Int32) : Int32> = f\ntask: Task<Int32> = try spawn apply(identity)\nreturn 0\nend\n",
		"task entry arguments must be complete and shallow-copyable")
}

func TestPositionEligibilityRechecksGenericSpecializations(t *testing.T) {
	// A generic function's parameter is an open declaration; its
	// specialization must recheck eligibility with the concrete argument.
	requireDiagnostic(t, "fun identity<T>(value: T): T do\nreturn value\nend\nfun run(): Int32 do\ncounter: Atomic<Int32> = Atomic<Int32>.new(0)\nreturn identity(counter)\nend\n",
		"function parameter Atomic<Int32> is not shallow-copyable")
	requireDiagnostic(t, "type Box<T> = { value: T }\nfun f(box: Box<Atomic<Int32>>): Int32 do\nreturn 0\nend\n",
		"function parameter Box<Atomic<Int32>> is not shallow-copyable")
}
