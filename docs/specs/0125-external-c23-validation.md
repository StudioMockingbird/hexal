# RFC 0125: External C23 Validation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Created: 2026-08-26
- Updated: 2026-08-26
- Scope: compiling, running, and trapping generated C under GCC, Clang, and
  `zig cc` in a tagged suite that closes the standing generated-C coverage gap
- Depends on: the dormant harness in `compiler/tests/c23validation` and the
  runtime templates in `compiler/generator/packages`
- Coordinates with: ADR 0055 (filesystem and build driver), RFC 0052 (target
  profiles), RFC 0124 (compiler fuzzing), and `docs/status.md` known coverage
  gaps
- Changes no Hexal grammar, type, function signature, or result contract
- Accepted cost: a tagged suite that requires two installed toolchains, runs
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

*Does not compile* -- RFC 0073's D2 (handle types reachable only through a
declaration) and D33 (`uint64_t` in a Size-only program), each found by an
external reviewer reading generated C by hand.

*Compiles and behaves wrongly* -- RFC 0084's C1 (a `try` in a nested block ran
its operand twice; `try spawn` in a loop spawned one task too many and leaked
it) and C3 (POSIX fiber stacks lacked their guard page, so an overflow
corrupted the heap silently). Both were found while hand-writing one example
program.

The second kind is the one that matters. A textual assertion catches an
undeclared identifier; nothing short of executing the binary catches a task
spawned twice. Every runtime claim this project has made -- 45 distinct
`[Runtime Error]` messages, `print`'s exact output forms, RFC 0085's task
lifetime behavior, RFC 0087's rune counts, RFC 0115's iterator traps, RFC
0122's parking protocol -- is currently asserted textually because that was the
only mechanism available.

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

### Three toolchains, always

Every tier runs under GCC, Clang, and `zig cc`. None is a fallback for another,
and the suite does not pick whichever is installed.

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
| `zig cc` | LLVM (bundled Clang) | `x86_64-unknown-windows-gnu` | hermetic bundled headers, no external SDK, and cross-target compilation |

Clang and `zig cc` share the LLVM frontend, so their *diagnostic* overlap is
high. Their value is not a third opinion on syntax: it is that they compile the
same source against three different libc and header sets. Most portability
defects in generated C are header and ABI defects, not parse defects.

### Cross-target compilation

`zig cc` compiles for a target other than the host without a cross toolchain,
because it ships its own headers and libc for each target. That makes it the
only member of the matrix that can compile-check a platform-specific runtime
path from a machine that is not that platform.

This matters immediately: the POSIX fiber runtime (`ucontext`, `mmap`,
`mprotect`, the `PROT_NONE` guard page) has never been compiled by anything,
and on a Windows development machine nothing else here can compile it.

The suite therefore adds a compile-and-link tier that builds every program for
at least one non-host target. Linking is required, not just compiling: the
POSIX finding below is a link error that a compile-only check misses entirely.

Cross-target builds are compiled and linked, never executed. Running a
cross-built binary needs an emulator or a remote runner, which is out of scope.

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
  runtime has ever been shown to build, and it is what the cross-target tier
  exists to keep working.

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
- Toolchain paths resolve through `exec.LookPath` once per run, with the
  discovered versions reported in the test log so a passing run says what it
  passed under.

### Three tiers

**Tier 1 -- compiles clean.** `-std=c23 -Wall -Wextra -Werror` under both
toolchains. This catches the D2/D33 class directly.

**Tier 2 -- runs and produces exact output.** The program executes, exits zero,
and its stdout matches exactly. This is the only mechanism that can verify
`print`'s output forms.

**Tier 3 -- traps.** The program exits non-zero and its stderr contains the
exact `[Runtime Error]` text. `hex_runtime_trap` writes the message to stderr
and calls `abort()`, so both halves are observable.

Tier 3 is the largest single win available: 45 distinct messages, none of them
verified today.

### The warning-suppression list is a debt ledger

The existing harness passes `-Wno-unused-function`, `-Wno-unused-variable`,
`-Wno-unused-parameter`, `-Wno-unused-but-set-variable`, and
`-Wno-maybe-uninitialized`.

Each suppression must name the gap that requires it, in a comment at the flag:

- The four `unused-*` flags exist because the generator emits helper families
  wholesale -- equality, print, union, heap, io -- which `docs/status.md`
  already records as an open gap. They stay until demand-driven helper emission
  lands, and narrowing them is that gap's job.
- `-Wno-maybe-uninitialized` is different in kind and should not survive. The
  other four suppress warnings about code that is provably harmless; this one
  suppresses a warning about reading storage that may never have been written,
  which is precisely a "compiles and behaves wrongly" defect. Remove it,
  measure what fires, and record each remaining instance rather than
  re-suppressing the class.

A suppression added later requires the same treatment: a named owning gap, or
it does not go in.

### Sanitizers

Add a fourth tier under Clang: `-fsanitize=address,undefined`.

This is the tier that catches the second severity. ASan would have caught the
RFC 0084 C3 guard-page fault as a heap overflow at the moment of corruption
rather than as silent damage; UBSan catches signed overflow, misaligned access,
and invalid enum values in generated arithmetic that `-Werror` cannot see.

**ThreadSanitizer is deliberately excluded, with a reason.** TSan is the tool
that would have found the four Task parking defects RFC 0122 fixed -- one fiber
running on two OS threads is exactly its specialty. But the Hexal scheduler
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

The suite runs what the host supports and reports the rest as unrun, not as
passed. A claim that is only ever validated on one platform stays recorded in
`docs/status.md` as half-verified until the other runs it.

## Coverage backlog

The required target list is the set of claims `docs/status.md` currently
records as textually asserted only. Implementation works through it; each item
moves out of the coverage-gap entry as its test lands.

- Every `[Runtime Error]` message fires from a program that provokes it, with
  its exact text. This is the tier-3 backlog and the largest item.
- `print`'s exact output forms for every printable type.
- RFC 0084: a `try` in a nested block evaluates its operand once; `try spawn`
  in a loop spawns exactly one task per iteration and leaks none.
- RFC 0085: a fiber overflow reaches the guard page and traps; resident cost
  per Task; 10,000 concurrently live Tasks; a 64 KiB reserve overflowing sooner
  than 1 MiB.
- RFC 0087: a slice over multi-byte input is byte-identical to the scanning
  version; a concatenated rune count matches an independent scan.
- RFC 0115: `push`, `free`, and Dict `insert` during traversal trap with the
  collection-modified message rather than looping or reading freed memory.
- RFC 0122: immediate yield, join completion, Channel wake, and Mutex wake
  never run one fiber on two workers and never lose a wake; a contended Mutex
  transfers ownership without trapping its selected waiter.
- RFC 0123: allocation failure traps; the removed double-free and
  wrong-allocator messages do not appear.

## Non-goals

- Making the external suite part of the default gate.
- A build driver, project files, linking against foreign objects, or artifact
  materialization outside a temp directory. ADR 0055 owns that layer; this
  suite writes to `t.TempDir()` and nothing else.
- Cross-compilation, target profiles, or non-host ABIs. RFC 0052 owns those.
- `clang-cl`, MSVC's own `cl.exe`, or any fourth toolchain.
- Executing cross-built binaries. The cross-target tier compiles and links; an
  emulator or remote runner is out of scope.
- Fixing the Windows `<threads.h>` defect. This RFC records and reproduces it;
  its own spec owns the fix.
- ThreadSanitizer, for the reason recorded above.
- Performance benchmarking of generated code.
- Fixing the generated dead-code emission that the `unused-*` suppressions
  tolerate.

## Validation

This section is exhaustive. RFC 0125 is complete only when every item passes:

- `go test ./...` passes on a machine with neither GCC nor Clang installed and
  invokes no external process.
- `go test -tags c23 ./...` fails with a clear message when any of the three
  toolchains is missing, and never skips.
- Every tier runs under all three toolchains; a program accepted by one and
  rejected by another fails the suite and is recorded as a divergence.
- The discovered toolchain versions and their default targets appear in the run
  log.
- The cross-target tier compiles **and links** at least one non-host target;
  a compile-only check does not satisfy it, because the musl finding above is a
  link error.
- The concurrency tier runs where `<threads.h>` exists and is reported as unrun
  elsewhere, never as passed.
- Tier 1 uses `-std=c23 -Wall -Wextra -Werror` and every suppression names its
  owning gap in a comment.
- `-Wno-maybe-uninitialized` is removed and every instance it was hiding is
  either fixed or individually recorded.
- Tier 2 asserts exact stdout, not a substring or an exit code alone.
- Tier 3 asserts both a non-zero exit and the exact `[Runtime Error]` text.
- The sanitizer tier runs under Clang with `-fsanitize=address,undefined` and
  fails on any report.
- No test writes outside `t.TempDir()`.
- Platform-specific claims run where their code exists and are reported as
  unrun, never as passed, elsewhere.
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
