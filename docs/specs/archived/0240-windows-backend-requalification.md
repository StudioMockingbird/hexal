# RFC 0240: Windows Backend Re-qualification

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed. Implemented and verified 2026-09-24 on an x86-64 Windows
  host with Clang 23.1.2 and MinGW-w64/UCRT. Doctor passes end to end; the
  tagged c23 suite runs green on Windows (1121s); the signed-overflow fixture
  traps in debug and completes in release; the e2e heap/Task/IO/text fixture
  produces fixed stdout; the snippet manifest moved only for the package
  `#ifndef` guards that deduplicate feature-test defines under the harness's
  `-Werror`; `docs/reference.md` gained the Build modes section with the
  per-lane UB-reporting difference; build-mode measurements are recorded in
  `docs/benchmarks.md`
- Created: 2026-09-24
- Scope: restore a native Windows build path beside the Linux one — embed the
  `x86_64-windows-gnu-ucrt` runtime pack, qualify installed Clang against
  MinGW-w64/UCRT, and make the driver's host, triple, and feature-define
  choices per-target instead of per-release
- Origin: archived RFC 0217 deferred this explicitly. Its non-goals name
  "Native Windows backend requalification; a later RFC must qualify installed
  [Clang]", and its target section says it "deliberately retires the native
  Zig-powered Windows driver until a focused RFC qualifies Clang/MinGW-w64/UCRT
  and a Windows runtime pack." That RFC was never written; this is it
- Depends on: the rebuilt Windows runtime pack (2026-09-24, Clang 23.1.2, all
  three archives validated by a combined probe and recorded in `lib/BUILD.md`)
  and implemented RFC 0217's installed-Clang driver
- Coordinates with: archived RFC 0187 (build modes), RFC 0158 (leak detection,
  whose Linux gate this RFC must not break), deferred RFC 0185 (ASan), and
  archived RFC 0125 (external C23 validation)
- Does not change: Hexal syntax, semantics, the string-in/string-out compiler
  boundary, generated C23 as the backend representation, or the Linux lane's
  behaviour

## Summary

Hexal generates correct Windows C today and cannot build it. The
`x86_64-windows-gnu-ucrt` compiler target is intact — the checker and generator
branch on it, and pure-Go tests cover its output — but the driver rejects it
before running any external command.

This RFC restores the build path. The work is smaller than it looks, because
the two things that sound hardest are already done: the runtime pack was
rebuilt and validated on 2026-09-24, and `Options` already takes a target
profile so the leak flag stays Linux-only.

What remains is six driver changes and one design decision forced by the
toolchain.

## What is already done

Recorded so this RFC's scope is not overestimated.

- **The runtime pack exists, is current, and is validated.** All three archives
  were rebuilt from the pinned module commits with Clang 23.1.2 and `llvm-ar`
  23.1.2, replacing the Zig 0.16.0 artifacts that RFC 0217 orphaned. libuv is
  354,654 bytes, mimalloc 285,880, utf8proc 349,994 — the whole pack dropped
  from 3.43 MB to 0.99 MB and now sits alongside the Linux pack's sizes. A
  combined probe links all three, hands libuv the mimalloc allocator through
  `uv_replace_allocator` as the generated scheduler bootstrap does, runs a real
  loop, calls `uv_exepath`, and decodes U+1F600. `lib/BUILD.md` carries the
  build identity, per-archive recipes, digests, and that probe; all 34
  `manifest.json` entries verify.
- **`Options` is already per-target.** `internal/driver/mode.go:107` is
  `Options(mode BuildMode, target compilerTypes.TargetProfileID)`, and
  `isLinuxTarget` gates `-fsanitize=leak` so a MinGW link never sees it. RFC
  0158's Phase 2 landed this, and its comment already anticipates "when the
  Windows driver returns."
- **The C-header import path is target-aware.** `normalize.go`'s
  `scalarTarget` returns `TargetWindowsUCRT` for every non-Linux identity, so
  LLP64 scalar mapping is already correct for Windows.
- **The generated Windows C still compiles and runs.** This was the load-bearing
  assumption of the whole RFC and it was measured rather than trusted, because
  the lane has not been exercised since RFC 0217 and the missing
  `replace_windows.go` proved unexercised lanes rot. A program using the heap, a
  `String` literal, pointer dereference, `spawn`/`join`, and `print` was
  compiled through the string-in/string-out API with
  `Project{Target: TargetX86_64WindowsGNU}`, emitting 20 artifacts and demanding
  libuv and mimalloc. All 8 generated `.c` files compiled clean under
  `clang --target=x86_64-w64-windows-gnu -std=c23`, linked against the rebuilt
  pack and the ten system libraries, ran, and produced the expected output with
  exit 0 — including a Task dispatched through libuv's scheduler. The
  generator's Windows branches have not bit-rotted.

## The blocker that changes the design

**UBSan's diagnostic runtime does not exist for `x86_64-w64-windows-gnu`.**
Measured:

```text
$ clang --target=x86_64-w64-windows-gnu -O0 -g -ffp-contract=off \
    -fsanitize=undefined -fno-sanitize-recover=all t.c -o t.exe
ld.lld: error: could not open 'liblibclang_rt.ubsan_standalone.a':
  no such file or directory
```

The LLVM Windows installer ships compiler-rt for the MSVC target only:
`lib/clang/23/lib/windows/` holds `clang_rt.ubsan_standalone-x86_64.lib`, and
`lib/clang/23/lib/x86_64-pc-windows-msvc/` exists while no MinGW equivalent
does. This is the same failure class that deferred RFC 0185, where Zig's
Windows ASan runtime was unavailable.

Archived RFC 0187 makes the UB backstop part of what debug mode *is*, so
silently dropping it on Windows would make one mode name mean two different
contracts. The resolution is **trap mode**, which needs no runtime at all
because it emits a trap instruction instead of calling a handler:

```text
clang --target=x86_64-w64-windows-gnu -O0 -fsanitize=undefined \
  -fsanitize-trap=undefined ub.c -o ubtrap.exe
```

Measured against real undefined behaviour — signed overflow,
`value * 2147483647` with `value = 2147483647`:

| Build | Result |
| --- | --- |
| no sanitizer | prints `1`, exits 0 — UB unnoticed |
| `-fsanitize-trap=undefined` | prints `before`, then **Illegal instruction**, exit 132 |

So Windows debug mode keeps a real UB backstop. What it loses is the
diagnostic text: Linux prints which check failed and where, Windows halts. That
is the same bargain Hexal already makes for its own runtime traps, and it is
strictly better than no backstop.

**This is a contract difference between lanes and must be stated in
`docs/reference.md`'s Build modes section**, which RFC 0158 introduces. Debug
mode detects undefined behaviour on both lanes; only the reporting differs.

## Driver changes

Each item is a measured defect, not a guess.

### 1. `checkHost` rejects every non-Linux host

`internal/driver/driver.go:405` hard-codes `runtime.GOOS != "linux" ||
runtime.GOARCH != "amd64"`. It becomes a lookup over the qualified host set,
with `windows/amd64` added. The diagnostic keeps naming the host and the
qualified set rather than one release's single option.

### 2. `driverProfiles` holds exactly one pair, and the triple is a constant

`internal/driver/profile.go:21` carries `qualifiedTriple = "x86_64-linux-gnu"`
as a package constant, beside a one-element registry whose comment states
"There is exactly one qualified pair." The triple moves onto the profile
record and the registry gains `x86_64-windows-gnu-ucrt` →
`x86_64-w64-windows-gnu`.

**This is the largest item in the RFC, and its size is easy to underestimate.**
The constant has 26 use sites across six files, because every external
invocation names a triple:

| File | Sites |
| --- | ---: |
| `doctor.go` | 17 |
| `frontend.go` | 4 |
| `driver.go` | 2 |
| `profile.go` | 2 |
| `foreign.go` | 1 |
| `mode.go` | 1 |

Each becomes a read of the selected profile. Two deserve attention rather than
mechanical replacement:

- `mode.go:189` writes the triple into `buildIdentity`. That is correct and
  must stay: the triple belongs in the cache key, and making it per-target is
  what keeps two targets' caches from colliding. The Validation item for cache
  keys covers this.
- `doctor.go`'s 17 sites are the probes. Those are where a Windows toolchain
  actually gets qualified, so they are not a find-and-replace — each probe has
  to be reconsidered for whether it means the same thing on MinGW. The UBSan
  probe already does not; see The blocker that changes the design.

### 3. `executableFile` uses the Unix execute bit

`internal/driver/driver.go` tests `info.Mode()&0o111 != 0`. Go on Windows
reports no execute bit. Measured:

```text
C:\Program Files\LLVM\bin\clang.exe    mode=-rw-rw-rw-  regular=true  execbit=false
```

So the current check rejects `clang.exe` itself, and `-cc` would fail with "is
not an executable file" for a perfectly good compiler. On Windows the test
becomes a regular file with an executable extension; the Unix branch is
unchanged.

### 4. `linuxFeatureDefines` is applied unconditionally

`driver.go:580` defines `-D_POSIX_C_SOURCE=200809L` and five sites append it:
`driver.go:594`, `frontend.go:75`, `frontend.go:132`, `frontend.go:253`, and
`doctor.go:113-114`. Three of those are the C-header import path, where
declaring POSIX source on a MinGW header set is wrong. It becomes a per-target
lookup returning the POSIX defines for Linux and the pack's Windows defines
otherwise.

### 5. `ubsanProbe` hard-codes the Linux triple

`doctor.go` already threads a target into `Options(ModeDebug, target)` but then
passes the `qualifiedTriple` constant to `CompileOne`. It reads the selected
profile's triple, and on Windows asserts trap-mode behaviour rather than a
runtime link.

### 6. `lib/pack.go` embeds one profile

The embed set gains the Windows pack. RFC 0236 recorded that `go:embed` cannot
exclude, so the set is enumerated explicitly and a pattern that admits the
wrong profile's archives is caught by the existing completeness walk in
`verifyRuntimePack`, which rejects any file the manifest does not list.

## Non-goals

- **Cross-compilation.** A Windows host builds Windows; a Linux host builds
  Linux. `checkHost` still pairs host with target, because the driver links and
  runs what it produces. Cross-compilation is a separate decision that would
  need a bundled sysroot per target.
- **ASan on Windows.** Deferred RFC 0185 owns it, and the runtime gap measured
  above applies to it more severely than to UBSan — ASan has no trap mode.
- **Leak detection on Windows.** LSan does not exist for this target. RFC
  0158's `isLinuxTarget` gate already handles this and must not be widened.
- **Any other target profile.** macOS, musl, aarch64, and RISC-V remain
  commitments without drivers. This RFC restores one lane, not the matrix.
- **Changing the Linux lane.** Every change here is additive or a
  per-target generalization of something currently hard-coded.

## Validation

This section is exhaustive.

Toolchain and pack:

- `hexal doctor` passes on an x86-64 Windows host with installed Clang 18 or
  newer and MinGW-w64/UCRT, verifying every Windows pack payload against
  `manifest.json` and running the combined-archive probe.
- A Clang whose MinGW target cannot link the pack fails doctor with a
  diagnostic naming the toolchain, not a link error from a later stage.
- The Windows pack is embedded in `bin/hexal`, and a build on a host with no
  adjacent `lib/` tree succeeds.
- The embed set admits exactly the shipped profiles: a manifest-listed path
  that is not embedded fails verification, and an embedded file no manifest
  lists fails the completeness walk.

Building and running:

- A Hexal program that uses the heap, a Task, IO, and text compiles, links, and
  runs on Windows, producing the same stdout as the Linux lane for the same
  source.
- The ten Windows system libraries recorded in `lib/BUILD.md` — `psapi`,
  `shell32`, `user32`, `advapi32`, `bcrypt`, `iphlpapi`, `userenv`, `ws2_32`,
  `dbghelp`, `ole32` — are declared by the driver and appear in the link in
  manifest order.
- `-cc` accepts a path to `clang.exe` and rejects a directory and a missing
  file, each with its existing diagnostic. The execute-bit regression is
  covered: `clang.exe` is accepted on Windows.

Modes:

- Debug on Windows compiles with `-fsanitize=undefined -fsanitize-trap=undefined`
  and links without a compiler-rt runtime.
- A program with signed overflow built in Windows debug mode halts on the trap
  and exits non-zero; the same program in release mode does not.
- Debug on Linux is unchanged: it still uses `-fno-sanitize-recover=all` with
  the diagnostic runtime, and still carries `-fsanitize=leak`.
- No Windows build carries `-fsanitize=leak` or `-fsanitize=address`.
- Release mode is byte-identical in its option set on both lanes except for the
  target triple.
- The build identity includes the target triple, so the same source built for
  two targets produces two cache entries and never reuses one for the other.

Target-qualified generation:

- Generated C for `x86_64-windows-gnu-ucrt` selects the Windows entrypoint,
  threading, fiber, IO, process, signal, and terminal paths, as the checker and
  generator already do; this RFC changes no generator branch.
- The host-neutral zero `Project{}` output is unchanged, so the snippet
  manifest moves no hash.
- The C-header import path uses Windows feature defines and LLP64 scalar
  mapping on the Windows lane, and POSIX defines with LP64 on Linux.

Suite:

- `go test ./...`, `go vet ./...`, and `gofmt -l` pass on both hosts.
- The tagged `c23` lane runs on Windows with the Windows toolchain and reports
  which fixtures are lane-qualified.
- `docs/reference.md`'s Build modes section states that debug detects
  undefined behaviour on both lanes and that Windows reports it as a trap
  rather than a diagnostic.

## Implementation plan

### Phase 1 — make the driver's target facts per-target

1. Move the Clang triple onto the profile record and add the Windows pair.
2. Replace the `qualifiedTriple` constant's uses with the selected profile's
   triple, including in `ubsanProbe`.
3. Turn `linuxFeatureDefines` into a per-target lookup and update all five
   application sites.
4. Fix `executableFile` for Windows.

Each is a pure generalization: the Linux lane's behaviour must be identical
before and after, which the existing suite checks.

### Phase 2 — widen the host gate and embed the pack

1. Add `windows/amd64` to the qualified host set and reword the diagnostic to
   name the set.
2. Extend `lib/pack.go`'s embed set to the Windows pack.
3. Confirm doctor verifies the Windows pack end to end.

### Phase 3 — the debug-mode backstop

1. Add trap mode to debug's option set for non-Linux targets, keeping
   `-fno-sanitize-recover=all` with the diagnostic runtime on Linux.
2. Add the signed-overflow fixture and assert it halts in debug and not in
   release.
3. Update `docs/reference.md`'s Build modes section with the per-lane reporting
   difference, after behaviour stabilizes.

### Phase 4 — validation lane

1. Run the tagged `c23` suite on Windows and record which fixtures qualify.
2. Add the end-to-end Windows build fixture: heap, Task, IO, and text in one
   program, compiled, linked, run, and compared against the Linux lane's
   stdout.
3. Record the measured result in `docs/benchmarks.md`'s build-mode section,
   which currently holds Windows numbers from the retired Zig driver.

## Implementation readiness

Implementation-ready. Every blocker is measured rather than assumed, on the
host this would run on:

| Item | Measured |
| --- | --- |
| MinGW target compiles and links | yes, Clang 23.1.2, `windows.h` program runs |
| Runtime pack | rebuilt, 0.99 MB, combined probe passes, 34/34 digests verify |
| UBSan diagnostic runtime | **absent for MinGW**; MSVC-only compiler-rt present |
| UBSan trap mode | links, and halts on real signed overflow (exit 132) |
| `executableFile` on Windows | rejects `clang.exe` — `mode=-rw-rw-rw-` |
| `Options(mode, target)` | already per-target; leak flag already Linux-gated |
| C-header scalar mapping | already returns LLP64 for non-Linux |
| `qualifiedTriple` blast radius | 26 sites, six files, 17 of them doctor probes |
| Generated Windows C | 8/8 files compile under `-std=c23`; links, runs, correct output |

The one design decision — trap mode instead of the diagnostic runtime — is
forced by the toolchain rather than chosen, and it preserves the property that
matters: debug mode detects undefined behaviour on both lanes.

### What is and is not proven

Proven end to end, by hand, on this host: source in, Windows C out, compiled,
linked against the rebuilt pack, executed, correct output. That covers the
generator, the pack, the C dialect, the system-library link set, and the
scheduler.

Not proven, and the honest residue of this RFC: **none of it went through the
driver.** Every step above was run manually. The driver changes in Phases 1 to 3
are the work, and their correctness is established by the suite, not by this
evidence. What the evidence retires is the risk that the work is pointless
because the lane underneath it has rotted — it has not.

The remaining risk is narrower than breadth: it is that the 17 doctor probes
encode Linux assumptions beyond the triple. One of them demonstrably does, and
that one was found by running it. The other sixteen were read, not run, and
Phase 2 is where that changes. A probe that turns out to mean something
different on MinGW is a spec amendment, not a surprise to absorb during
implementation.
