# RFC 0247: Compile-Time and Runtime Memory Diagnostics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; option inventory and recommendation recorded,
  implementation not authorized until the diagnostic bundle is selected
- Created: 2026-09-26
- Scope: improve memory-bug diagnosis for Hexal programs without adding an
  ownership/lifetime type system or changing release representations
- Depends on: the implemented local memory analysis, build modes, runtime
  checks, target profiles, Clang backend, libuv/mimalloc runtime packs, and
  qualified C23 test lane
- Coordinates with: deferred RFC 0185 (ASan fiber coverage), rejected RFC 0158
  (Linux leak detection), archived RFC 0160 (memory-bug coverage inventory),
  archived RFC 0165 (local memory diagnosis), and archived RFC 0187 (build
  modes)
- Reference synchronization: none until a bundle is selected. Any selected
  user-visible debug-mode behavior must update `docs/reference.md` after it is
  implemented and verified

## Purpose

Hexal should diagnose memory mistakes twice where practical:

1. **At compile time**, when the checker can prove a mistake cheaply from
   local types, provenance, aliases, and control flow.
2. **At runtime in a diagnostic build**, when the mistake depends on an
   address, allocation lifetime, foreign code, escaped alias, task schedule,
   or input that static local analysis cannot decide.

No one mechanism covers both. This RFC inventories the complete option space,
records current facts and prior measured results, and proposes combinations to
review. It deliberately does not yet choose a bundle.

## Governing constraints

Any selected design must preserve these existing contracts:

1. `compiler.Compile` remains mode-blind and in-memory. Build mode changes how
   generated C is compiled, not the generated C text.
2. Debug and release accept the same Hexal programs and have the same language
   semantics. Compile-time memory errors therefore apply in every mode.
3. Bounds, allocation-size, conversion, allocator, handle, and other
   language-mandated runtime checks remain in release builds. A sanitizer is a
   diagnostic backstop, not a replacement for language semantics.
4. The checker remains local and forward-only. No analyzer pass, lifetime
   system, region inference, implicit destruction, or general borrow checker
   is introduced.
5. Safe Hexal preserves direct C representations and trivial C interoperability.
   Debug-only fat pointers or a different debug ABI are not acceptable.
6. Runtime diagnostic capability is qualified per target profile. The target
   matrix is a commitment, not evidence that every profile presently has every
   sanitizer runtime.
7. Native dependencies and foreign translation units are part of the claim.
   A diagnostic mode must state whether generated Hexal C, Hexal runtime C,
   libuv, mimalloc, utf8proc, imported C sources, and precompiled foreign
   objects are instrumented or opaque.
8. A debug tool may report a program defect that the checker could not prove.
   It must not silently change program behavior, repair memory, or claim that a
   clean run proves absence of memory defects.

## Verified current baseline

### Compile-time checks

The checker already rejects locally provable cases including:

- use after free and double free through tracked allocation identities and
  copy-propagated local aliases;
- a free of an address traceable to local storage;
- a release through a provably wrong Heap, Stash, or Pool;
- a Pool destroy with a locally tracked live slot;
- a Stash reset/destroy followed by use through a locally tracked allocation;
- nullable-pointer dereference without narrowing;
- constant out-of-range Array access;
- allocation without an initializer; and
- pointer operations that require `unsafe do ... end`.

Parameters, object members, collection-reached pointers, foreign pointers,
escaped aliases, and physical leaks remain undecided when local analysis
cannot prove their identity or lifetime. The current rule is
**undecided means accepted**.

### Runtime checks

Generated programs already enforce language-level checks for dynamic bounds,
allocation-size overflow, invalid dynamic alignment, Pool capacity and slot
state, allocator identity where runtime evidence exists, resource/handle
lifecycle, collection mutation during iteration, task stack exhaustion, and
selected concurrency and IO lifecycle errors. These are ordinary language
semantics and remain present in every build mode.

### Toolchain checks

Debug builds currently compile generated Hexal C with UBSan:

```text
-O0 -g -ffp-contract=off -fsanitize=undefined -fno-sanitize-recover=all
```

The qualified Linux/Clang lane links the diagnostic UBSan runtime. Windows
uses `-fsanitize-trap=undefined`, so it terminates at the same class of fault
without descriptive sanitizer text. Release carries no sanitizer
instrumentation.

The tagged C23 validation suite runs UBSan over runnable fixtures. A narrow
Linux LeakSanitizer test lane also exists, but **LeakSanitizer is not enabled
in user debug builds on any target**.

### Native runtime inputs

The checked-in libuv and mimalloc archives are optimized static inputs in both
build modes. They are not rebuilt with sanitizer, debug, secure, guarded, or
tracking options when a Hexal program is compiled. Foreign C compilation
currently strips Hexal's sanitizer options, and precompiled foreign objects
cannot be instrumented after the fact.

## Corrections to prior proposals and reviews

1. RFC 0158 is **Rejected**, not an implementation-ready bridge. Its final
   proposal would have added Linux LSan to user debug builds, but the project
   chose equivalent shipped capability across targets instead.
2. RFC 0158's mimalloc probes found that `mi_heap_visit_blocks` cannot
   enumerate the runtime's ordinary `mi_malloc` traffic. Routing every
   allocation through an explicit mimalloc heap measured a persistent 7-12%
   cost and was rejected as an always-on tax for an optional diagnostic.
3. Current user debug builds have UBSan, not LSan. LSan is test-lane-only.
4. ASan is not a one-flag change for Hexal. Task fiber stacks require balanced
   sanitizer switch annotations, and the native dependency pack must be
   compatible with the ASan runtime.
5. Mimalloc statistics report allocator aggregates; they do not identify each
   leaked Hexal allocation.
6. `MIMALLOC_SHOW_STATS=1` is useful telemetry, not a leak detector.

## Detection classes

| Defect | Static local proof | Existing Hexal runtime | Native diagnostic option |
| --- | --- | --- | --- |
| Direct/local use after free | yes | not needed for proven case | ASan, mimalloc debug, quarantine |
| Escaped/parameter/member use after free | generally no | no general check | ASan or Hexal shadow metadata |
| Direct/local double free | yes | Pool validates slots | ASan, mimalloc debug |
| Physical memory leak | no | no | LSan or Hexal allocation registry |
| Dynamic collection bounds | sometimes | yes | ASan is only a backstop |
| Raw/foreign buffer overflow | usually no | no general foreign check | ASan, guard pages, Valgrind |
| Uninitialized read | prevented in safe Hexal | no general foreign check | MSan or Valgrind |
| Stale pointer after Stash reset | local aliases only | no general escaped-alias check | poisoning, ASan, or generation metadata |
| Stale Pool slot after reuse | local facts plus Pool address/live validation | partial | Pool generations or ASan |
| Wrong allocator/free | local provenance, Pool runtime checks | partial | mimalloc checked free or ASan |
| Data race | not a memory-lifetime check | atomics/locks enforce their own contracts | TSan or a separate race detector |
| Generator emits C undefined behavior | no source-program check | UBSan today | UBSan, ASan, MSan test lanes |

## Compile-time options

### C0 — Keep the implemented local checker

Continue the current must-proof facts for allocation identity, aliases,
freed-state, allocator kind, narrowing, constant bounds, and source lifetime.

**Benefits:** no new concepts, fast, deterministic, precise, already aligned
with compiler-assisted manual memory management.

**Limit:** it deliberately accepts anything whose identity or escape behavior
cannot be proved locally.

**Assessment:** mandatory baseline under every bundle.

### C1 — Extend cheap intraprocedural facts

Possible focused additions include:

- preserve allocation identity through more local `unsafe` pointer operations
  where the operation itself proves the same allocation;
- fold more literal bounds before runtime;
- track additional locally obvious Stash/Pool generation invalidations; and
- diagnose a fill/write performed through a locally proven freed allocation.

Each addition must be independently specified and must reject only a proven
mistake. A broad "memory analyzer" abstraction is not introduced.

**Assessment:** recommended case by case after a reproducing gap exists.

### C2 — Interprocedural effect summaries

Private functions could carry inferred summaries such as:

```text
parameter 0 is freed
result aliases parameter 1
result is a new Heap allocation
parameter 2 escapes
```

This could diagnose cross-function double free and use after free without new
source syntax.

**Cost:** recursion, generics, imported modules, indirect calls, summary
invalidation, and incremental compilation all become part of the analysis.
It is analysis growth toward the lifetime model Hexal avoids.

**Assessment:** defer until repeated real bugs justify a narrow summary form.

### C3 — Static rejection of non-escaping local leaks

The checker could reject an allocation that provably:

1. originates in one function;
2. never escapes through a return, call, member, collection, global, Task, or
   foreign boundary; and
3. is not released on every exit path, including `defer` and `errdefer`.

This is sound for a narrow case, but it changes language acceptance and needs
an explicit convention that such allocations must be freed before return.
Escaping leaks remain undecidable.

**Assessment:** open language-policy decision, not implied by current local
freed-state analysis.

### C4 — Warning/lint diagnostics for suspicious memory use

Examples include likely off-by-one loop bounds or an allocation that may leak
on some paths but cannot be proved to do so.

Hexal has no non-fatal diagnostic channel or suppression model. Adding one is
a separate language/tooling decision.

**Assessment:** unavailable until warnings are designed; do not smuggle
uncertain findings into compiler errors.

### C5 — Clang Static Analyzer over generated C

Run `clang --analyze` against the complete generated C project with the exact
target triple, sysroot, headers, and compile definitions. It can find some
leaks, invalid frees, use-after-free paths, and null/bounds mistakes in
generated or imported C.

Limitations:

- it sees C rather than Hexal semantic types and allocation identities;
- mimalloc, Stash, Pool, libuv, and Hexal runtime APIs need analyzer models or
  annotations for useful results;
- diagnostics may point into generated/runtime C despite `#line` mapping;
- analysis is neither exhaustive nor necessarily target-uniform; and
- it duplicates some checker work while being less authoritative about Hexal.

**Assessment:** worthwhile as a bounded prototype and possible CI oracle, not
as the foundation of Hexal's compile-time safety contract.

### C6 — Ownership, affine types, or borrow/lifetime checking

These systems can make broad classes of leaks and stale aliases unrepresentable.
They also add moves, lifetime relations, escape restrictions, annotations, and
substantial C-interop friction.

**Assessment:** rejected for this arc. It conflicts with Hexal's small surface
and compiler-assisted manual-management direction.

## Runtime and toolchain options

### R0 — Existing semantic guards plus UBSan

Keep all language-required traps in every mode and UBSan in debug. This is the
minimum baseline and remains useful even if stronger tools are added because
ASan/LSan do not implement Hexal's semantic contracts.

**Assessment:** mandatory baseline.

### R1 — AddressSanitizer

ASan can detect heap/stack/global overflows, use after free, invalid and double
free, stack use after scope/return, overlapping memory operations, and other
invalid accesses. It usually provides allocation, release, and fault stacks.

Hexal integration requires:

1. inventorying every root/worker/park/resume/complete/abandon fiber switch;
2. balanced `__sanitizer_start_switch_fiber` and
   `__sanitizer_finish_switch_fiber` calls;
3. ASan-instrumented generated C and Hexal runtime C;
4. an ASan-compatible mimalloc build using its `MI_TRACK=ASAN` support, or a
   deliberately selected diagnostic allocator;
5. compatible libuv and utf8proc artifacts, or a documented opaque boundary;
6. a symbolizer and stable source mapping;
7. a faulty canary proving detection rather than merely successful linking;
8. policy for imported C source, which can be instrumented, and precompiled C
   objects/libraries, which cannot; and
9. qualification per target profile.

ASan, TSan, and MSan cannot be combined in one program. ASan therefore implies
a distinct diagnostic lane if the others are ever adopted.

**Assessment:** highest-value broad invalid-access detector. Reactivate RFC
0185 for a Linux/Clang implementation first; do not claim fiber coverage until
the transition map and canary pass.

### R2 — LeakSanitizer

LSan reports heap allocations unreachable at exit. It can run with ASan or as
a stand-alone sanitizer and adds little cost until the exit scan.

Hexal must classify:

- process-lifetime registries and argument snapshots;
- libuv and mimalloc retained state;
- live/detached Tasks at root completion;
- memory reachable only from suspended fiber stacks;
- Stash/Pool retained blocks; and
- allocations owned by imported libraries.

Possible integration uses root-region registration, ignored-object APIs, and
small reviewed suppressions. A suppression must name an intentional owner, not
hide an unexplained leak.

**Assessment:** valuable Linux/POSIX CI lane and optional deep diagnostic. It
is not presently an equivalent cross-target default and must not be described
as one.

### R3 — Debug/secure/guarded mimalloc pack

A separate checked-in diagnostic mimalloc archive can enable combinations of:

- `MI_DEBUG=FULL` for allocator assertions and expensive heap invariants;
- padding/redzones for byte-precise block-overflow detection;
- `MI_FREE_IS_CHECKED=ON` for invalid free validation;
- `MI_SECURE=ON` for protected metadata, encoded free lists, and corruption
  detection;
- `MI_GUARDED=ON` or `FULL` for sampled OS guard pages; and
- detailed statistics and error output.

Runtime options can select guard sample rate, seed, size range, precise
placement, statistics, and error reporting. Guarding every allocation is
expensive; deterministic sampled guarding is a more practical default.

Limitations:

- allocator statistics do not identify leaks;
- it sees backing allocations, not every logical Pool slot or Stash object;
- it does not provide Hexal binding/source ownership by itself; and
- current native packs are optimized archives, so this requires new
  target-qualified diagnostic artifacts and build identities.

**Assessment:** strong pragmatic cross-platform layer and useful before ASan
fiber support. It complements rather than replaces ASan.

### R4 — Hexal-owned allocation registry

A debug runtime could record, per physical allocation:

```text
address, byte size, allocator kind, live state,
allocation source, release source, and optional generation
```

It could report outstanding allocations at exit and reject some invalid frees.
Comprehensive use-after-free and bounds checking would additionally require
instrumenting every dereference/copy/access or changing pointer representation.

Prior evidence matters: mimalloc cannot enumerate ordinary `mi_malloc`
traffic, and routing all allocations through explicit heaps cost 7-12% in the
rejected RFC 0158 experiment. A debug-only `#if` path avoids release overhead
but still creates a second allocator lifecycle and substantial runtime code.

**Assessment:** do not build a general shadow allocator now. Consider narrow
metadata only where native tools cannot express a Hexal concept.

### R5 — Narrow Stash/Pool generations, poisoning, and quarantine

Debug-only allocator-specific checks can:

- increment a Stash generation on reset;
- assign a generation to each Pool slot reuse;
- mark released storage dead;
- poison invalidated bytes;
- delay reuse through a bounded quarantine; and
- use ASan poison/unpoison APIs when ASan is active.

This targets the semantic gap where physical backing memory remains allocated
after a logical Stash object or Pool slot becomes invalid.

Without fat pointers or an access lookup, a raw stale pointer does not carry
its old generation. Generation metadata therefore helps only where an access
path can recover and check the owner/slot, or when ASan poisoning provides the
actual invalid-access trap.

**Assessment:** promising narrow complement; specify separately after the
actual missed case and access mechanism are proven.

### R6 — Hexal shadow memory or fat/tagged pointers

A full debug shadow map could validate every access against live allocation
ranges. Fat pointers could carry bounds, owner, and generation directly.

Both approaches are large. Fat pointers change ABI, FFI, atomics, collection
layouts, generated C readability, and debug/release representation. Shadow
memory requires pervasive codegen instrumentation and effectively creates a
second sanitizer runtime.

**Assessment:** reject. Established sanitizers provide more value with less
Hexal-specific architecture.

### R7 — MemorySanitizer

MSan detects reads of uninitialized memory and can track origins. It generally
requires all code, including dependent libraries and often libc-adjacent code,
to be instrumented; partial instrumentation produces false reports. It is not
available across Hexal's target matrix and cannot combine with ASan.

Safe Hexal already requires initialization, so the remaining value is mostly
generator, runtime, unsafe, and foreign-code validation.

**Assessment:** defer as a specialized Linux CI experiment, not a user debug
contract.

### R8 — ThreadSanitizer

TSan detects data races rather than allocation lifetime errors. It needs broad
instrumentation, is expensive, cannot combine with ASan/MSan, and requires a
separate feasibility design for Hexal fibers, libuv workers, and scheduler
happens-before edges.

**Assessment:** separate future concurrency diagnostic mode, not part of the
initial memory bundle.

### R9 — Valgrind Memcheck

Memcheck can detect invalid accesses, invalid frees, uninitialized values, and
leaks on supported hosts without making it a Hexal build mode. Mimalloc can be
built with `MI_TRACK=VALGRIND` for better cooperation.

It is slow, external, and not uniformly available across the target matrix.

**Assessment:** optional Linux developer/CI oracle, never the sole Hexal
diagnostic mechanism.

### R10 — Guard allocators and hardware-assisted sanitizers

OS guard-page allocators can deterministically catch boundary overflows but
consume large amounts of virtual address space and system calls. HWASan or
hardware memory tagging can reduce overhead on supported AArch64 platforms but
is not a uniform desktop/server target capability.

Mimalloc guarded mode already provides the lower-complexity guard-page route.

**Assessment:** use mimalloc guarding first; revisit hardware-assisted tools
only per qualified target.

### R11 — Stack protector and fortified libc calls

`-fstack-protector-strong` and `_FORTIFY_SOURCE` can harden selected stack and
libc buffer operations. They do not diagnose general Heap/Stash/Pool lifetime
or leaks; fortification also depends on optimization and object-size knowledge.

**Assessment:** useful separate hardening audit, not the memory-diagnostic
story and not a substitute for ASan.

## Generated-C and foreign-code boundaries

Every runtime option must explicitly classify these compilation units:

| Unit | Can be instrumented? | Required decision |
| --- | --- | --- |
| Generated module C | yes | always instrument in selected lane |
| Generated Hexal runtime/component C | yes | always instrument in selected lane |
| Checked-in mimalloc archive | only by rebuilding/replacing pack | diagnostic pack or opaque boundary |
| Checked-in libuv archive | only by rebuilding/replacing pack | diagnostic pack or opaque boundary |
| Checked-in utf8proc archive | only by rebuilding/replacing pack | diagnostic pack or opaque boundary |
| Imported C source compiled by Hexal | yes | inherit selected sanitizer or deliberately opt out |
| User-supplied object/static library | no | document as opaque; sanitizer may still catch invalid accesses at instrumented call sites |
| System libc/SDK | generally opaque/intercepted | depend on tool support; do not claim full instrumentation |

The current policy strips sanitizer options from foreign C. Keeping that rule
means memory bugs inside imported C sources are outside any user-facing ASan
claim. Changing it affects C interoperability and needs explicit validation
against ordinary third-party C projects.

## Source-level reporting

Generated C carries `#line` mappings back to Hexal. A sanitizer stack in
generated module code should therefore name the Hexal logical source where the
tool respects line tables. Frames inside runtime components, libuv, mimalloc,
utf8proc, or foreign code naturally name their C source instead.

A selected lane must qualify:

- symbolizer discovery and deterministic path handling;
- Hexal `#line` mapping in sanitizer reports;
- allocation and release stacks across Task switches;
- distinction between `[Runtime Error]` traps and sanitizer reports; and
- one planted fault per claimed diagnostic family.

A build that merely accepts sanitizer flags proves nothing.

## Candidate bundles

### Bundle A — Minimal cross-target diagnostics

- C0 current local checker;
- C1 only for individually proven cheap facts;
- R0 language checks and UBSan;
- R3 target-qualified debug/secure mimalloc with sampled guard pages; and
- optional R9 Valgrind documentation on Linux.

**Benefit:** lowest new complexity, works before fiber annotations, no language
surface.

**Gap:** weaker diagnosis of arbitrary use after free and leaks.

### Bundle B — Toolchain-led deep memory diagnostics

- Bundle A;
- R1 ASan after RFC 0185;
- R2 LSan where qualified;
- ASan-aware/debug native dependency packs;
- sanitizer inheritance for imported C source; and
- CI canaries for access, fiber-stack, allocation, and leak failures.

**Benefit:** broadest high-ROI coverage while outsourcing shadow memory to
Clang and mimalloc.

**Gap:** target capability is asymmetric; opaque precompiled libraries remain.

### Bundle C — Hexal-owned symmetric debug runtime

- C0/C1;
- R0;
- R3;
- R4 allocation registry;
- R5 allocator generations/poisoning; and
- pervasive access validation sufficient to make the registry meaningful.

**Benefit:** Hexal-specific messages and potential target symmetry.

**Cost:** a second memory runtime, high codegen/runtime complexity, and a large
maintenance burden. Prior tracking evidence already rejected an always-on
variant.

### Bundle D — Stronger static rejection

- C2 interprocedural summaries;
- C3 non-escaping-local leak rejection; and
- one of Bundles A or B for dynamic cases.

**Benefit:** some bugs never execute.

**Cost:** increased checker/incremental complexity and a language-acceptance
change. It moves Hexal toward ownership analysis without adopting a coherent
ownership model.

## Provisional recommendation

The best fit with Hexal's goals is **Bundle B in stages**:

1. retain and incrementally improve the local checker only where proof is
   cheap and decisive;
2. retain all language-level runtime checks and UBSan;
3. add a separate target-qualified debug mimalloc pack with checked frees,
   full assertions, and deterministic sampled guarding;
4. reactivate RFC 0185 and implement ASan correctly for Task fibers on
   Linux/Clang;
5. add LSan as an opt-in/CI deep diagnostic where intentional roots are
   classified;
6. instrument imported C sources in that deep lane while describing supplied
   objects and system libraries as opaque; and
7. add narrow Stash/Pool poisoning or generation checks only for demonstrated
   gaps ASan cannot diagnose clearly.

Do not add a general tracking allocator, fat pointers, whole-program lifetime
analysis, MSan/TSan to the default debug mode, or new ownership syntax.

## Open decisions for review

1. **Default or explicit deep lane:** should ASan/LSan eventually be part of
   ordinary `debug`, or selected by a separate memory-diagnostic option?
   A separate deep lane is recommended because of target asymmetry, overhead,
   and incompatibility with future TSan/MSan lanes.
2. **Target-equivalence policy:** must every shipped target expose equivalent
   diagnostics before any target exposes them to users, or may capability be
   qualified per target? Per-target qualification is recommended; otherwise
   the weakest target permanently caps the strongest one.
3. **Static leaks:** should a provably non-escaping local allocation that is not
   freed on every exit path become a compile error? Keeping it accepted is
   recommended until Hexal explicitly adopts a must-free convention.
4. **Foreign C sources:** should the deep diagnostic lane pass sanitizer flags
   to imported C source? Yes is recommended, with a documented escape for
   incompatible projects; objects remain opaque.
5. **Diagnostic native pack:** should debug use separately built libuv,
   mimalloc, and utf8proc artifacts? Yes for the deep lane; at minimum mimalloc
   must be ASan-aware for a trustworthy allocator claim.
6. **Clang Static Analyzer prototype:** should a time-boxed prototype measure
   useful findings and false positives before it becomes any build command?
   Yes; do not promise a permanent lane without evidence.
7. **Leaks as failure:** should a detected leak merely report, fail tests, or
   alter a user's program exit status? CI should fail; interactive user runs
   should follow the sanitizer's explicit configuration until one consistent
   user contract is selected.
8. **Mimalloc guarding level:** sampled guarding or guard every allocation?
   Sampled, deterministically seeded guarding is recommended for ordinary
   debug; guard-every-allocation belongs to the deep lane.

## What a promoted implementation RFC must add

This proposal is not implementation-ready. After the decisions above, split
the selected work so each mechanism has one testable lifecycle:

1. one RFC for diagnostic native packs and mimalloc modes;
2. RFC 0185 revised for concrete ASan fiber integration and target gates;
3. one LSan ownership/suppression RFC if leak reporting is selected;
4. one checker RFC for any selected C1/C2/C3 behavior; and
5. one driver/interface RFC for a new diagnostic lane, if ordinary `debug` is
   not selected.

Each implementation RFC needs an exhaustive Validation section, complete
runtime/native dependency inventory, deliberately faulty canaries, source-map
assertions, target qualification, and a required sweep of superseded flags,
comments, tests, and status entries.

## Evidence sources

- [Clang AddressSanitizer](https://clang.llvm.org/docs/AddressSanitizer.html)
- [Clang LeakSanitizer](https://clang.llvm.org/docs/LeakSanitizer.html)
- [Clang MemorySanitizer](https://clang.llvm.org/docs/MemorySanitizer.html)
- [Clang ThreadSanitizer](https://clang.llvm.org/docs/ThreadSanitizer.html)
- [Clang UndefinedBehaviorSanitizer](https://clang.llvm.org/docs/UndefinedBehaviorSanitizer.html)
- [Clang Static Analyzer checker reference](https://clang.llvm.org/docs/analyzer/checkers.html)
- [mimalloc debug, secure, guarded, ASan, and Valgrind build options](https://github.com/microsoft/mimalloc/blob/main3/CMakeLists.txt)
- [mimalloc runtime options](https://microsoft.github.io/mimalloc/environment.html)
- [Valgrind Memcheck manual](https://valgrind.org/docs/manual/mc-manual.html)

## Validation

This is an option proposal, not an implementation specification. Its present
validation is documentary and exhaustive for this status:

- every mechanism distinguishes the defect class it can detect from those it
  cannot;
- current debug, UBSan, LSan-test, native-pack, foreign-C, and checker facts
  match the tree;
- rejected RFC 0158 evidence is preserved rather than reopened by implication;
- ASan remains gated on RFC 0185's fiber-transition design and execution
  qualification;
- no option claims equal availability across unqualified target profiles;
- the recommendation adds no ownership/lifetime syntax, analyzer pass, fat
  pointer ABI, or release overhead; and
- no compiler, runtime, driver, reference, or generated-C behavior changes
  under this proposal.
