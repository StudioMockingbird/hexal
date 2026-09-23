# RFC 0158: Leak Detection on the Linux Lane

- Kind: Tooling Proposal
- Status: Implementation ready, for the Linux lane only. Rescoped 2026-09-23
  from a Hexal-owned tracking allocator to a LeakSanitizer gate, after a sweep
  showed the vendored mimalloc cannot enumerate the runtime's allocations and
  that the only fix costs 7-12% on every allocation. Consuming receivers were
  withdrawn earlier and are not revisited
- Created: 2026-09-10
- Updated: 2026-09-23
- Depends on: implemented RFC 0217 (installed-Clang Linux backend), which
  supplies the ASan/LSan-capable execution gate that every earlier revision of
  this RFC lacked
- Coordinates with: RFC 0183 (runtime validation; owns the UBSan lane), RFC
  0185 (ASan fiber coverage), archived RFC 0187 (driver build modes), and RFC
  0165, which defers leak reporting here
- Updates `docs/reference.md`: yes, a Build modes section. The leak instrument
  itself is tooling, but the mode it lands in has a normative contract that the
  reference has never stated, and archived RFC 0187 deferred exactly that edit
  pending explicit approval. See Reference synchronization

## Summary

Detect memory leaks by running Hexal programs under LeakSanitizer on the Linux
lane, not by building a tracking allocator.

It adds one flag to the debug mode, one pair of `#if`-guarded root-region calls
in the concurrency runtime, one weak-symbol default, and no Hexal-owned
bookkeeping. It adds no keyword, type, method, ownership rule, build mode,
driver option, or generated-program semantic change, and the generated C text
is byte-identical in every mode.

## Why the tracking allocator was abandoned

The previous design reused mimalloc's own facilities and fell back to a
Hexal-owned table. A sweep on 2026-09-23 against the vendored
`mimalloc_v3.5.1` archive, linked with GCC 16.1.0, showed the reuse path does
not exist and the fallback is not worth its cost.

**`mi_heap_visit_blocks` cannot see allocations made by `mi_malloc`.** With two
live blocks of 64 and 128 bytes, visiting the default heap returns `true`,
invokes the visitor once with a `NULL` block, and reports one area with
`used=0, block_size=0`. The identical program allocating 256 bytes through an
explicitly created heap (`mi_heap_new` plus `mi_heap_malloc`) reports
`used=1, block_size=256` and enumerates the block. `hex_heap_allocate` calls
`mi_malloc`, so the enumeration facility returns nothing for every allocation
the runtime actually makes.

| Facility | Vendored 3.5.1 reality |
| --- | --- |
| `mi_heap_visit_blocks` | Exported and callable; enumerates only explicit-heap allocations |
| `mi_heap_get_default` | Renamed `mi_theap_get_default` in 3.x; the 2.x spelling does not compile |
| `mi_stats_print` | Exported; page, arena, and process aggregates only. One leaked 128-byte block is invisible |
| `mi_stats_merge` | Declared in the header, **absent from the archive**; calling it fails at link |
| `mi_check_owned` | Exported; not exercised |

Routing the runtime through an explicit heap is the only way to make the
facility work, and it costs, over 3,000,000 48-byte allocate/free pairs in
three runs: `mi_malloc` 3.38/3.38/3.39 ns against `mi_heap_malloc`
3.63/3.64/3.80 ns — **+0.24 to +0.41 ns, +7% to +12%, consistent.** That is a
permanent tax on every allocation in every shipped program, paid so an opt-in
development facility can work, against language goal 15.

LSan costs nothing in ordinary builds, reports a stack trace per leak rather
than a bare size, and needs no allocator change at all.

## Why LSan is tractable where ASan is not

RFC 0185 is deferred because ASan over the Task scheduler requires balanced
`__sanitizer_start_switch_fiber` and `__sanitizer_finish_switch_fiber`
annotations across root, worker, parked, resumed, completed, and abandoned
transitions. That inventory is real work and is not this RFC's.

LSan does not need it. It does not follow control flow; it stops the world at
exit, scans roots for pointers, and reports unreachable allocations. Its only
requirement is knowing where the roots are.

POSIX Task stacks are `mmap(MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE)`
regions in `compiler/generator/packages/concurrency.c`, not heap blocks, so
LSan does not scan them by default. A pointer whose only reference lives on a
suspended fiber stack would be reported as leaked. The fix is two calls, not
six transitions:

```c
__lsan_register_root_region(region, stack_size);    // after mmap succeeds
__lsan_unregister_root_region(region, stack_size);  // before munmap
```

That is the entire fiber accommodation. Six transition points collapse to one
create site and one destroy site because a leak scan cares where a stack *is*,
never when it is switched to.

## Generated C stays byte-identical

The calls are emitted unconditionally and compile to nothing unless the binary
is built with LSan:

```c
#if defined(__has_feature)
#  if __has_feature(leak_sanitizer)
#    include <sanitizer/lsan_interface.h>
#    define HEX_LSAN_ROOT_REGISTER(p, n)   __lsan_register_root_region((p), (n))
#    define HEX_LSAN_ROOT_UNREGISTER(p, n) __lsan_unregister_root_region((p), (n))
#  endif
#endif
#ifndef HEX_LSAN_ROOT_REGISTER
#  define HEX_LSAN_ROOT_REGISTER(p, n)     ((void)0)
#  define HEX_LSAN_ROOT_UNREGISTER(p, n)   ((void)0)
#endif
```

One text, selected by the preprocessor. So the snippet manifest moves exactly
once, when the macros and their two call sites are added, and never again for
building in one mode or the other. No generated call site changes, no branch
enters any hot path, and no release build links a sanitizer runtime.

## Selection boundary

**Leak checking belongs to the debug mode. It is not a separate driver option,
a new mode, or a source-language or `Project` setting.**

- `-fsanitize=leak` joins debug's existing
  `-fsanitize=undefined -fno-sanitize-recover=all` in `modeOptionTable` in
  `internal/driver/mode.go`. Release is untouched and gains nothing.
- No new surface. A developer already reaches leak checking by building the way
  they already build while developing, which is the one obvious way.
- Standalone LSan is chosen over `-fsanitize=address`, which would pull in
  ASan's shadow memory and the unresolved fiber-switch problem RFC 0185 owns,
  for a facility that only needs the leak check.
- Foreign C is exempt with no work: `ForeignCompileOptions` already drops every
  option containing `sanitize`, so third-party translation units keep compiling
  unmodified. That filter was written for the UBSan backstop and covers this
  flag for the same reason — the instrumentation is a check on Hexal-generated
  C and is never imposed on source Hexal did not write.

### The Windows lane needs an explicit answer

`Options(mode)` takes a mode and no target, so a flag added to debug applies to
every target. LSan does not exist on the MinGW/UCRT lane, so a debug build
there would fail to link.

Today that is latent rather than live: `checkHost` restricts native builds to a
`linux/amd64` host, so Linux is the only lane a debug build currently reaches.
It must not stay latent. `Options` takes a target profile alongside the mode,
and the leak flag is added only for a Linux profile. Relying on the host gate
instead would leave a defect armed for whoever re-enables the Windows lane.

### What leak checking does not change

Build-mode semantics are otherwise unchanged. RFC 0187's rule that debug and
release produce byte-identical generated C still holds — this is a flag, not a
lowering difference. Leak checking observes; it does not alter allocation,
cleanup, trap, stdout, or successful-program behaviour.

## Reporting versus failing

**A leak reports; it does not fail the run.** Debug mode hard-fails UBSan with
`-fno-sanitize-recover=all`, and leak checking deliberately does not match that
policy, because the two findings differ in kind: undefined behaviour is always
a bug, and an allocation held to process exit is not. Hexal has explicit manual
cleanup, no destructors, and — as RFC 0165 records while declining static leak
diagnosis — no convention for marking an allocation as intentionally
process-lifetime. A program that allocates a cache and never releases it is
correct Hexal, and must not fail its debug run.

The existing fixture suite is the evidence rather than the speculation: its
allowlist exists precisely because an unrestricted leak check was noisy against
this project's own code.

The mechanism is the weak symbol, not an environment variable. The driver
builds programs; it does not run them, so it cannot set `LSAN_OPTIONS` for a
user's execution. LSan calls `__lsan_default_options` at startup if the binary
defines it:

```c
#if defined(__has_feature)
#  if __has_feature(leak_sanitizer)
const char *__lsan_default_options(void) { return "exitcode=0"; }
#  endif
#endif
```

This has a property worth keeping: `LSAN_OPTIONS` in the environment overrides
`__lsan_default_options`, so report-only is the default and a developer or CI
job that wants hard failure opts in with `LSAN_OPTIONS=exitcode=23` without any
rebuild or any new Hexal surface.

## Contract

- A clean run prints nothing and exits with the program's own status.
- A leaking run prints LSan's report — one entry per leak with its allocation
  stack trace — on stderr, and exits with the program's own status. The leak is
  information, not a failure.
- `LSAN_OPTIONS` in the environment overrides that default, so an opt-in hard
  failure needs no rebuild.
- Process-lifetime runtime allocations that Hexal's own runtime never releases
  are suppressed at their allocation site with `__lsan_ignore_object`, guarded
  by the same macro pair, so the runtime never reports itself. A *user*
  program's intentional retention is not suppressed: it is reported, and
  reporting is harmless because it does not fail the run.
- A Task still suspended at exit owns a live fiber stack that is a registered
  root, so pointers reachable only from it are not reported. A Task whose stack
  has been unmapped has been unregistered, so a genuine leak from a completed
  Task is still reported.
- Hexal trap output and LSan diagnostics are distinguishable: traps are the
  program's own stderr, LSan's report is prefixed by its runtime.

## Non-goals

- Language-level ownership, affinity, moves, consuming receivers, destructors,
  or exactly-once cleanup.
- Static leak diagnosis. RFC 0165 declines it deliberately and this RFC does
  not reintroduce it; a compile-time leak error needs a process-lifetime
  allocation convention Hexal does not have.
- Proving that a File, socket, process, or foreign handle was closed. A leak
  report may reveal memory retained by an unclosed resource, but it is not a
  resource-lifecycle proof.
- Treating each Stash value or Pool slot as a distinct allocation. A Stash
  block and a Pool backing region are single allocations; values carved from
  them are not independently visible, and this RFC does not make them so.
- ASan, memory-error detection, and the fiber switch annotations RFC 0185 owns.
- Windows leak detection. The Windows lane has no LSan; it keeps UBSan under
  RFC 0183 and gains nothing here.
- A tracking allocator, in mimalloc or in Hexal. See Why the tracking allocator
  was abandoned.

## What already exists

This RFC is not introducing LeakSanitizer to the project. `TestC23SuiteLeak`
in `compiler/tests/c23validation/leak_test.go` already rebuilds selected
fixtures with `leakFlags = []string{"-fsanitize=leak"}` and requires exact
stdout, no report, and a zero exit.

It is deliberately narrow, and its comment states the reason this RFC must
answer at a larger scale:

> Every other fixture deliberately leaves bindings unfreed, so a program-wide
> leak check would report their intentional leaks; only the transforms promise
> to release the malloc buffer utf8proc_map returns, so only they are
> leak-checked.

So the mechanism is proven on this toolchain and the allowlist exists because
an unrestricted leak check was already known to be noisy. What this RFC adds is
leak checking for **user programs built in debug mode**, which the fixture
suite does not cover. The intentional-retention problem that forced the fixture
allowlist reappears for user programs, and Reporting versus failing is where it
is answered.

## Relationship to RFC 0185

RFC 0185's "work required" list includes *"define leak-check ownership for
process-lifetime runtime allocations and intentionally abandoned Tasks."* This
RFC takes that item, because it is the leak question and this is the leak RFC.

Everything else in RFC 0185 stays there: ASan, the six fiber transitions, the
shadow-memory cost, and the faulty-access canary. Its reactivation condition —
a POSIX target that can compile, link, and execute Clang sanitizers — is now
met by implemented RFC 0217, so that spec is unblocked on toolchain grounds
even though this one does not close it.

## Implementation plan

### Phase 1 — the macro pair and the root registration

1. Add the `HEX_LSAN_ROOT_REGISTER` / `HEX_LSAN_ROOT_UNREGISTER` macro pair to
   the concurrency runtime component, in the form above.
2. Call them at the two fiber-stack sites in
   `compiler/generator/packages/concurrency.c`: after the `mmap` succeeds and
   the guard page is protected, and before each `munmap`, including the error
   paths that unwind a partially constructed context.
3. Rebuild the snippet manifest, confirm the only artifact that moved is the
   concurrency component, and say so in the commit message.

### Phase 2 — the debug-mode flag

1. Give `Options` a target profile alongside the mode. This is the prerequisite,
   not an afterthought: the table is keyed by mode alone today, and adding a
   Linux-only flag to a mode-keyed table arms a link failure for whoever
   re-enables the Windows lane.
2. Add `-fsanitize=leak` to `ModeDebug`'s compile and link sets for a Linux
   profile only, beside the existing `-fsanitize=undefined`.
3. Add the `__lsan_default_options` weak symbol returning `exitcode=0`, under
   the same `#if` guard as the macro pair.
4. Confirm `ForeignCompileOptions` still strips the flag from foreign
   translation units — it filters on `sanitize` and needs no change, but the
   behaviour is asserted rather than assumed.
5. Confirm release mode is untouched and no generated C moves.

### Phase 3 — suppressing the runtime's own retention

1. Enumerate every allocation Hexal's runtime deliberately holds to process
   exit.
2. Mark each with `__lsan_ignore_object` under the same macro guard, so the
   runtime never reports itself. User-program retention is deliberately not
   suppressed; it reports, harmlessly.
3. Confirm a program that spawns, joins, and exits cleanly reports nothing.

### Phase 4 — the canary and the gate

1. Add a deliberately leaking fixture to the Linux validation lane and confirm
   it is reported once, with a stack trace, and exits with the program's own
   status.
2. Confirm the same fixture exits 23 under `LSAN_OPTIONS=exitcode=23`, proving
   the override works without a rebuild.
3. Add a clean counterpart and confirm silence and a zero exit.
4. Add a suspended-Task fixture and confirm no false positive from a live fiber
   stack.
5. Document debug mode's leak reporting in `docs/benchmarks.md`'s
   diagnostic-runs section beside the race, coverage, and shuffle entries.

### Phase 5 — the reference's Build modes section

Last, because it asserts behaviour Phase 4 establishes.

1. Add the **Build modes** section to `docs/reference.md` as set out under
   Reference synchronization: the two modes and their selection, the
   mode-independence contract, the stack-overflow resource-limit exception,
   what each mode adds, and debug's leak reporting.
2. Place it with the other driver-facing contracts rather than inside the
   language-semantics run, since it governs compilation of the generated C and
   not the meaning of Hexal source.
3. Verify each claim against the tree rather than against this spec: the
   byte-identical-C claim against the snippet manifest across both modes, and
   the leak-reporting claim against the Phase 4 fixtures.
4. Leave `docs/benchmarks.md`'s "Build mode comparison" numbers alone. The
   reference states the contract; the measurement file keeps the measurements.

## Reference synchronization

`docs/reference.md` gains a **Build modes** section. It does not exist today:
archived RFC 0187 implemented the modes and deliberately left the reference
untouched, recording that *"approved implementation adds only the
mode-independence contract and the resource-limit exception after behavior
stabilizes, with explicit user approval."* That approval was given on
2026-09-23, so this RFC carries the edit 0187 deferred, plus the one line its
own change adds.

`docs/language.md` is not the target and must not be recreated: it was retired
into `docs/reference.md`, which is the sole normative language document.
`docs/benchmarks.md`'s existing "Build mode comparison" section stays where it
is — it holds *measurements* of the two modes, which is a different thing from
their contract, and the reference must not restate its numbers.

The section states rules, not a tutorial, per the reference's own style:

- The two modes, `debug` and `release`, and that `-mode` selects one with
  `release` as the default when omitted.
- **The mode-independence contract**, which is the normative core and the
  reason this belongs in the reference at all: generated C is byte-identical in
  both modes, and no mode changes program semantics, output, trap behaviour, or
  evaluation order. A mode selects how the C is compiled, never what it is.
- The one documented exception: a mode may change the point at which a
  stack-overflow resource limit is reached, because optimization changes frame
  sizes. That is a resource limit, not a semantic difference.
- What each mode adds, as a contract rather than a flag list. Debug: the
  undefined-behaviour backstop, non-recoverable, so UB stops the program at the
  point of the fault; debug information retained. Release: optimized, stripped,
  unused sections discarded.
- **Leak reporting in debug**, this RFC's own addition: a debug build on the
  Linux lane reports allocations still live at exit, with their allocation
  stack traces, on stderr. It does not change the exit status, because holding
  an allocation to process exit is not an error in a language with explicit
  manual cleanup and no process-lifetime convention. `LSAN_OPTIONS` overrides
  that. The report is absent on lanes without LeakSanitizer, and absent in
  release.
- That neither mode is a correctness contract: a program that is correct in one
  is correct in the other, and the diagnostics debug adds are instruments for
  finding bugs rather than a different language.

Written after behaviour stabilizes, per the standing rule, and not before Phase
4 has produced the leak-reporting evidence the section will assert.

## Validation

This section is exhaustive.

- A program that allocates and never frees is reported exactly once, with an
  allocation stack trace naming the allocating frame, and exits with the
  program's own status. A leak reports; it does not fail the run.
- The same program exits 23 under `LSAN_OPTIONS=exitcode=23`, with no rebuild,
  proving the environment override reaches `__lsan_default_options`.
- A program that allocates and frees reports nothing and exits with its own
  status.
- A program that spawns a Task, joins it, and exits cleanly reports nothing:
  the fiber stack was unmapped and unregistered, and nothing on it is mistaken
  for a leak.
- A program holding a pointer reachable only from a suspended Task's fiber
  stack at exit produces **no** leak report, because the stack is a registered
  root region.
- Every allocation Hexal's runtime deliberately holds to exit is suppressed, so
  a clean program's report is empty rather than merely short, and the runtime
  never reports itself.
- The generated C text is byte-identical in debug and release: the macro pair,
  its call sites, and the weak-symbol default are present unconditionally, and
  the preprocessor selects. The snippet manifest moves once, in Phase 1, and
  only for the concurrency component.
- A release build links no sanitizer runtime, and `go test ./...` invokes no
  external process, tracker, or sanitizer.
- `Options` is keyed by mode and target, and a debug build for a non-Linux
  profile carries no leak flag rather than failing to link.
- `ForeignCompileOptions` strips `-fsanitize=leak` from foreign translation
  units, so third-party C compiles unmodified.
- The debug and release modes behave exactly as archived RFC 0187 specifies,
  including that both still produce byte-identical generated C.
- UBSan coverage under RFC 0183 is unchanged, and a program can be built with
  both sanitizer sets without either being dropped.
- Hexal trap output remains distinguishable from LSan's report in a run that
  both traps and leaks.
- `docs/reference.md` holds a Build modes section stating the two modes and
  their selection, the mode-independence contract, the stack-overflow
  resource-limit exception, what each mode adds, and debug's leak reporting
  with its non-failing exit behaviour.
- Every claim in that section is verified against the tree: the
  byte-identical-C claim against the snippet manifest built in both modes, and
  the leak-reporting claim against the Phase 4 fixtures. No claim is copied
  from this spec without being checked.
- `docs/language.md` is not created, and `docs/benchmarks.md`'s "Build mode
  comparison" measurements are unchanged and not duplicated into the reference.

## Implementation readiness

Implementation-ready, with one honest limitation stated rather than hidden.

The mechanism is settled and its costs are measured: the mimalloc facilities do
not work, the explicit-heap fix costs 7-12% per allocation, LSan costs nothing
in a release build, and the fiber accommodation is two calls rather than RFC
0185's six transitions. The generated-C question is settled by the `#if` form,
which keeps one text. Selection is settled: leak checking is part of the debug
mode, so there is no new option, mode, or setting anywhere. Failure policy is
settled: a leak reports and does not fail, with `LSAN_OPTIONS` as the opt-in
override.

The toolchain question that blocked every earlier revision is also closed by
something that already shipped. `-fsanitize=leak` is in use today in
`compiler/tests/c23validation/leak_test.go`, so the flag, the Clang lane, and
the clean-run contract are all proven on this project rather than assumed.

**The limitation: none of the Validation items above has been executed.** The
driver host-gates native builds to `linux/amd64`, and the sweep that produced
this rescope ran on Windows against the vendored archive with GCC. Every claim
about mimalloc is measured; every claim about LSan's behaviour on this runtime
is reasoned from LSan's documented contract and the runtime's own `mmap` call,
not observed. Phase 4 is where it becomes observed, and the suspended-Task case
is the one most likely to surprise — it is listed in Validation precisely so it
cannot be skipped.

That limitation is also why Phase 5 is last. The reference is the normative
document, so its Build modes section must assert what Phase 4 measured, not
what this spec predicted. If a Phase 4 fixture contradicts a claim above, the
reference follows the tree and this spec is the thing that was wrong.
