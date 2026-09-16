# RFC 0127: Native Threading Primitives

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented 2026-08-27. `hexal/concurrency.h` declares
  `hex_mutex_raw` (`SRWLOCK` on Windows, `pthread_mutex_t` on POSIX) and
  `hex_cond` (`CONDITION_VARIABLE`/`pthread_cond_t`) before `struct hex_task`,
  which now embeds `hex_mutex_raw lifecycle_mutex` in place of `mtx_t`; the
  `<threads.h>` include and its `__STDC_NO_THREADS__` guard/`#error` are
  gone entirely. `hexal/concurrency.c` implements the ten-operation closed
  vocabulary twice, inside the existing `#if defined(_WIN32)` split: Windows
  uses `InitializeSRWLock`/`AcquireSRWLockExclusive`/`ReleaseSRWLockExclusive`,
  `InitializeConditionVariable`/`SleepConditionVariableSRW`/
  `WakeConditionVariable`/`WakeAllConditionVariable` (checked, trapping on
  `FALSE`), and `_beginthreadex` behind one `unsigned __stdcall` trampoline
  over a private start record freed before the entry runs, closed with
  `CloseHandle` immediately (the running thread is unaffected); POSIX uses
  `pthread_mutex_*`/`pthread_cond_*` (every result checked, an unexpected
  failure trapping through one shared `[Runtime Error] native threading
  operation failed` message) and `pthread_create` with
  `PTHREAD_CREATE_DETACHED` attributes set before creation, never a joinable
  thread detached afterward; ownership of the start record transfers to the
  new thread at a successful `pthread_create` and attribute cleanup after
  that point never reclaims it, including on its own failure. All three
  thread-creation call sites (`hex_scheduler_init`'s worker loop,
  `hex_blocking_init`, and `hex_blocking_call`'s overflow-worker path) now
  call `hex_thread_spawn_detached` directly with no separate detach step and
  no retained handle. `hex_cond_init`'s own two callers (the ready queue and
  the blocking pool) no longer check its result: the function traps
  internally on POSIX and is statically infallible on Windows, so the two
  previous call-site-specific messages ("scheduler condition variable
  initialization failed", "blocking pool condition variable initialization
  failed") are superseded by the one shared native-operation message, an
  intentional reduction in message specificity traded for one centralized
  failure owner. `hex_cond_destroy` is defined in both branches but currently
  unused by any call site (the ready-queue and blocking-pool conds are
  process-lifetime statics, and neither Channel nor Mutex embeds one) and is
  marked `[[maybe_unused]]` rather than given an invented caller, keeping the
  vocabulary a complete, symmetric ten. `docs/reference.md`'s Task section now
  states the verified native primitive per platform in place of the "verified
  C23 `<threads.h>`" clause and its `Unsupported Error` mention, which named a
  diagnostic the compiler never emitted.

  External compilation, the gate this RFC specifies (compiling and linking,
  not executing) rather than a deferred coverage gap: a Task+Channel program
  exercising the ready queue's mutex/cond, `Task.yield()`, join, and
  `hex_thread_spawn_detached` compiled and linked warning-clean under
  `-std=c23 -Wall -Wextra -Werror` (with the pre-existing, separately tracked
  helper-over-emission warnings suppressed, per `docs/status.md`'s standing
  note that the generator emits whole helper families demand-independently)
  on GCC (MinGW-w64 UCRT) and on `zig cc` (bundled Clang 21, standing in for
  both the RFC's Clang and zig cc rows since no standalone `clang` binary was
  available in this environment); the POSIX branch cross-compiled and linked
  clean under `zig cc -target x86_64-linux-gnu`, with and without an explicit
  `-lpthread`. Mutex and the blocking pool could not be exercised through
  real compilation: doing so surfaced three pre-existing defects entirely
  unrelated to this RFC's own file changes -- a Mutex-in-union-payload field
  declared by value instead of by pointer (incomplete-type compile error,
  unlike Task/Channel which already use pointer typedefs), a `print`-only
  program omitting `hexal/list.h` despite `hexal/io.c` needing it (a
  `printUsed`-vs-`ioState.used` gap in the component-merge condition), and an
  invalid `hex_strand header = 0;` scalar initializer for a struct type in
  `hex_io_header`. None are touched by this RFC and none are fixed here;
  each is a previously-undiscovered defect, invisible until something
  actually invoked a toolchain, and is recorded in `docs/status.md`.

  Compiling and running that same Task+Channel program surfaced a fourth,
  far more consequential defect, also pre-existing and also outside this
  RFC's scope: `hex_scheduler_init` never returns, because nothing ever
  pushes the root Task onto the ready queue, so every worker blocks forever
  on its first `hex_cond_wait` and the root statements that would spawn a
  Task never run. Reproduced with the prior C11 primitives' exact call
  sites and ordering (only the primitive types differ), confirming it is a
  scheduler-bootstrap gap in the park/commit/wake protocol, not a
  consequence of this migration. Fixing it is a protocol change this RFC's
  own non-goals exclude ("Reworking the Task park/commit/wake protocol");
  it is recorded in `docs/status.md`'s Unowned section for a future spec.
  `gofmt`, `go build ./...`, `go vet ./...`, `go vet -tags c23 ./...`, and
  `go test -count=1 ./...` all pass; the rebuilt snippet manifest moved
  exactly the fifteen scheduler-selecting snippets' `hexal/concurrency.c`/
  `.h` entries and nothing else. The workbench was rebuilt.
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: replace the generated runtime's dependency on C11 `<threads.h>` with
  the platform primitives the same file already uses for fiber contexts
- Depends on: the implemented M:N Task runtime, the implemented Task
  park/commit/wake protocol, the implemented scheduler-aware blocking pool,
  and `docs/reference.md`
- Coordinates with: RFC 0052 (target profiles), RFC 0055 (build driver), and
  RFC 0125 (external C23 validation), which reproduces the defect this RFC
  fixes
- Changes no Hexal grammar, type, function signature, or result contract
- Accepted cost: one short-lived start record per native thread creation on
  Windows, one generic internal native-operation trap, and platform-local
  pthread attributes for detached creation

## Summary

`hexal/concurrency.c` switches fiber contexts through platform APIs — Windows
Fibers on one side, System V `ucontext` on the other — but reaches for C11
`<threads.h>` for every mutex, condition variable, and OS thread. That header
is optional in C11, absent on every Windows target tested, and its absence is
not detectable through the macro the standard provides for the purpose.

This RFC removes `<threads.h>`. Mutexes, condition variables, and threads join
fiber contexts inside the platform split the file already has.

## Motivation

The generated concurrency runtime does not compile on Windows.

`<threads.h>` is unavailable under MinGW-w64 UCRT, `x86_64-unknown-windows-gnu`,
and `x86_64-pc-windows-msvc` alike. The runtime anticipated this: it guards the
include with `__STDC_NO_THREADS__` and a clear `#error`. The guard never fires,
because a hosted implementation that simply omits the optional header is not
required to define that macro, and none of the three does. The author sees a
bare `threads.h: No such file or directory` instead of the intended diagnostic.

The situation does not improve by waiting. `<threads.h>` was optional in C11 —
that is why `__STDC_NO_THREADS__` exists — and remains something an
implementation may omit. Availability is a property of each target's C library,
not of the language version, so no choice of `-std=` fixes it. Hexal remains on
C23, but that choice does not make the header appear.

The asymmetry that hid the defect is worth naming. The file *looks*
platform-split: its first 260 lines are `#ifdef _WIN32` around fiber creation,
switching, and guard pages. Everything below that line — the ready queue, the
blocking pool, Channel, Mutex, and each Task's lifecycle mutex — silently
assumes a header that half the supported platforms do not ship.

## Decision

### Remove the header, do not detect it

The generated runtime never includes `<threads.h>` and never tests
`__STDC_NO_THREADS__`. Both are deleted.

A conditional implementation that prefers C11 threads where present and falls
back otherwise is rejected. It is three code paths where two suffice, it makes
the common path the one least exercised on the maintainer's own machine, and
its correctness rests on exactly the detection mechanism this RFC exists
because it failed.

### One internal vocabulary, two implementations

The runtime defines its own names and implements them twice, in the platform
split already present in `hexal/concurrency.c`:

```c
typedef struct hex_mutex_raw hex_mutex_raw;
typedef struct hex_cond hex_cond;

static bool hex_mutex_raw_init(hex_mutex_raw *mutex);
static void hex_mutex_raw_lock(hex_mutex_raw *mutex);
static void hex_mutex_raw_unlock(hex_mutex_raw *mutex);
static void hex_mutex_raw_destroy(hex_mutex_raw *mutex);
static void hex_cond_init(hex_cond *cond);              /* traps on failure */
static void hex_cond_wait(hex_cond *cond, hex_mutex_raw *mutex);
static void hex_cond_signal(hex_cond *cond);
static void hex_cond_broadcast(hex_cond *cond);
static void hex_cond_destroy(hex_cond *cond);
static bool hex_thread_spawn_detached(int (*entry)(void *), void *argument);
```

Hexal-owned names, never the standard spellings. Defining `mtx_t` would collide
on every platform that does provide the header, which is the one failure mode a
compatibility layer must not introduce.

The replacement surface is closed and small: two stored primitive types and ten
operations. No thread-handle type exists because every created native thread is
detached before the spawn operation reports success and no caller retains its
handle.

The current runtime uses these standard names:

| Used today | Count |
|---|---|
| `mtx_t`, `cnd_t`, `thrd_t` | 3 types |
| `mtx_init`, `mtx_lock`, `mtx_unlock`, `mtx_destroy` | 4 |
| `cnd_init`, `cnd_wait`, `cnd_signal`, `cnd_broadcast` | 4 |
| `thrd_create`, `thrd_detach` | 2 |
| `mtx_plain`, `thrd_success` | 2 constants |

Nothing else is reachable: no `cnd_timedwait`, no `thrd_join`, no `call_once`,
no `tss_*`, no `mtx_recursive`, and no `mtx_timed`. Only plain mutexes and
untimed waits. The layer above is therefore the whole layer, not a first
instalment.

Implementations may add no capability beyond that table. A timeout, a recursive
mutex, or a joinable thread arrives with the RFC that needs it.

### Platform mapping

| Concept | Windows | POSIX |
|---|---|---|
| mutex | `SRWLOCK` + `AcquireSRWLockExclusive` / `ReleaseSRWLockExclusive` | `pthread_mutex_t` |
| condition variable | `CONDITION_VARIABLE` + `SleepConditionVariableSRW` / `WakeConditionVariable` / `WakeAllConditionVariable` | `pthread_cond_t` |
| thread | `_beginthreadex` then immediate `CloseHandle` | `pthread_create` with `PTHREAD_CREATE_DETACHED` attributes |

`_beginthreadex` rather than `CreateThread`: the runtime calls `malloc` and
`free` on its worker and pool threads, and only `_beginthreadex` initializes
the per-thread CRT state those require. It is declared in `<process.h>` and its
entry point is `unsigned __stdcall`, so the Windows side wraps the shared
`int (*)(void *)` entry in one trampoline. The wrapper allocates one private
start record containing the shared entry and argument. Allocation or
`_beginthreadex` failure frees that record and returns `false`; on success the
trampoline copies both fields, frees the record, calls the shared entry, and
returns its result as `unsigned`. Closing the returned handle immediately does
not stop the running thread.

The POSIX side initializes a local `pthread_attr_t`, selects
`PTHREAD_CREATE_DETACHED`, creates the thread with those attributes, and
destroys the attribute object. Preparation or creation failure returns `false`
before any live thread is published. An attribute-destroy failure after a
successful create is an internal runtime failure and traps; it must not report
spawn failure after a detached thread has begun using the caller's argument.
At successful `pthread_create`, ownership of the argument has already
transferred to the new thread; attribute cleanup never frees, reclaims, or
returns that argument to the caller, including on cleanup failure.

`SRWLOCK` and `CONDITION_VARIABLE` rather than `CRITICAL_SECTION`: both
initialize without allocating, cannot fail to initialize, need no destroy, and
are pointer-sized. That last property matters — see the cost below.
The Windows `hex_mutex_raw_destroy` and `hex_cond_destroy` operations therefore
remain present as empty inline operations so the closed vocabulary is uniform;
they are not omitted and call no native destroy API.

`_Thread_local` is a language keyword, not a `<threads.h>` symbol. It is
supported by every toolchain in RFC 0125's matrix and is unaffected.

### Failure ownership

`InitializeSRWLock` and `InitializeConditionVariable` return `void` and cannot
fail. `pthread_mutex_init` and `pthread_cond_init` can.

`hex_mutex_raw_init` therefore returns `bool`, preserving the existing caller's
failure owner:

- root scheduler and blocking-pool initialization trap;
- spawned Task creation follows its existing `Error` result;
- Channel and Mutex construction follow their existing allocation/construction
  `Error` result; and
- Windows always returns `true` after its infallible initializer.

`hex_cond_init` is used only by program-wide scheduler/pool initialization and
traps on POSIX failure; its Windows branch is statically infallible. These facts
live here and beside the wrappers as local contracts. No terminal specification
is edited.

Every other POSIX operation checks its result. An unexpected lock, unlock,
wait, signal, broadcast, or destroy failure is an internal runtime failure and
traps. The Windows wait wrapper checks `SleepConditionVariableSRW` and traps on
`FALSE`; the untimed wrapper never treats timeout as a normal result. Existing
predicate loops remain mandatory because both condition-variable APIs permit
spurious or stolen wakeups.

All unexpected native-operation failures, including POSIX attribute cleanup
and Windows `CloseHandle`, use one exact diagnostic:
`[Runtime Error] native threading operation failed`. Caller-owned initialization
and thread-creation failures retain their existing Error/trap messages and do
not use this catch-all.

`hex_thread_spawn_detached` returns `bool` because thread creation can fail on
both platforms and the existing callers already have Error paths for it.

## Cost

Per-Task state changes size. `struct hex_task` embeds one lifecycle mutex by
value, so the per-Task cost is the platform primitive's size:

- Windows: one `SRWLOCK`, 8 bytes on a 64-bit target.
- POSIX: one `pthread_mutex_t`, which is what `mtx_t` already resolves to on
  glibc, so the size is unchanged.

At a target of 10,000 concurrently live Tasks the Windows cost is roughly
80 KB, and the POSIX cost is exactly what it is today. Windows moves from
"does not compile" to a smaller per-Task footprint than the header would have
given it.

The scheduler's ready queue and blocking pool each retain one program-wide
primitive. Each runtime Channel and Mutex control retains its own primitive;
their per-instance count and ownership are unchanged.

## Source-language contract

No source surface changes.

- Task, Channel, Mutex, Atomic, `spawn`, `Task.yield()`, join, and detach keep
  their exact signatures, semantics, traps, and Error results.
- The implemented Task park/commit/wake protocol is unchanged. This RFC
  replaces the primitives the protocol is built on, never the protocol.
- The implemented scheduler-aware blocking pool is unchanged for the same
  reason.
- Lock ordering, wake counts, and every ordering rule the park/commit/wake
  protocol establishes hold exactly as written. `SRWLOCK` in exclusive mode and
  `pthread_mutex_t` both provide the mutual exclusion those rules assume.
- No fairness or acquisition-order guarantee is added. Neither platform
  primitive promises one, and no Hexal rule depends on one.
- Source cannot observe which primitive implements a wait.

## Generated-component rules

- The layer lives in `hexal/concurrency.h` and `hexal/concurrency.c`. No new
  component pair is introduced.
- `concurrency.h` declares the two primitive types, because `struct hex_task`
  embeds a lifecycle mutex by value and Channel and Mutex controls embed one
  each. `hex_mutex_raw` is internal storage and is distinct from the
  source-visible `Mutex` control type; neither primitive name appears in
  `hexal.h` as a public language API.
- Windows adds `<windows.h>` and `<process.h>` to the concurrency component's
  include demand; POSIX adds `<pthread.h>`. Neither reaches any other
  component, and `<threads.h>` is demanded by nothing.
- The `__STDC_NO_THREADS__` guard and its `#error` are deleted, not relaxed.
- Selection is unchanged: the layer is emitted exactly when the scheduler
  runtime is emitted, and an `Atomic<T>`-only program still emits no scheduler.
- Repeated compilation produces byte-identical artifacts.

## Non-goals

- Changing the C standard the generated code targets. Hexal-generated
  translation units remain C23; this design does not depend on lowering that
  floor.
- Timeouts, recursive mutexes, joinable threads, thread-local storage APIs,
  reader-writer semantics, or priority control.
- Replacing the fiber context layer, the guard-page handler, or the scheduler
  worker model.
- Reworking the Task park/commit/wake protocol or the blocking pool.
- Adding a third implementation for any other platform.
- Making `Atomic<T>` independent of `<stdatomic.h>`. That header is not
  optional in the way `<threads.h>` is, and every toolchain in RFC 0125's
  matrix provides it.

## Required sweep

- Delete every `<threads.h>` include, the `__STDC_NO_THREADS__` guard, and its
  `#error`.
- Replace every `mtx_t`, `cnd_t`, and `thrd_t` declaration and every
  `mtx_*`, `cnd_*`, and `thrd_*` call with the layer's names. No standard
  spelling survives outside historical specs.
- Keep `_Thread_local` exactly as it is.
- Implement each operation twice, inside the platform split already present;
  do not introduce a second `#ifdef _WIN32` region elsewhere in the file.
- Preserve every lock order, wake count, and ordering rule the Task
  park/commit/wake protocol states.
- Keep runtime C in `compiler/generator/packages/*.c` and `*.h`, not Go
  strings.
- Preserve the POSIX lifecycle-mutex initialization failure path. The Windows
  wrappers have no corresponding branch because the selected native
  initialization operations cannot fail; state that local implementation fact
  beside those wrappers as a CARE comment rather than editing a closed RFC.
- Remove this RFC's Windows-compilation bug from `docs/status.md` only after
  the external compile/link gates pass.

## Implementation plan

### Implementation map

| Area | Required work |
|---|---|
| `compiler/generator/packages/concurrency.h` | Remove `<threads.h>` and its guard; declare the closed Hexal-owned threading vocabulary and platform-backed embedded types. |
| `compiler/generator/packages/concurrency.c` | Implement both native branches in the existing platform region; migrate ready queue, blocking pool, Channel, Mutex, Task lifecycle, and detached-thread creation without changing their protocols. |
| `compiler/generator/concurrency_component.go` | Preserve demand-driven component rendering and provide only the platform-dependent template data actually needed by the package files. |
| `compiler/generator/concurrency_component_test.go` | Assert exact platform includes, two implementations, complete call-site migration, absence of C11 thread spellings, and unchanged component selection. |
| existing generator/integration tests | Preserve Task, Channel, Mutex, IO, failure, ordering, and deterministic-output contracts; update expected C only where the primitive spellings legitimately change. |
| snippet manifest | Rebaseline only snippets selecting the scheduler runtime and prove every other existing artifact hash is unchanged. |
| `docs/reference.md` and `docs/status.md` | Replace the obsolete verified-`<threads.h>` target contract with the native Windows/POSIX primitive contract, then remove the owned Windows compilation bug after external gates pass. |

### Phase 0: baseline and inventory

1. Record the green test/vet baseline and snippet manifest.
2. Record artifacts for Task-only, Channel-only, Mutex-only, Atomic-only,
   Task-plus-IO, and no-concurrency programs.
3. Confirm the symbol table above against the current tree; any symbol outside
   it blocks the migration until the table is corrected.
4. Record that generated concurrency programs do not compile on any Windows
   target, and that `zig cc -target x86_64-linux-gnu` is the one configuration
   in which they do. That is the before state this RFC must improve on.

### Phase 1: the layer

1. Add the two stored primitive types and ten operations to
   `packages/concurrency.h` and
   `packages/concurrency.c`, each implemented in both branches of the existing
   platform split.
2. Add the Windows start record and `unsigned __stdcall` trampoline over the
   shared entry signature, with the allocation/ownership rules above.
3. Add POSIX detached-at-creation attributes; do not create a joinable thread
   and detach it afterward.
4. Add component tests asserting both implementations exist, that neither
   standard spelling appears, and that the include demand differs per platform.

### Phase 2: migrate every site

1. Migrate the ready-queue mutex and condition variable.
2. Migrate the blocking-pool mutex and condition variable.
3. Migrate Channel and Mutex control state.
4. Migrate `struct hex_task`'s lifecycle mutex.
5. Migrate all three thread-creation sites to `hex_thread_spawn_detached`,
   preserving their existing failure handling.
6. Delete the guard, the `#error`, and the include.
7. Check every fallible POSIX operation and Windows condition wait, preserving
   caller-owned initialization failures and trapping internal operation
   failures.

### Phase 3: conformance

1. Implement every validation item below and no additional behavior.
2. Rebuild the snippet manifest once; movement is confined to programs
   selecting the scheduler runtime.
3. Compile a generated concurrency program with each toolchain on the host, and
   record the result. This RFC owns the focused one-off commands needed for its
   own gate; RFC 0125 later makes the host matrix a permanent reusable harness,
   so neither RFC waits on the other.
4. Compile and link the POSIX branch once through
   `zig cc -target x86_64-linux-gnu`. This RFC rewrites POSIX threading code,
   and on a Windows machine nothing else can compile it; landing that branch
   with no compiler having read it repeats the defect this RFC exists to fix.
   This is a one-off implementation check, not a suite capability: RFC 0125 is
   host-only and adds no cross-target tier.
5. Run `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...`.
6. Rebuild and restart the workbench.
7. Update `docs/reference.md` after behavior stabilizes: supported Task targets
   use the verified native Windows and POSIX primitives defined here, not C23
   `<threads.h>`.
8. Remove this RFC from open status and clear its bug entries only after code,
   tests, artifacts, and canonical docs agree.

## Validation

This section is exhaustive.

### Compiler and generated-text validation

- No generated artifact contains `<threads.h>`, `__STDC_NO_THREADS__`,
  `mtx_t`, `cnd_t`, `thrd_t`, or any `mtx_`, `cnd_`, or `thrd_` prefixed call.
- The two stored primitive types and ten operations are each defined exactly
  twice, once per
  platform branch, inside the existing `#ifdef _WIN32` split.
- The Windows branch names `SRWLOCK`, `CONDITION_VARIABLE`,
  `SleepConditionVariableSRW`, and `_beginthreadex`, and declares one
  `unsigned __stdcall` trampoline. Its start record is freed on every failure
  and success path exactly once, and no closed thread handle is retained.
- Windows destroy wrappers exist as empty operations in the platform branch;
  no native destroy call is invented for SRW locks or condition variables.
- Every `SleepConditionVariableSRW` result is checked; `FALSE` traps through
  the native-operation catch-all. The wrapper is untimed and has no timeout
  result to reinterpret.
- The POSIX branch names `pthread_mutex_t`, `pthread_cond_t`,
  `pthread_attr_setdetachstate`, `PTHREAD_CREATE_DETACHED`, and
  `pthread_create`; it does not call `pthread_detach`.
- After successful `pthread_create`, the new detached thread exclusively owns
  its argument even if `pthread_attr_destroy` subsequently traps.
- Windows adds `<windows.h>` and `<process.h>` to concurrency include demand;
  POSIX adds `<pthread.h>`. No other component's demand changes.
- `_Thread_local` still appears and is unchanged.
- `struct hex_task` embeds exactly one lifecycle mutex of the layer's type.
- Every ready-queue, blocking-pool, Channel, Mutex, and lifecycle acquisition
  present before the migration is present after it, in the same order.
- Thread creation reaches `hex_thread_spawn_detached` at exactly three sites,
  and each retains its existing failure handling.
- Mutex initialization returns `bool` and each caller preserves its existing
  trap-or-Error owner; condition initialization traps; thread spawning returns
  `bool`. Every other fallible native operation is checked and traps on an
  internal failure.
- The native-operation catch-all appears exactly once as the defined message
  above; no standard API failure is silently discarded.
- Every Channel, Mutex, and scheduler/pool wait remains in a predicate loop
  under the corresponding mutex, so a spurious wake cannot bypass its state
  check.
- Task-only, Channel-only, Mutex-only, and Task-plus-IO programs emit the
  layer; an `Atomic<T>`-only program emits no scheduler and no layer.
- Unaffected manifest entries remain byte-identical; changed entries are
  limited to programs selecting the scheduler runtime.
- Repeated compilation produces byte-identical artifacts.
- Ordinary tests remain pure Go.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

### External compilation

This RFC is the first whose completion is checkable by compiling generated C,
so these are gates rather than coverage gaps:

- A generated concurrency program compiles and links under GCC on Windows.
- The same program compiles and links under Clang and under `zig cc`.
- The POSIX branch compiles and links once through
  `zig cc -target x86_64-linux-gnu`, the configuration that works today and
  must not regress. This is a one-off gate for this RFC, not a tier RFC 0125
  inherits; that suite is host-only.
- POSIX compilation and linking use the toolchain's pthread option; a driver
  that omits that option does not satisfy this component's link contract.
- No toolchain in the matrix reports a warning under
  `-std=c23 -Wall -Wextra -Werror`.

### Canonical documentation

The reference edit is a completion gate, not a follow-up. This RFC falsifies a
normative sentence, so leaving it unedited ships a false contract:

- `docs/reference.md` no longer conditions supported Task targets on "verified
  C23 `<threads.h>`", and states the Windows and POSIX primitive contracts
  instead.
- The same sentence's `Unsupported Error` clause is gone. That diagnostic does
  not exist in the compiler and never did -- no non-test Go emits it -- so the
  clause was already false before this RFC and must not survive the edit that
  touches its sentence. Removing it changes no behavior because no behavior
  implements it.
- No other Task, Channel, Mutex, Atomic, scheduling, or synchronization rule in
  `docs/reference.md` changes wording. This RFC replaces the primitives beneath
  those contracts, never the contracts.

### Runtime behavior retained as a coverage gap

Ordinary tests cannot execute generated C, so these remain unverified until
RFC 0055 or RFC 0125 can run programs, with one exception recorded honestly
rather than left as merely unverified: a debug build compiled and run under
GCC on Windows during this RFC's own external-compilation gate showed that
`hex_scheduler_init` never returns. It switches from the root fiber into
worker zero's dispatch loop without ever pushing the root Task onto the ready
queue, so every worker blocks forever waiting for a ready task that never
arrives, and the root statements that would spawn one never run. This is a
pre-existing defect in the park/commit/wake protocol's bootstrap, independent
of the primitive migration -- the same call sites and ordering reproduce it
under the prior C11 primitives -- and is tracked as its own item in
`docs/status.md` rather than fixed here, since a correct fix is a protocol
change this RFC's non-goals exclude. Compiling and linking, the gate this RFC
actually specifies, is unaffected: the defect is in scheduler control flow
this RFC does not touch, not in generated syntax or types.

- The Task park/commit/wake orderings behave identically on both primitives.
- A contended Mutex, a full Channel, and a saturated blocking pool each block
  and wake correctly on Windows.
- Detached worker and pool threads terminate with the process under the
  existing lifetime rule.
- Thread-creation failure reaches the existing Error path on both platforms.
- POSIX condition initialization and internal-operation failure paths are
  present textually. Runtime fault injection for those native APIs remains a
  coverage gap; caller-owned mutex initialization paths are verified by their
  generated branches rather than claimed as externally forced failures.

## Reference synchronization

Required, not deferred: the Validation gate above depends on it. Replace the
statement that supported Task targets require verified C23 `<threads.h>`, and
delete its `Unsupported Error` clause, which names a diagnostic the compiler
has never emitted. State instead that Windows x64 uses
verified SRW locks, condition variables, and `_beginthreadex`, while POSIX
x86-64 uses verified pthread mutexes, condition variables, and detached
threads. Task, Channel, Mutex, Atomic, scheduling, and synchronization semantics
remain unchanged.
