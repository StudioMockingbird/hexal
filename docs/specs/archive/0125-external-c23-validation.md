# RFC 0125: External C23 Validation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; blocked on RFC 0127
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: compiling, running, and trapping generated C under GCC, Clang, and
  `zig cc` in a tagged suite that closes the standing generated-C coverage gap
- Depends on: RFC 0127 (native threading primitives), the verified
  production-source boundary for runtime traps (both
  `compiler/generator/packages/*.c`/`*.h` templates and non-test Go generator
  code), the dormant harness in `compiler/tests/c23validation`, and the
  runtime templates in `compiler/generator/packages`
- Coordinates with: ADR 0055 (filesystem and build driver), RFC 0052 (target
  profiles), RFC 0124 (compiler fuzzing), and `docs/status.md` known coverage
  gaps
- Prior art: the Pixel compiler at `Forge/agents/pixel`, a prior attempt at
  this language, shipped a working version of much of this. Findings adopted
  from it are attributed inline.
- Changes no Hexal grammar, type, function signature, or result contract
- Accepted cost: a tagged suite that requires three installed toolchains, runs
  outside the default gate, and is platform-conditional for platform-specific
  runtime paths

## Summary

Generated C is currently unverified by anything. `go test ./...`,
`go vet ./...`, and `go vet -tags c23 ./...` all pass on output no compiler has
ever read. This RFC activates the existing `//go:build c23` harness, extends it
to run both GCC and Clang, and gives it three assertion tiers: the program
compiles clean, it runs and produces exact output, and it traps with the exact
runtime message.

The default gate does not change. `go test ./...` must still pass with no
external toolchain installed.

## Motivation

`docs/status.md` records this as the project's largest coverage gap, in two
severities:

*Does not compile* -- handle types reachable only through a declaration and a
`uint64_t` use in a Size-only program each produced invalid C; both were found
by an external reviewer reading generated C by hand.

*Compiles and behaves wrongly* -- a `try` in a nested block once ran its
operand twice; `try spawn` in a loop spawned one task too many and leaked it;
and POSIX fiber stacks once lacked their guard page, so an overflow corrupted
the heap silently. All were found while hand-writing one example program.

The second kind is the one that matters. A textual assertion catches an
undeclared identifier; nothing short of executing the binary catches a task
spawned twice. Every runtime claim this project has made -- the production
`[Runtime Error]` inventory derived from both production emission sources,
`print`'s exact output,
Task lifetime behavior, rune counts, iterator traps, and the Task parking
protocol -- is currently asserted textually because that was the only
mechanism available.

## What already exists

`compiler/tests/c23validation` is a complete working harness that nothing runs.
`c23_harness_test.go` already materializes every artifact in `Files` into a
temp directory, compiles every `.c` translation unit, and provides
`compileGeneratedC`, `runGeneratedC`, and `trapGeneratedC`. Seven smoke files
supply representative programs.

The suite is dormant for exactly one reason: every test function is named in
lower camel case (`c23GeneratedArrayViewCCompiles`), so Go never collects it.
This RFC does not rebuild the harness. It restores the entry points, adds the
second toolchain, and extends coverage.

## Decision

### Three toolchains for the portable tiers

The compile, run, and trap tiers run under GCC, Clang, and `zig cc`. None is a
fallback for another, and the suite does not pick whichever is installed. The
sanitizer tier is Clang-only because the selected sanitizers are a Clang runner,
not a fourth portable-C opinion.

`AGENTS.md` already sets dual-toolchain compatibility as the target: generated
C uses the latest C features "as long as the latest gcc & clang versions
support that". A construct one accepts and another rejects violates that rule
whichever way it fails. A divergence is a finding, recorded and fixed, never
resolved by preferring one compiler's judgment.

**What each one actually adds**, stated honestly rather than implying three
independent opinions:

| Toolchain | Frontend | Default target | Adds |
|---|---|---|---|
| GCC | GNU | MinGW-w64 UCRT | the only non-LLVM frontend; different diagnostics and different extension tolerance |
| Clang | LLVM | `x86_64-pc-windows-msvc` | the MSVC ABI and the MSVC/Windows SDK header set |
| `zig cc` | LLVM (bundled Clang) | `x86_64-unknown-windows-gnu` | hermetic bundled headers and libc, needing no external SDK |

Clang and `zig cc` share the LLVM frontend, so their *diagnostic* overlap is
high. Their value is not a third opinion on syntax: it is that they compile the
same source against three different libc and header sets. Most portability
defects in generated C are header and ABI defects, not parse defects.

### Host-only, for now

Every tier builds and runs for the host target only. The suite adds no
cross-target tier.

`zig cc` can compile for other targets without a cross toolchain, and that
capability is real and useful -- it is currently the only way to compile-check
the POSIX fiber runtime from a Windows machine. It is nevertheless deferred: it
widens the matrix, needs a per-host mandatory-target policy, and would make the
first version of this suite larger than the gap it closes.

The consequence is stated rather than hidden. **A platform-conditional path is
verified only where the suite is run.** On a Windows-only workflow the POSIX
fiber runtime (`ucontext`, `mmap`, `mprotect`, the `PROT_NONE` guard page)
stays uncompiled by this suite, and vice versa on POSIX. `docs/status.md`
carries that as a standing half-verified record until a run on the other
platform closes it.

A future RFC may add cross-target compile-and-link. If it does, linking is
required rather than compiling alone: the musl finding recorded below is a link
error that a compile-only check misses entirely.

### Measured toolchain facts

These were measured against real generated output, not assumed. They are
recorded because they determine what the suite can assert today.

- Non-concurrency programs compile clean under `-std=c23 -Wall -Wextra -Werror`
  and run correctly under all three toolchains on Windows.
- `<stdckdint.h>` and `<stdatomic.h>` are available everywhere tested.
- **`<threads.h>` is unavailable on every Windows target tested** -- MinGW-w64
  UCRT, `windows-gnu`, and `windows-msvc` alike. Generated concurrency programs
  therefore do not compile on Windows at all. See the open bug below.
- `zig cc -target x86_64-linux-musl` compiles the concurrency runtime but fails
  to link: musl does not implement `getcontext`, `makecontext`, or
  `swapcontext`.
- `zig cc -target x86_64-linux-gnu` compiles **and links** the full concurrency
  runtime clean under `-Werror`. This is the only configuration in which that
  runtime has ever been shown to build. It was a one-off measurement, and no
  tier in this suite reproduces it while cross-target work stays deferred.

The concurrency tier is consequently glibc-Linux-only until the Windows
threading defect is resolved. That is a limit to record, not to hide: a suite
that quietly skips the concurrency runtime reports success for the code most
likely to be wrong.

### Gating

- The `//go:build c23` tag stays. `go test ./...` is unchanged, invokes no
  external tool, and passes on a machine with no compiler.
- `go test -tags c23 ./...` **fails** when a toolchain is missing. It never
  skips. A suite that silently skips reports success for work it did not do,
  which is the failure mode this RFC exists to remove.
### Toolchain discovery

`exec.LookPath` alone is insufficient, as this RFC's own authoring proved:
clang was installed and working at `C:\Program Files\LLVM\bin\clang.exe` while
absent from the inherited `PATH`, so a `LookPath`-only suite would have
reported it missing and failed a machine that had it.

Each tool is therefore data, not a hardcoded path -- a spec carrying a name, an
override environment variable (`HEXAL_GCC`, `HEXAL_CLANG`, or `HEXAL_ZIG`),
candidate `PATH` names, and explicit platform-specific fallback paths, resolved
in that order. A spec as data means a machine can redirect one tool without
editing a test, and a missing tool is reported by name rather than as an opaque
exec failure.

The fallback lists must include the locations these toolchains actually install
to on Windows: the WinGet package directories under
`%LOCALAPPDATA%\Microsoft\WinGet\Packages` for the MinGW-w64 and zig packages,
and `C:\Program Files\LLVM\bin` for LLVM. Versioned `PATH` names
(`clang-22` alongside `clang`) belong in the candidate list.

On Linux and macOS, discovery uses the explicit override, then `PATH`, then the
conventional absolute roots `/usr/bin`, `/usr/local/bin`, and
`/opt/homebrew/bin`, plus `$HOME/.local/bin` for user-installed zig. It does not
recursively search a home directory or infer a package manager database.

The discovered path and version of every tool is reported in the test log, so a
passing run says what it passed under.

### Three tiers

**Tier 1 -- compiles clean.** `-std=c23 -Wall -Wextra -Werror` under all three
toolchains. This catches the D2/D33 class directly.

**Tier 2 -- runs and produces exact output.** The program executes, exits zero,
and its stdout matches exactly. This is the only mechanism that can verify
`print`'s output forms.

The tier runs the generated program's existing `main` directly; Hexal programs
return successful process status and no harness-owned replacement entrypoint is
needed. Capture stdout and stderr separately. Stdout is normalized before exact
comparison because a C runtime in text mode on Windows translates `\n` to
`\r\n` on the way through a pipe. A fixture asserts the bytes the program
wrote, not the platform's line-ending habits. Stderr must be empty for a
non-terminating fixture; combining the streams cannot prove either contract.

**Tier 3 -- traps.** The program exits non-zero and its stderr contains the
exact `[Runtime Error]` text. `hex_runtime_trap` writes the message to stderr
and calls `abort()`, so both halves are observable.

A terminating fixture asserts its message as a required stderr substring; a
non-terminating fixture asserts stderr is otherwise silent. Both forms are
needed: without the second, a program that traps spuriously while still
producing correct stdout passes.

Tier 3 is the largest single win available: a broad production trap inventory,
none of it executed today. Its size is derived rather than copied here.

### Every fixture must be executed by some runner

A fixture that declares an expected exit code which no runner ever executes is
worse than no fixture: it reports coverage that does not exist. Pixel shipped
exactly that -- fixtures registered for a runtime tier that only the sanitizer
executor ran, so their expectations went unchecked until a gate was added that
runs the whole tagged set by definition.

The suite owns one test-only Go catalog inside
`compiler/tests/c23validation`; it does not extend the workbench `Snippet`
schema. A fixture contains:

- one stable name;
- exactly one program source: either a workbench snippet ID, resolved through
  `snippets.Load()`, or an inline `Sources` map plus entrypoint;
- an optional process expectation containing zero/non-zero exit mode, exact
  normalized stdout, and empty-or-required-substring stderr mode;
- the host operating systems on which that expectation can execute; and
- for trap coverage, the derived message classification, reason, and owner
  when no safe fixture exists.

Every fixture enters Tier 1. A zero-exit process expectation derives Tier 2;
a non-zero expectation derives Tier 3. No separate tier-registration slices
exist. Workbench snippets are compile fixtures automatically; focused runtime
fixtures may reference one by ID rather than duplicate its source. A catalog
guard fails on duplicate names, an unknown snippet ID, both/neither source
forms, an impossible expectation, or a fixture selected by no runner.

### Cost control: dedup by generated artifact

Invoking a C compiler per candidate is three orders of magnitude slower than an
in-process check and finds less. The interesting axis is distinct *generated
C*, not distinct sources: many inputs collapse onto the same output. Every
compiler-invoking tier therefore keys on SHA-256 over a canonical stream:
artifact names sorted bytewise, with each filename length, filename bytes,
content length, and content bytes encoded in sequence. It compiles each
distinct output exactly once per toolchain, target, flag set, and test run. The
cache is in-memory; generated files and executables remain under `t.TempDir()`.

This is what makes the tier affordable enough to connect to RFC 0124's fuzzing
later, and it is why the gate stays behind the `c23` tag rather than defaulting
on -- see Gating above. Pixel defaulted its equivalent gate to on and paid for
it in day-to-day iteration speed.

### The warning-suppression list is a debt ledger

The existing harness passes `-Wno-unused-function`, `-Wno-unused-variable`,
`-Wno-unused-parameter`, `-Wno-unused-but-set-variable`, and
`-Wno-maybe-uninitialized`.

Each suppression must name the gap that requires it, in a comment at the flag:

- The four `unused-*` flags exist because the generator emits helper families
  wholesale -- equality, print, union, heap, io -- which `docs/status.md`
  already records as an open gap. They stay until demand-driven helper emission
  lands, and narrowing them is that gap's job.
- `-Wno-maybe-uninitialized` is different in kind and does not survive. The
  other four suppress warnings about code that is provably harmless; this one
  suppresses a warning about reading storage that may never have been written,
  which is precisely a "compiles and behaves wrongly" defect. Remove it,
  measure what fires, and fix each reported generated-C defect in its owning
  generator path. A finding blocks this RFC; recording it without fixing it
  cannot satisfy `-Werror`, and the warning class is never re-suppressed.

A suppression added later requires the same treatment: a named owning gap, or
it does not go in.

**One suppression is not debt, and the distinction matters.** A warning that
fires on *correct* generated C for a construct the language guarantees is
permanently suppressed, and its comment names the guarantee rather than a gap.
The prior Pixel compiler's case was `-Wno-tautological-compare`: a program may
legitimately compare a value with itself, the emitted C is valid and does
exactly what the source asked, and leaving the warning as an error "would make
`x == x` a program that compiles in Pixel and fails in C, which is the one
thing the language promises cannot happen". Self-comparison is also the
standard NaN test, so the warning is simply wrong for floating-point.

Hexal's current list has no entry of this kind. It will acquire them if
`-Wconversion` or `-Wshadow` are ever added, since both fire on correct
generated C. Each suppression is therefore labelled as one of exactly two
kinds:

- **Debt** -- names the open gap that requires it and is removed when that gap
  closes.
- **Principle** -- names the language guarantee it protects and is permanent.

An unlabelled suppression is neither and does not go in.

### Sanitizers

Add a fourth tier under a supported host Clang configuration. Every runnable
host fixture receives UndefinedBehaviorSanitizer with
`-fsanitize=undefined -fno-sanitize-recover=all -O1 -g`. An artifact set that
does not contain `hexal/concurrency.c` additionally receives AddressSanitizer
through `-fsanitize=address,undefined`. Any sanitizer diagnostic or non-zero
process result fails the fixture. Cross-built binaries do not run this tier,
and a host without the required sanitizer runtime is reported according to the
tagged-suite missing-tool policy rather than passed.

Scheduler artifact sets are deliberately excluded from ASan, not silently
treated as covered. LLVM requires a custom fiber runtime to call
`__sanitizer_start_switch_fiber` before every stack switch and
`__sanitizer_finish_switch_fiber` after it. Hexal emits no such annotations.
Adding conditional sanitizer machinery to the runtime is a separate design;
this validation RFC does not change generated output merely to widen one test
tier. Scheduler fixtures still receive UBSan and their ordinary runtime tests.

This tier catches ordinary generated heap misuse, signed overflow, misaligned
access, and invalid enum values that `-Werror` cannot see. A custom
`mmap`/`mprotect` Task guard-page fault may appear only as a fatal signal and is
validated by its dedicated runtime fixture rather than claimed as an ASan heap
diagnostic.

**ThreadSanitizer is deliberately excluded, with a reason.** TSan is the tool
that would have found the four Task parking defects the park/commit/wake
protocol rework fixed -- one fiber running on two OS threads is exactly its
specialty. But the Hexal scheduler
switches fibers with `swapcontext`, and TSan does not model user-space context
switching: without `__tsan_switch_to_fiber` annotations threaded through every
switch site, it reports the scheduler's own stack reuse as a race and produces
unusable output. Adding those annotations is real work in
`packages/concurrency.c` and is its own decision. Record it as deferred rather
than adding TSan and drowning the signal.

### Platform conditionality

Some runtime paths exist only on one platform, and a claim can only be
validated where its code runs:

- POSIX fiber stacks (`mmap`, `mprotect`, `ucontext`) and the guard-page fault
  are POSIX-only.
- Windows fibers, `GetStdHandle`, and the structured-exception stack-overflow
  handler are Windows-only.

The suite runs what the host supports. A fixture whose platform code cannot
run here is not collected here. A claim that is only ever validated on one platform stays recorded in
`docs/status.md` as half-verified until the other runs it.

## Coverage backlog

The required target list is the set of claims `docs/status.md` currently
records as textually asserted only. Implementation works through it; each item
moves out of the coverage-gap entry as its test lands.

- Every `[Runtime Error]` message is derived directly from `.c`/`.h` templates
  under `compiler/generator/packages/` and non-test Go files directly under
  `compiler/generator/`, then classified as one of: deterministically triggerable,
  resource-failure requiring an explicit injection seam, platform-only, or an
  internal invariant unreachable from valid source. Triggerable messages fire
  with exact text; the other classes retain an explicit reason and owner rather
  than gaining an unsafe or nondeterministic fixture.
- `print`'s exact output forms for every printable type.
- A `try` in a nested block evaluates its operand once; `try spawn` in a loop
  spawns exactly one task per iteration and leaks none.
- A fiber overflow reaches the guard page and traps; resident cost per Task is
  measured; 10,000 Tasks can be live concurrently; and a 64 KiB reserve
  overflows sooner than 1 MiB.
- A String slice over multi-byte input is byte-identical to the scanning
  definition; a concatenated rune count matches an independent scan.
- `push`, `free`, and Dict `insert` during traversal trap with the
  collection-modified message rather than looping or reading freed memory.
- Immediate yield, join completion, Channel wake, and Mutex wake never run one
  fiber on two workers and never lose a wake; a contended Mutex transfers
  ownership without trapping its selected waiter.
- Allocation failure traps; the removed double-free and wrong-allocator
  messages do not appear.

## Test lifecycle

Every tier is host-only. There is no cross-target tier and no mandatory
non-host target; see Host-only, for now.

Two situations look alike and are not, so they get different mechanisms.

**A missing toolchain is an environment defect and fails.** An explicitly
requested tagged run is strict. A missing or unusable toolchain fails by name,
and the message states which tool, which discovery locations were tried, and
what the run needed it for, so it is actionable without reading this RFC.
There is no skip, no opt-out, and no degraded mode. Failed compilation and a
toolchain below the supported floor fail the same way. The ordinary untagged
suite remains toolchain-independent and is unaffected.

**A platform-conditional fixture is a property of the fixture, not a defect.**
POSIX guard-page faults cannot run on Windows and Windows fibers cannot run on
POSIX. Such a fixture declares the platforms it applies to and is not collected
on the others.

Together those give one rule with no third state: **a tagged run either passes
everything it collected, or fails by name.** There is no `unrun` result, no
ledger of unrun items, and no `t.Skip` to interpret -- a fixture that does not
apply to the host is not part of the run, and every other absence is a failure.

What a host cannot verify stays recorded in `docs/status.md` as half-verified
until a run on the other platform closes it. Aggregating results across runs
and machines is a CI concern and is out of scope.

The suite remains C23 permanently. GCC older than 15 and Clang older than 18
fail discovery as unsupported even if they accept an earlier `c2x` spelling;
each accepted toolchain must additionally pass the selected RFC 0052 profile's
header, builtin, target, runtime, and linker probes.

## Required sweep

- Remove `CombinedOutput` from run and trap assertions; capture stdout and
  stderr separately.
- Remove the harness-owned replacement entrypoint; execute generated `main`
  directly.
- Remove `-Wno-maybe-uninitialized` and fix every finding instead of replacing
  it with another class-wide suppression.
- Do not preserve fixed runtime-trap counts; derive the production inventory
  directly from both emission sources when building executable fixtures.
- Rewrite terminal-spec provenance in the coverage backlog as present-tense
  behavior before terminal specifications are deleted.

## Implementation plan

### Phase 0: prerequisites and measured baseline

1. Land RFC 0127.
2. Record the green ordinary and tagged-suite baseline and the current snippet
   manifest.
3. Re-run the toolchain/version/target probes and correct stale measured facts
   before using them as gates.
4. Remove `-Wno-maybe-uninitialized`, inventory every warning it exposes, and
   fix those generator defects before proceeding.

### Phase 1: harness and toolchain model

1. Replace the GCC-only helper with data records for GCC, Clang, and `zig cc`,
   including the three named overrides, PATH candidates, platform fallbacks,
   version command, default target, and standard flag.
2. Parse and enforce GCC 15 and Clang 18 as the minimum frontend versions;
   reject an older discovered compiler by name and version.
3. Materialize every artifact and compile every `.c` translation unit under
   `t.TempDir()` in sorted order, with the platform's required pthread/link
   options.
4. Add the canonical artifact-set hash and per-run cache keyed by artifact,
   toolchain, target, and flags.
5. Add a focused executable check that every suppression is classified as Debt
   or Principle and names its current owner.

### Phase 2: portable runners

1. Restore runnable test entrypoints and derive fixture membership from the
   test-only catalog above rather than parallel registration lists.
2. Implement compile, exact-output, and trap runners for all three toolchains.
3. Execute generated `main` directly, normalize stdout line endings, capture
   stderr separately, and assert the complete exit/output contract.
4. Declare each fixture's platform applicability and collect only the fixtures
   that apply to the host. Add no cross-target build and no third result state.

### Phase 3: runtime and sanitizer backlog

1. Classify the derived trap inventory and add every safe deterministic trap
   fixture.
2. Add exact print-output and the remaining runtime-behavior fixtures listed
   above, preserving platform ownership.
3. Add the Clang UBSan runner for every runnable host fixture and add ASan only
   when the generated artifact set omits `hexal/concurrency.c`; fail on every
   sanitizer report.
4. Re-derive `docs/status.md`'s remaining coverage gaps from what the runners
   actually execute.

### Phase 4: conformance

1. Implement every Validation item below and no additional behavior.
2. Run `gofmt`, `go test ./...`, the resolved tagged suite, `go vet ./...`, and
   the matching tagged vet command.
3. Verify no test writes outside `t.TempDir()` and no ordinary test invokes an
   external process.

## Non-goals

- Making the external suite part of the default gate.
- A build driver, project files, linking against foreign objects, or artifact
  materialization outside a temp directory. ADR 0055 owns that layer; this
  suite writes to `t.TempDir()` and nothing else.
- Cross-target compilation of any kind. Every tier is host-only; a future RFC
  may add compile-and-link for non-host targets.
- General user-facing cross-compilation, target profiles, or arbitrary
  non-host ABIs. RFC 0052 owns those.
- `clang-cl`, MSVC's own `cl.exe`, or any fourth toolchain.
- Fixing the Windows `<threads.h>` defect. RFC 0127 must land first and owns the
  fix; this RFC makes the repaired runtime continuously reproducible.
- ThreadSanitizer, for the reason recorded above.
- ASan fiber-switch annotations or a claim that scheduler fixtures are
  address-sanitized.
- Performance benchmarking of generated code.
- Fixing the generated dead-code emission that the `unused-*` suppressions
  tolerate.

## Validation

This section is exhaustive. RFC 0125 is complete only when every item passes:

- `go test ./...` passes on a machine with neither GCC nor Clang installed and
  invokes no external process.
- `go test -tags c23 ./...` fails with a clear message when any of the three
  toolchains is missing, and never skips.
- The compile, exact-output, and trap tiers run under all three toolchains; a
  program accepted by one and rejected by another fails the suite and is
  recorded as a divergence. The sanitizer tier is Clang-only.
- The discovered toolchain versions and their default targets appear in the run
  log.
- No tier builds for a non-host target, and the suite emits no third result
  state: a collected fixture passes or fails.
- After RFC 0127, the concurrency compile tier runs under every required
  toolchain on the host.
- Every tool resolves through a spec carrying an override variable, candidate
  `PATH` names, and fallback paths; a tool present at a standard install
  location but absent from `PATH` is found, not reported missing.
- A missing tool is reported by name.
- Tier 2 executes generated `main`, captures stdout and stderr separately, and
  normalizes stdout line endings before comparison; a multi-line expectation
  passes on Windows and unexpected stderr fails.
- Tier 3 asserts terminating fixtures by required stderr substring and
  non-terminating fixtures by stderr silence.
- The test-only catalog has the exact schema and derivation rules above. It
  rejects duplicate names, invalid source selection, unknown snippet IDs,
  impossible expectations, and fixtures selected by no runner. The workbench
  `Snippet` schema is unchanged.
- Every compiler-invoking tier compiles each distinct generated artifact set
  once per toolchain, target, and flag set, keyed by the canonical hash defined
  above.
- Every warning suppression is labelled Debt or Principle and names either its
  owning gap or the language guarantee it protects; an executable guard fails
  when a compiler flag suppressing a warning lacks the adjacent classification.
- Tier 1 uses `-std=c23 -Wall -Wextra -Werror` and every suppression names its
  owning gap in a comment.
- `-Wno-maybe-uninitialized` is removed and every instance it was hiding is
  fixed; recording a warning without fixing it does not satisfy this RFC.
- Tier 2 asserts exact stdout, not a substring or an exit code alone.
- Tier 3 asserts both a non-zero exit and the exact `[Runtime Error]` text.
- The sanitizer tier runs under a supported host Clang configuration and fails
  on any report or non-zero exit. Every runnable host fixture receives UBSan;
  only artifact sets omitting `hexal/concurrency.c` receive ASan. A scheduler
  fixture is excluded from ASan by its declared fiber-annotation reason, never
  reported as address-sanitized.
- No test writes outside `t.TempDir()`.
- Platform-specific claims run where their code exists. A fixture declares the
  platforms it applies to and is not collected elsewhere, so no `t.Skip` and no
  third result state is emitted. A missing toolchain fails by name instead.
- Every backlog item above has a test or an explicit recorded reason it cannot
  have one yet.
- `docs/status.md`'s generated-C coverage-gap entry shrinks to exactly what
  remains unverified, re-derived rather than edited by hand.
- `gofmt`, `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

None directly. This RFC adds no language rule; it verifies rules
`docs/reference.md` already states. If execution shows generated behavior
disagreeing with a stated rule, that conflict is resolved under the owning
spec before this RFC closes -- a disagreement is a defect in the compiler or an
error in the reference, never a reason to weaken a test.
