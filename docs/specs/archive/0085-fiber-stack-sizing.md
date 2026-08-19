# RFC 0085: Fiber Stack Sizing

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; verified 2026-08-19. The POSIX fiber stack now maps
  the whole reserve read-write with `MAP_NORESERVE`, mprotects one guard page
  at its low end, and names the reserve-less-one-page region in
  `ss_sp`/`ss_size`; the initial commit is documented as a Windows-only knob
  at that site. The overflow trap lands on both platforms: POSIX installs a
  process-wide `SIGSEGV`/`SIGBUS` handler on a per-worker `sigaltstack`
  region, fed by a thread-local guard range the scheduler updates on every
  context switch; Windows installs a vectored handler on
  `EXCEPTION_STACK_OVERFLOW`. Both write `[Runtime Error] task stack
  overflow` and exit, and re-raise non-guard faults unchanged
  (`EXCEPTION_CONTINUE_SEARCH`). The compile-and-link check against a strict
  `-std=c23` glibc x86-64 toolchain (zig cc, both default and 64 KiB reserve)
  exposed a pre-existing defect: `hex_scheduler_init` and `hex_root_task`
  were `static` in `hexal/concurrency.c` while generated `main` calls them,
  so no Task program could ever link; both are now extern and declared in the
  component header. The whole generated concurrency runtime compiles and
  links under that toolchain. The runnable validation items — resident cost,
  10,000 live Tasks, trap firing, the re-raise, overflow-sooner, and
  the >8 KiB commit usage — could not be executed on this machine (no local
  toolchain provides C23 `<threads.h>` for Windows, no POSIX environment
  exists) and are recorded as unverified in `docs/status.md`.
- Created: 2026-08-19
- Scope: how much memory one Task costs, and how a Task stack grows
- Depends on: RFC 0084 C3 (POSIX guard page) — this RFC extends the same
  allocation site and should land after it. RFC 0086 (project configuration)
  supplies the two size fields; until it lands they may be constants with the
  same defaults.
- Coordinates with: `docs/reference.md` (Tasks), `docs/status.md`
- Does not change: the M:N scheduler, `spawn`/`join`/`detach` semantics,
  `Task.yield()`, or any language surface

## Summary

The goal is goroutine-scale concurrency: spawn thousands of Tasks, each costing
kilobytes rather than a megabyte, with the stack growing on demand.

**The goroutine mechanism itself is not available to Hexal.** Go grows stacks by
copying them and rewriting every pointer that referenced the old stack. That
requires precise stack maps the compiler emits for every frame. Hexal generates
C, so the C compiler owns frame layout, and Hexal cannot know which words in a
frame are pointers.

Worse, it would be unsound even with that knowledge: `ref place` yields
`Ptr`/`MutPtr` into local storage (`reference.md:430`), and a root-level `View`
binding is a local (`:741`). Moving a stack invalidates every such pointer, with
no way to find and fix them.

This RFC therefore specifies how to achieve the **observable properties** — small
per-Task cost, automatic growth, thousands of live Tasks — by a mechanism that
works when generating C.

## What a Task costs today

Per `hex_task_spawn`, each Task allocates:

- the `hex_task` control block (`calloc`)
- an argument frame (`malloc`, sized to the spawn arguments)
- a result frame (`malloc`, sized to R)
- a **1 MiB stack** (`malloc` on POSIX; `CreateFiberEx` default on Windows)

The 1 MiB figure is the one to change.

### The resident cost is already lower than it looks

On Linux, `malloc(1 MiB)` exceeds glibc's mmap threshold, so it becomes an
anonymous mapping whose pages are demand-zero. Untouched pages consume no
physical memory. A Task that uses 3 KiB of stack has roughly 3 KiB resident, not
1 MiB. Windows `CreateFiberEx(0, 0, ...)` reserves from the PE header and commits
on demand, which is the same behaviour by design.

**So the present problem is address space and safety, not resident memory:**
1 MiB × N of virtual address space per program, and — on POSIX — no guard page,
so an overflow corrupts adjacent heap silently (RFC 0084 C3).

This matters because it changes what the fix must accomplish. Measure resident
cost per Task before and after; if it is already page-sized, the win is in
address space, diagnosability, and the reserve ceiling rather than in RSS.

## Options

### Option A — copying stacks, as Go does

Rejected, not deferred. It requires rewriting pointers into the moved stack.
Hexal cannot enumerate them: C owns the frames, and `ref` to a local is a
supported, documented operation. Any implementation would silently corrupt
programs that take an address of a local — which is most systems code.

### Option B — segmented stacks

Allocate a fresh non-contiguous segment when the current one runs low and link
them. No copying, so existing pointers stay valid.

Costs, both real:

- **A stack-limit check in every generated function prologue.** Hexal emits the
  C, so this is expressible — but it is per-call runtime overhead in every
  function, against `AGENTS.md` goal 15.
- **The hot-split problem.** A loop whose call crosses a segment boundary
  allocates and frees a segment per iteration. This is the specific reason Go
  abandoned segmented stacks in 1.4, and it is a performance cliff invisible in
  the source.

Possible, and the cure is worse than the disease.

### Option C — reserve large, commit small (recommended)

Keep one contiguous mapping per Task, sized to the reserve, whose pages become
resident only as they are touched.

**The two platforms reach this differently, and conflating them produces a
broken construction.** An earlier revision of this RFC specified

```
mmap(reserve, PROT_NONE, ...)                       /* WRONG */
mprotect(usable_base, initial_commit, PROT_READ|PROT_WRITE)
```

which gives each Task a hard ceiling at `initial_commit`. `PROT_NONE` pages do
not auto-commit on access — the kernel raises `SIGSEGV`. Everything above the
initial region would be permanently unreachable, contradicting the whole point.
Windows has a growable-reservation mechanism; POSIX does not.

**POSIX** — map the whole reserve readable and writable, and let demand-zero
paging do the growth:

```
base = mmap(reserve, PROT_READ|PROT_WRITE,
            MAP_PRIVATE|MAP_ANONYMOUS|MAP_NORESERVE)
mprotect(base, page_size, PROT_NONE)     /* guard page, RFC 0084 C3 */
stack = base + page_size                 /* usable region is the rest */
```

Untouched pages consume no physical memory, so growth is automatic and the
resident cost tracks actual use.

**On POSIX, `TaskStackCommit` has no effect and must not pretend otherwise.**
There is nothing to pre-commit — the mapping is already usable and already lazy.
This RFC's own resident-cost analysis proves it: today's `malloc(1 MiB)` is an
anonymous mapping with demand-zero pages, which is the same behaviour. The field
is meaningful only on Windows.

**Windows** — `CreateFiberEx(commit_size, reserve_size, ...)`, currently called
with `0, 0`. Here the reservation genuinely is growable: the guard page moves up
as the stack grows and the kernel commits the next page. `TaskStackCommit` is
real on this platform and sets the initial commit.

This delivers the properties asked for:

- **Starts small.** Resident cost is the pages actually touched, on both
  platforms.
- **Grows automatically** — demand-zero paging on POSIX, guard-page commit on
  Windows. No prologue check, no runtime overhead.
- **Thousands of Tasks are cheap**, bounded by address space rather than RAM.
- **Overflow traps** at the guard page instead of corrupting the heap.

What it does not give: growth beyond the reserve. A Task that needs more than its
reserve fails, where a goroutine would keep growing. That is the honest trade,
and it is the one Option A cannot be made to pay.

## Decided

### Sizes: 1 MiB reserve, 8 KiB initial commit

The reserve is nearly free on the 64-bit targets `reference.md` already
restricts Tasks to (Windows x64, POSIX x86-64). 10,000 Tasks reserve ~10 GiB of
address space and hold resident only the pages they touch.

Both values come from the project configuration struct rather than being
literals in the runtime template — see RFC 0086, which owns that struct. This
RFC is its first consumer and defines two fields:

```
TaskStackReserve  = 1 MiB   // per-Task address-space ceiling, both platforms
TaskStackCommit   = 8 KiB   // initial commit; Windows only, see Option C
```

Defaults are the values above, so a caller that supplies no configuration gets
them. RFC 0086 owns validation (commit ≤ reserve, both page-multiples, reserve
positive); this RFC assumes valid values.

`TaskStackCommit` is deliberately kept in the struct despite being inert on
POSIX: it is a real Windows knob, and one field that is a no-op on one platform
is better than two platform-specific fields or a silently different meaning per
target. The runtime comment at the POSIX allocation site must say it is
unused there, so the next reader does not "fix" its absence.

### Overflow: trap with a diagnostic

Exceeding the reserve emits a structured trap, not a bare fault:

```
[Runtime Error] task stack overflow
```

**Stack overflow is not compile-time decidable and this RFC does not pretend
otherwise.** Depth is data-dependent, direct self-recursion is a supported
language feature, and `Fun<>` values make some calls indirect. Even for a
program with no recursion at all, Hexal could bound call *depth* but not
*bytes*, because the C compiler owns frame layout. There is nothing to reject at
compile time, so the only question is what the runtime does at the fault.

The alternative — letting the guard page raise a bare `SIGSEGV` /
`EXCEPTION_STACK_OVERFLOW` — was rejected on consistency grounds.
`docs/reference.md` specifies runtime traps throughout, and every one emits a
structured `[Runtime Error]` message. A raw segfault would be the only failure
mode in the language that does not say what happened, and it is
indistinguishable from heap corruption at the point a user sees it.

#### What the handler costs

This is more machinery than one line of prose suggests, and it is the largest
piece of work in this RFC.

The faulting Task has no usable stack, so the handler cannot run on it. POSIX
needs, per worker thread:

- a `sigaltstack` region, allocated once at worker start;
- a `SIGSEGV`/`SIGBUS` handler installed process-wide;
- **thread-local state naming the guard range of the Task that worker is
  currently running.** The handler receives a fault address and must decide
  whether it belongs to a Task guard page. A global registry of every live
  Task's guard range would need a lock, and taking a lock in a signal handler is
  not async-signal-safe. A per-worker TLS pointer updated on every context
  switch avoids both problems: the handler reads one word.

Windows needs a vectored exception handler on `EXCEPTION_STACK_OVERFLOW`, which
carries the faulting address directly and needs no equivalent bookkeeping.

**A fault outside the current Task's guard range is re-raised unchanged.** The
handler must not swallow unrelated memory bugs, and this is the single most
important property to test — a handler that reports every `SIGSEGV` as a stack
overflow is worse than no handler at all.

The context-switch cost is one thread-local store per switch. If measurement
shows that is material on the scheduler's hot path, the fallback is to
compute the guard range from the current Task pointer the scheduler already
tracks, rather than caching it.

## Invariants

1. No language surface changes. No new keyword, type, or spawn parameter.
2. `spawn`, `join`, `detach`, `yield`, Channel, and Mutex semantics are
   unchanged.
3. Generated C for programs that do not spawn is byte-identical.
4. A Task's stack address is stable for its lifetime. Pointers from `ref`,
   `View`s rooted in locals, and `from_pointer` regions remain valid — this is
   what rules out Option A and must not be reintroduced.
5. The scheduler continues to require no allocator for its own storage.

## Validation

- A Task using a few KiB of stack has resident cost on the order of pages, not
  its reserve. Measure before and after; record both figures, because the
  before-figure may already be small.
- 10,000 concurrently live Tasks spawn and join successfully.
- A Task that recurses past its reserve prints `[Runtime Error] task stack
  overflow` and exits, rather than corrupting adjacent memory or dying with a
  bare fault.
- A segmentation fault whose address is **not** in any Task guard page is
  re-raised unchanged — the handler must not swallow unrelated memory bugs.
- Non-default `TaskStackReserve` reaches the generated runtime on both
  platforms: a program built with a 64 KiB reserve overflows sooner than the same
  program at 1 MiB. `TaskStackCommit` reaches the Windows `CreateFiberEx` call
  and is documented as inert on POSIX.
- **A POSIX Task uses more than `TaskStackCommit` of stack without faulting.**
  This is the regression test for the inverted construction: the earlier
  `PROT_NONE` + `mprotect` form would fault at 8 KiB, and nothing else in this
  list would catch it.
- `reference.md`'s stack sentence is updated to state reserve and initial commit
  separately, replacing "Stacks reserve 1 MiB including guard page", and to name
  the overflow trap alongside the others.
- Generated C for non-spawning programs is byte-identical; the snippet manifest
  moves only for concurrency snippets, if at all.
- `go test ./...`, `go vet ./...`.

Note that the first three cannot be verified by the Go suite, which executes no
generated C. They require running a built binary, and until that exists they must
be performed manually and the results recorded in the spec's closure.

## Non-goals

- Copying or segmented stacks — Options A and B, rejected above with reasons.
- Per-Task stack size in Hexal source. The language does not expose scheduler
  configuration and should not start here; the values are build configuration,
  owned by RFC 0086.
- The configuration struct itself, its validation, or any file format for it —
  RFC 0086.
- Compile-time stack-depth analysis. Not decidable here, for the reasons under
  Decided.
- Changing scheduling, preemption, work stealing, or worker count.
- Reducing the control-block, argument-frame, or result-frame allocations. Those
  are small and separate; fold them into a later pass if measurement shows they
  matter.
- 32-bit targets. `reference.md` already restricts Tasks to Windows x64 and
  POSIX x86-64.

## Drawbacks

- A Task's maximum depth becomes a fixed property of the runtime rather than
  something that grows with need. Programs with deep recursion inside a Task must
  restructure, where a goroutine would not.
- `mmap` plus `mprotect` is more platform surface than `malloc`, though it is the
  same construction RFC 0084 C3 already requires for the guard page.
- The improvement is largely invisible on Linux, where demand-zero pages already
  keep resident cost low. The real gains — address-space headroom, a hard
  overflow boundary, and an explicit initial-commit figure — are less immediately
  legible than "stacks got smaller", and the spec should not be sold as the
  latter.
