# RFC 0127: Native Threading Primitives

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: replace the generated runtime's dependency on C11 `<threads.h>` with
  the platform primitives the same file already uses for fiber contexts
- Depends on: the implemented M:N Task runtime, RFC 0122 (safe Task parking and
  reclamation), RFC 0121 (scheduler-aware blocking pool), and
  `docs/reference.md`
- Coordinates with: RFC 0052 (target profiles), RFC 0055 (build driver), and
  RFC 0125 (external C23 validation), which reproduces the defect this RFC
  fixes
- Changes no Hexal grammar, type, function signature, or result contract

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
not of the language version, so no choice of `-std=` fixes it. RFC 0128's move
to C11 does not fix it either; the two RFCs are independent.

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
typedef struct hex_thread hex_thread;

static void hex_mutex_raw_init(hex_mutex_raw *mutex);   /* traps on failure */
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

The surface is closed and small. The current runtime uses exactly three types
and ten functions:

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
| thread | `_beginthreadex` then `CloseHandle` | `pthread_create` then `pthread_detach` |

`_beginthreadex` rather than `CreateThread`: the runtime calls `malloc` and
`free` on its worker and pool threads, and only `_beginthreadex` initializes
the per-thread CRT state those require. It is declared in `<process.h>` and its
entry point is `unsigned __stdcall`, so the Windows side wraps the shared
`int (*)(void *)` entry in one trampoline.

`SRWLOCK` and `CONDITION_VARIABLE` rather than `CRITICAL_SECTION`: both
initialize without allocating, cannot fail to initialize, need no destroy, and
are pointer-sized. That last property matters — see the cost below.

`_Thread_local` is a language keyword, not a `<threads.h>` symbol. It is
supported by every toolchain in RFC 0125's matrix and is unaffected.

### Initialization failure

`InitializeSRWLock` and `InitializeConditionVariable` return `void` and cannot
fail. `pthread_mutex_init` and `pthread_cond_init` can.

The layer therefore traps on failure rather than reporting it, and the trap is
unreachable on Windows by construction. This is a real consequence for RFC
0122, which specifies that "lifecycle-mutex initialization failure follows the
existing owner: root initialization traps; spawned Task creation fails through
its existing Error path". That failure path remains live on POSIX and becomes
statically unreachable on Windows. RFC 0122's Failure behavior gains one
sentence saying so; no behavior is removed on the platform where it can occur.

`hex_thread_spawn_detached` returns `bool` because thread creation can fail on
both platforms and the existing callers already have Error paths for it.

## Cost

Per-Task state changes size. `struct hex_task` embeds one lifecycle mutex by
value, so the per-Task cost is the platform primitive's size:

- Windows: one `SRWLOCK`, 8 bytes on a 64-bit target.
- POSIX: one `pthread_mutex_t`, which is what `mtx_t` already resolves to on
  glibc, so the size is unchanged.

At RFC 0085's stated target of 10,000 concurrently live Tasks the Windows cost
is roughly 80 KB, and the POSIX cost is exactly what it is today. Windows moves
from "does not compile" to a smaller per-Task footprint than the header would
have given it.

The scheduler's own state gains nothing: the ready-queue, blocking-pool,
Channel, and Mutex primitives are one instance each.

## Source-language contract

No source surface changes.

- Task, Channel, Mutex, Atomic, `spawn`, `Task.yield()`, join, and detach keep
  their exact signatures, semantics, traps, and Error results.
- RFC 0122's park/commit/wake protocol is unchanged. This RFC replaces the
  primitives the protocol is built on, never the protocol.
- RFC 0121's blocking pool is unchanged for the same reason.
- Lock ordering, wake counts, and every ordering rule in RFC 0122 hold exactly
  as written. `SRWLOCK` in exclusive mode and `pthread_mutex_t` both provide
  the mutual exclusion those rules assume.
- No fairness or acquisition-order guarantee is added. Neither platform
  primitive promises one, and no Hexal rule depends on one.
- Source cannot observe which primitive implements a wait.

## Generated-component rules

- The layer lives in `hexal/concurrency.h` and `hexal/concurrency.c`. No new
  component pair is introduced.
- `concurrency.h` declares the three types, because `struct hex_task` embeds a
  lifecycle mutex by value and Channel and Mutex controls embed one each.
- Windows adds `<windows.h>` and `<process.h>` to the concurrency component's
  include demand; POSIX adds `<pthread.h>`. Neither reaches any other
  component, and `<threads.h>` is demanded by nothing.
- The `__STDC_NO_THREADS__` guard and its `#error` are deleted, not relaxed.
- Selection is unchanged: the layer is emitted exactly when the scheduler
  runtime is emitted, and an `Atomic<T>`-only program still emits no scheduler.
- Repeated compilation produces byte-identical artifacts.

## Non-goals

- Changing the C standard the generated code targets. RFC 0128 owns that, and
  neither RFC depends on the other.
- Timeouts, recursive mutexes, joinable threads, thread-local storage APIs,
  reader-writer semantics, or priority control.
- Replacing the fiber context layer, the guard-page handler, or the scheduler
  worker model.
- Reworking RFC 0122's parking protocol or RFC 0121's pool.
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
- Preserve every lock order, wake count, and ordering rule RFC 0122 states.
- Keep runtime C in `compiler/generator/packages/*.c` and `*.h`, not Go
  strings.
- Update RFC 0122's Failure behavior with the Windows unreachability of
  lifecycle-mutex initialization failure.
- Move the open bug this RFC fixes off RFC 0125 in `docs/status.md`.

## Implementation plan

### Phase 0: baseline and inventory

1. Record the green test/vet baseline and snippet manifest.
2. Record artifacts for Task-only, Channel-only, Mutex-only, Atomic-only,
   Task-plus-IO, and no-concurrency programs.
3. Confirm the symbol table above against the current tree; any symbol outside
   it blocks the migration until the table is corrected.
4. Record that generated concurrency programs do not compile on any Windows
   target, and that `zig cc -target x86_64-linux-gnu` is the one configuration
   in which they do, as the before state RFC 0125's matrix measures against.

### Phase 1: the layer

1. Add the three types and ten operations to `packages/concurrency.h` and
   `packages/concurrency.c`, each implemented in both branches of the existing
   platform split.
2. Add the Windows `unsigned __stdcall` trampoline over the shared entry
   signature.
3. Add component tests asserting both implementations exist, that neither
   standard spelling appears, and that the include demand differs per platform.

### Phase 2: migrate every site

1. Migrate the ready-queue mutex and condition variable.
2. Migrate the blocking-pool mutex and condition variable.
3. Migrate Channel and Mutex control state.
4. Migrate `struct hex_task`'s lifecycle mutex.
5. Migrate all three thread-creation sites to `hex_thread_spawn_detached`,
   preserving their existing failure handling.
6. Delete the guard, the `#error`, and the include.

### Phase 3: conformance

1. Implement every validation item below and no additional behavior.
2. Rebuild the snippet manifest once; movement is confined to programs
   selecting the scheduler runtime.
3. Compile a generated concurrency program with each toolchain in RFC 0125's
   matrix, on the host and on at least one cross target, and record the result.
4. Run `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...`.
5. Rebuild and restart the workbench.
6. Update `docs/reference.md` only if implementation exposes a genuine
   mismatch; this RFC introduces no language rule.
7. Remove this RFC from open status and clear its bug entry only after code,
   tests, artifacts, and canonical docs agree.

## Validation

This section is exhaustive.

### Compiler and generated-text validation

- No generated artifact contains `<threads.h>`, `__STDC_NO_THREADS__`,
  `mtx_t`, `cnd_t`, `thrd_t`, or any `mtx_`, `cnd_`, or `thrd_` prefixed call.
- The three types and ten operations are each defined exactly twice, once per
  platform branch, inside the existing `#ifdef _WIN32` split.
- The Windows branch names `SRWLOCK`, `CONDITION_VARIABLE`,
  `SleepConditionVariableSRW`, and `_beginthreadex`, and declares one
  `unsigned __stdcall` trampoline.
- The POSIX branch names `pthread_mutex_t`, `pthread_cond_t`, and
  `pthread_create` followed by `pthread_detach`.
- Windows adds `<windows.h>` and `<process.h>` to concurrency include demand;
  POSIX adds `<pthread.h>`. No other component's demand changes.
- `_Thread_local` still appears and is unchanged.
- `struct hex_task` embeds exactly one lifecycle mutex of the layer's type.
- Every ready-queue, blocking-pool, Channel, Mutex, and lifecycle acquisition
  present before the migration is present after it, in the same order.
- Thread creation reaches `hex_thread_spawn_detached` at exactly three sites,
  and each retains its existing failure handling.
- Initialization traps on failure and returns no status; thread spawning
  returns `bool`.
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
- The same program still compiles and links for a POSIX target through
  `zig cc -target x86_64-linux-gnu`, which is the configuration that works
  today and must not regress.
- No toolchain in the matrix reports a warning under
  `-std=c23 -Wall -Wextra -Werror`.

### Runtime behavior retained as a coverage gap

Ordinary tests cannot execute generated C, so these remain unverified until
RFC 0055 or RFC 0125 can run programs:

- RFC 0122's park/commit/wake orderings behave identically on both primitives.
- A contended Mutex, a full Channel, and a saturated blocking pool each block
  and wake correctly on Windows.
- Detached worker and pool threads terminate with the process under the
  existing lifetime rule.
- Thread-creation failure reaches the existing Error path on both platforms.
- POSIX mutex or condition-variable initialization failure traps.

## Reference synchronization

`docs/reference.md` describes Task, Channel, Mutex, and Atomic semantics, not
the primitives beneath them, and this RFC changes none of those. Verify after
implementation that the preserved contracts still read correctly and record
that no reference edit was required; edit only if implementation exposes a
genuine mismatch.
