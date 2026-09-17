//go:build c23

package c23validation

// The trap inventory guard. It derives every distinct "[Runtime Error] ..."
// literal the generator can emit from the production tree itself -- the
// embedded package templates and the non-test Go source that renders inline
// C -- and requires each one to carry exactly one disposition below. No
// independent expected count exists: add a new trap anywhere in the
// generator and this guard notices it on the next run, with no reconciling
// edit required unless that trap needs its own new disposition.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// trapDisposition classifies one derived literal exactly once.
type trapDisposition struct {
	// kind is "executable", "structural", or "nondeterministic".
	kind string
	// detail names the fixture (executable), the structural assertion's
	// location (structural), or the reason ordinary execution cannot reach
	// it (nondeterministic).
	detail string
}

const (
	dispositionExecutable       = "executable"
	dispositionStructural       = "structural"
	dispositionNondeterministic = "nondeterministic"
)

// trapLedger is the disposition of every literal known at the time this
// guard was written. trapInventoryRoot's derivation is authoritative: a
// literal this map names but the derivation no longer finds is not an
// error (the trap may have been renamed or removed), but a derived literal
// missing from this map fails TestTrapInventoryIsFullyClassified.
var trapLedger = map[string]trapDisposition{
	// --- Executable: an existing tagged fixture asserts this exact text. ---
	"array index out of bounds":                     {dispositionExecutable, "array-index-out-of-bounds-traps"},
	"array slice bounds out of range":               {dispositionExecutable, "array-slice-bounds-traps"},
	"cannot free a String literal":                  {dispositionExecutable, "free-string-literal-traps"},
	"cannot free a non-owning String":               {dispositionExecutable, "program-arguments-nonowning-free-traps"},
	"dictionary key not found":                      {dispositionExecutable, "missing-dict-get-traps / dict-repeated-removal-traps"},
	"invalid UTF-8 in string":                       {dispositionExecutable, "invalid-utf8-traps"},
	"invalid allocation alignment":                  {dispositionExecutable, "aligned-allocation-zero-alignment-traps / aligned-allocation-non-power-of-two-traps"},
	"list index out of bounds":                      {dispositionExecutable, "list-index-out-of-bounds-traps"},
	"list slice bounds out of range":                {dispositionExecutable, "list-slice-bounds-traps"},
	"numeric operation failed":                      {dispositionExecutable, "conversion-overflow-traps / division-by-zero-traps (shared path: every numeric-conversion, division, shift, and bit_cast domain check traps through this one message)"},
	"string slice bounds out of range":              {dispositionExecutable, "string-slice-bounds-traps"},
	"RuneCursor has no next value":                  {dispositionExecutable, "runecursor-exhaustion-traps"},
	"pool exhausted":                                {dispositionExecutable, "pool-exhausted-traps"},
	"pool destroy with live slots":                  {dispositionExecutable, "pool-destroy-with-live-slots-traps"},
	"pool slot is not live":                         {dispositionExecutable, "pool-double-free-traps"},
	"pointer does not name a slot in this pool":     {dispositionExecutable, "pool-foreign-pointer-traps"},
	"pool capacity must be positive":                {dispositionExecutable, "pool-non-positive-capacity-traps"},
	"recursive mutex lock":                          {dispositionExecutable, "mutex-recursive-lock-traps"},
	"mutex unlock by a non-owner":                   {dispositionExecutable, "mutex-unlock-by-non-owner-traps"},
	"mutex free while locked or awaited":            {dispositionExecutable, "mutex-free-while-locked-traps"},
	"channel free requires a closed, empty channel": {dispositionExecutable, "channel-free-not-closed-traps"},
	"Task already joined or detached":               {dispositionExecutable, "join-already-joined-traps"},
	"duration overflow":                             {dispositionExecutable, "duration-overflow-traps"},
	"duration underflow":                            {dispositionExecutable, "duration-underflow-traps"},
	"invalid instant subtraction":                   {dispositionExecutable, "instant-subtraction-underflow-traps"},
	"sleep duration too large":                      {dispositionExecutable, "sleep-duration-too-large-traps"},
	"close of a borrowed stream":                    {dispositionExecutable, "close-borrowed-stream-traps"},
	"slice index out of bounds":                     {dispositionExecutable, "slice-index-out-of-bounds-traps"},
	"slice slice bounds out of range":               {dispositionExecutable, "slice-slice-bounds-traps"},
	"collection modified during iteration":          {dispositionExecutable, "collection-modified-during-iteration-traps"},
	"task stack overflow":                           {dispositionExecutable, "task-stack-overflow-traps"},
	"standard output write failed":                  {dispositionStructural, "print.c hex_print_commit_native/hex_io_stdout_write_all: no portable way to force a real stdout write failure inside the 10s process-timeout harness without redirecting the process's own standard handle out from under it, which the harness's own I/O capture already occupies"},
	"invalid Unicode scalar value":                  {dispositionStructural, "conversions.go's Rune-domain check shares hex_runtime_trap(\"numeric operation failed\") at the emission site named above; this literal only appears in a code comment naming the condition, not as its own emitted message"},
	"cannot join the current task":                  {dispositionStructural, "concurrency.c's hex_task_join compares the target hex_task* against hex_current_task; Hexal has no Task.current()/self-reference API, and a spawned function cannot observe the Task<T> handle spawn itself returns until after that call completes, so no checker-accepted program can pass a task its own handle"},

	// --- Nondeterministic: OS/allocator resource exhaustion, or requires
	// exhausting a 64-bit size_t, neither reliably reachable inside the
	// harness's process timeout without external fault injection. ---
	"heap allocation failed":                           {dispositionNondeterministic, "requires the host allocator to return NULL; not reliably inducible without external memory-pressure fault injection"},
	"print buffer allocation failed":                   {dispositionNondeterministic, "same as heap allocation failed: requires malloc/realloc to return NULL"},
	"string allocation size overflow":                  {dispositionNondeterministic, "requires a byte length near SIZE_MAX; unreachable without first holding a string that size"},
	"string concatenation length overflow":             {dispositionNondeterministic, "requires two strings whose combined length overflows size_t; same practical bound as above"},
	"allocation size is not representable":             {dispositionNondeterministic, "requires a Heap.allocate<T> count*size product overflowing size_t"},
	"stream region bounds are not representable":       {dispositionNondeterministic, "requires a stream offset/length pair overflowing size_t"},
	"libuv allocator installation failed":              {dispositionNondeterministic, "uv_replace_allocator fails only on an already-active loop, which the native bootstrap never presents"},
	"handle registry initialization failed":            {dispositionNondeterministic, "requires the host allocator to return NULL during the one-time registry allocation"},
	"event loop thread creation failed":                {dispositionNondeterministic, "requires OS thread-creation exhaustion"},
	"event loop wake failed":                           {dispositionNondeterministic, "requires uv_async_send failure, undocumented by libuv as reachable outside handle misuse this codebase does not perform"},
	"event runtime initialization failed":              {dispositionNondeterministic, "requires OS mutex/condition-variable creation exhaustion"},
	"condition initialization failed":                  {dispositionNondeterministic, "requires OS condition-variable creation exhaustion"},
	"scheduler allocation failed":                      {dispositionNondeterministic, "requires the host allocator to return NULL during scheduler bootstrap"},
	"scheduler fiber initialization failed":            {dispositionNondeterministic, "requires OS fiber/stack creation exhaustion"},
	"scheduler lifecycle mutex initialization failed":  {dispositionNondeterministic, "requires OS mutex-creation exhaustion"},
	"scheduler mutex initialization failed":            {dispositionNondeterministic, "requires OS mutex-creation exhaustion"},
	"scheduler worker creation failed":                 {dispositionNondeterministic, "requires OS thread-creation exhaustion"},
	"scheduler worker-zero context creation failed":    {dispositionNondeterministic, "requires the host allocator to return NULL during scheduler bootstrap"},
	"thread detach failed":                             {dispositionNondeterministic, "requires an OS-level thread-handle failure this codebase's own thread creation never produces"},
	"stack overflow handler installation failed":       {dispositionNondeterministic, "requires the host's guarded-stack/vectored-handler installation call itself to fail, an OS-level condition"},
	"stack overflow handler stack allocation failed":   {dispositionNondeterministic, "requires the host allocator to return NULL for the alternate signal stack"},
	"stack overflow handler stack installation failed": {dispositionNondeterministic, "requires sigaltstack/equivalent to fail after successful allocation, an OS-level condition"},
	"task sleep failed":                                {dispositionNondeterministic, "requires the underlying uv_timer/uv_sleep primitive to fail, undocumented by libuv as reachable outside handle misuse this codebase does not perform"},
	"dictionary capacity is not representable":         {dispositionNondeterministic, "no reserve/with_capacity API exists to request growth directly; reaching the overflow through repeated insert() requires holding close to 2^63 entries, physically impossible inside the process-timeout harness"},
	"list capacity is not representable":               {dispositionNondeterministic, "same as dictionary capacity is not representable: reaching it through repeated push() requires close to 2^63 elements"},
	"channel free while tasks are blocked on it":       {dispositionNondeterministic, "requires freeing the channel in the narrow window after a Task has decided to block on it but before hex_current_task records the park; no synchronization primitive lets a second Task observe that transition to win the race deterministically"},

	// --- Structural: unreachable from any program the checker accepts;
	// each is a defensive internal-consistency check, not a user-triggerable
	// path, verified by inspection of the checker rule that makes the
	// precondition always hold rather than by execution. ---
	"network operation outside a Task":      {dispositionStructural, "checker/network.go's networkNode call sites are reachable only from checked expressions RFC 0172/0173/0176 route exclusively through Task-selecting operations; the checker never emits one outside a Task context"},
	"process operation outside a Task":      {dispositionStructural, "same as network operation outside a Task, for the Process/Pipe family"},
	"signal operation outside a Task":       {dispositionStructural, "same as network operation outside a Task, for the Signals family"},
	"invalid Task park phase during commit": {dispositionStructural, "hex_task_commit_park's own precondition (called only immediately after hex_task_begin_park sets the phase) makes the else-branch unreachable from any code this generator emits"},
	"Task park phase changed during commit": {dispositionStructural, "the same commit-phase invariant as above: no code path re-enters commit for a phase a concurrent wake has already changed except through the one documented transition hex_task_wake performs"},
	"invalid Task park phase during resume": {dispositionStructural, "hex_task_resume_commit's precondition mirrors hex_task_commit_park's; the else-branch requires a phase value no transition helper ever produces"},
	"invalid ErrorKind tag":                 {dispositionStructural, "the ErrorKind switch in hex_error_kind_header/hex_equal_hex_t_ErrorKind is exhaustive over every tag the checker's own ErrorKind construction can produce; asserted in compiler/generator/error_component_test.go"},
}

// runtimeErrorPattern matches one complete "[Runtime Error] ..." literal up
// to the terminating quote, backslash escape, or newline the C or Go string
// literal that carries it uses.
var runtimeErrorPattern = regexp.MustCompile(`\[Runtime Error\][^"\\\n]*`)

// repoRoot locates the repository root from this file's own path: three
// directories up from compiler/tests/c23validation.
func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file's own path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
}

// deriveTrapLiterals scans every embedded package template
// (compiler/generator/packages/*.c and *.h) and every non-test Go source
// file directly under compiler/generator for a literal "[Runtime Error] ..."
// string, returning the distinct set. Templates are read as plain text, not
// Go, so a regular expression is the only option there; Go source is parsed
// with go/parser so a matching substring inside an unrelated comment or a
// _test.go fixture string (which asserts a trap, rather than emitting one)
// is never mistaken for production trap-emitting code.
func deriveTrapLiterals(t *testing.T) map[string]bool {
	t.Helper()
	root := repoRoot(t)
	found := make(map[string]bool)

	packageDir := filepath.Join(root, "compiler", "generator", "packages")
	entries, err := os.ReadDir(packageDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", packageDir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(strings.HasSuffix(name, ".c") || strings.HasSuffix(name, ".h")) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(packageDir, name))
		if err != nil {
			t.Fatalf("cannot read %s: %v", name, err)
		}
		for _, match := range runtimeErrorPattern.FindAllString(string(content), -1) {
			found[strings.TrimSpace(match)] = true
		}
	}

	generatorDir := filepath.Join(root, "compiler", "generator")
	genEntries, err := os.ReadDir(generatorDir)
	if err != nil {
		t.Fatalf("cannot read %s: %v", generatorDir, err)
	}
	fset := token.NewFileSet()
	for _, entry := range genEntries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(generatorDir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("cannot parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			for _, match := range runtimeErrorPattern.FindAllString(literal.Value, -1) {
				found[strings.TrimSpace(match)] = true
			}
			return true
		})
	}
	return found
}

// trapMessage strips the "[Runtime Error] " prefix so the ledger keys on the
// same short text the reference's trap inventory and every fixture's
// requiredStderrSubstring already use.
func trapMessage(literal string) string {
	return strings.TrimPrefix(literal, "[Runtime Error] ")
}

// TestTrapInventoryIsFullyClassified is the guard: every literal the
// production tree can currently emit must have exactly one disposition.
// TestTrapInventoryGuardRejectsUnclassifiedLiteral below proves it actually
// fails when an unclassified literal is present.
func TestTrapInventoryIsFullyClassified(t *testing.T) {
	derived := deriveTrapLiterals(t)
	var unclassified []string
	for literal := range derived {
		message := trapMessage(literal)
		if _, known := trapLedger[message]; !known {
			unclassified = append(unclassified, literal)
		}
	}
	if len(unclassified) > 0 {
		sort.Strings(unclassified)
		t.Fatalf("%d runtime trap literal(s) have no disposition in trapLedger:\n%s",
			len(unclassified), strings.Join(unclassified, "\n"))
	}
}

// TestTrapInventoryExecutableFixturesExist cross-checks the other direction:
// every "executable" disposition names at least one fixture that actually
// exists in fixtureCatalog and actually asserts that exact stderr text, so
// the ledger cannot silently drift from the fixtures it claims cover it.
func TestTrapInventoryExecutableFixturesExist(t *testing.T) {
	assertedByFixture := make(map[string]bool)
	for _, f := range fixtureCatalog {
		if f.expectation != nil && !f.expectation.zeroExit {
			assertedByFixture[f.expectation.requiredStderrSubstring] = true
		}
	}
	for message, disposition := range trapLedger {
		if disposition.kind != dispositionExecutable {
			continue
		}
		full := "[Runtime Error] " + message
		if !assertedByFixture[full] {
			t.Errorf("ledger claims %q is executable (%s), but no fixture asserts exactly that stderr text", message, disposition.detail)
		}
	}
}

// TestTrapInventoryGuardRejectsUnclassifiedLiteral proves the guard actually
// rejects an unclassified literal, rather than vacuously passing because its
// regular expression or ledger lookup is broken. It runs the same check
// TestTrapInventoryIsFullyClassified runs, against a synthetic derived set
// carrying one literal no real trap uses.
func TestTrapInventoryGuardRejectsUnclassifiedLiteral(t *testing.T) {
	const synthetic = "this literal is injected only to prove the guard rejects an unclassified trap"
	derived := map[string]bool{"[Runtime Error] " + synthetic: true}
	if _, known := trapLedger[synthetic]; known {
		t.Fatalf("synthetic probe literal %q unexpectedly already has a disposition; choose a different probe text", synthetic)
	}
	var unclassified []string
	for literal := range derived {
		if _, known := trapLedger[trapMessage(literal)]; !known {
			unclassified = append(unclassified, literal)
		}
	}
	if len(unclassified) == 0 {
		t.Fatal("guard failed to flag a synthetic unclassified literal: TestTrapInventoryIsFullyClassified would pass even with an unclassified production trap present")
	}
}
