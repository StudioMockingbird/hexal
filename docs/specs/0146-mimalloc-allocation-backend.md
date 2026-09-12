# ADR 0146: mimalloc Allocation Backend

- Kind: Architecture Decision Record (ADR)
- Status: Implementation-ready; implementation not started
- Created: 2026-09-08
- Updated: 2026-09-13
- Scope: use a pinned mimalloc v3 source revision as the backing allocator for
  Hexal-owned dynamic storage on the qualified `x86_64-windows-gnu` backend;
  later let libuv consume the same allocator
- Depends on: RFC 0052 (C compiler backend) and RFC 0055 (filesystem/build
  driver)
- Coordinates with: RFC 0145 (libuv runtime), ADR 0164 (object cache), RFC
  0165 (current non-affine memory direction), and the allocation contracts in
  `docs/reference.md`
- Does not change: Hexal syntax, `Heap`, `Stash<T>`, `Pool<T>`, collection,
  String, ownership, lifetime, failure, or thread-safety contracts

## Summary

Use one pinned mimalloc v3 source revision, compiled and statically linked by
the installed Zig backend, as the implementation of Hexal-owned general
allocation. Keep every language-level allocator contract in Hexal:

- `Heap` remains a stateless token selecting one thread-safe default allocator.
- `Stash<T>` remains a typed growable bump allocator with reset and destroy.
- `Pool<T>` remains typed fixed-capacity reusable-slot storage.
- Lists, Dicts, Strings, Channels, Mutexes, Tasks, Stashes, and Pools retain
  their current ownership and cleanup rules.

Mimalloc supplies raw storage. It does not become a Hexal type and its heaps or
arenas are not exposed as language allocators.

When RFC 0145 is implemented, install the same mimalloc functions through
`uv_replace_allocator` before the first other libuv call. This unifies
Hexal-owned and libuv-internal general allocation without pretending that
foreign libraries share Hexal ownership.

Mimalloc is linked into each generated executable that demands it. It is not
linked into the Go compiler process, and compiling Hexal itself with mimalloc
would not make mimalloc symbols available to generated programs.

## Motivation

The current default allocator delegates to C `malloc`, `calloc`, and `free`.
That implementation is small, but it leaves allocator performance,
fragmentation, cross-thread behavior, diagnostics, and target variation to each
platform C runtime.

Mimalloc is designed for language runtimes and supplies:

- scalable thread-safe general allocation;
- efficient cross-thread frees;
- bounded allocator metadata and fragmentation behavior;
- static-library and single-object builds;
- allocation statistics and heap introspection;
- optional secure and debug configurations; and
- first-class heaps when a later measured design needs them.

Hexal Tasks may migrate among scheduler workers. The default allocator must
therefore support allocation and release from different native threads without
making a Task carry thread affinity. Mimalloc's global allocation API matches
that requirement.

This ADR does not claim that allocator replacement is automatically faster.
Qualification includes application-shaped measurements against each target's
native allocator. The design adopts mimalloc for one controlled runtime and
consistent target behavior; benchmark results determine configuration, not
whether Hexal exposes another allocator choice.

## Decision

### 1. Dependency and version

- Begin qualification with the official mimalloc `v3.5.1` source release. The
  tag is a candidate, not authority by itself: resolve and record its complete
  commit identity and downloaded archive SHA-256 before executing any source.
  If it fails qualification, do not silently fall back to another version;
  return this ADR to Open Discussion with the evidence.
- Vendor the qualified exact mimalloc v3 source release and commit under
  `third_party/mimalloc/`, including its required license and attribution
  material.
- Keep one small Go package at `third_party/mimalloc/` that embeds the required
  source, headers, configuration files, license, and vendoring record with
  `go:embed`. C sources remain below subdirectories rather than beside the Go
  file, so the package does not require cgo.
- Do not use a system-installed mimalloc and do not download it during a
  build. The checked-in revision is the complete v1 dependency-resolution
  rule.
- Mimalloc is not a Git submodule. A normal clone, source archive, and offline
  build of Hexal contain the complete dependency.
- Compile the vendored `src/static.c` as a separate object with the installed
  Zig 0.16.0 backend and the vendored include directory. Build it under the
  source dialect and definitions supported by that pinned mimalloc revision;
  Hexal's C23 output dialect does not force third-party C sources to use C23.
- Link that object statically into each executable that demands mimalloc. The
  executable has no mimalloc shared-library dependency.
- Qualify v1 only for RFC 0052's `x86_64-windows-gnu` profile with dynamic
  UCRT linkage. Every later target independently qualifies the same pinned
  source before that target becomes supported.
- Do not introduce a prebuilt target-pack archive in v1. Compiling the object
  for each build is the simple correct baseline; ADR 0164 may later cache it
  under the complete source, configuration, Zig, target, and ABI identity.
- The vendoring record states the upstream repository and release URLs,
  release, complete commit identity, downloaded archive digest, retained-file
  set, per-file SHA-256 manifest, qualified compile definitions, and license.
- V1 carries no Hexal patch to mimalloc. If qualification requires modifying
  upstream source, return the ADR to Open Discussion rather than silently
  creating a private allocator fork.

### 1a. Installed-driver source location

An installed `hexal.exe` does not search for a repository checkout, sibling
source tree, environment variable, or current-working-directory dependency.
The driver reads mimalloc from the embedded vendored snapshot and materializes
the required files under the current build's isolated staging tree:

```text
<staging>/dependencies/mimalloc/include/...
<staging>/dependencies/mimalloc/src/static.c
```

It then compiles that staged `static.c` and supplies the staged include root.
Materialization preserves the embedded bytes exactly and validates every
embedded logical path with the same containment rules as compiler artifacts.
The staged copy is removed with the rest of the staging tree. This makes the
installed compiler self-contained with respect to mimalloc source without
placing third-party files in `CompilationResult.Files` or giving the core
compiler filesystem responsibility.

Conceptual command shape; Phase 1 replaces the placeholder with the exact
qualified definitions and dialect flags:

```text
zig cc -target x86_64-windows-gnu <mimalloc-flags> \
  -I <staging>/dependencies/mimalloc/include \
  -c <staging>/dependencies/mimalloc/src/static.c \
  -o <staging>/objects/mimalloc.o

zig cc -target x86_64-windows-gnu \
  <generated-objects> <staging>/objects/mimalloc.o \
  -o <staging>/program.exe
```

The driver records both commands through the existing C-compilation and link
stages. It never invokes CMake, a mimalloc installer, or another build system
at user build time.

### 2. Explicit allocation, not process-wide interposition

Hexal-generated runtime code calls the prefixed API directly:

```c
mi_malloc(size)
mi_calloc(count, size)
mi_free(pointer)
```

`mi_realloc` is deliberately absent from that list. Hexal's Heap boundary has
no reallocation operation -- List and Dict growth allocate, copy the live
prefix, and free -- and adding one would be a wrapper with no Hexal semantic
adaptation, which Decision 3 forbids. A future RFC may introduce in-place
growth on its own evidence; this one does not.

Decision 5 still passes `mi_realloc` to `uv_replace_allocator`, and that is not
an exception to the rule above. Libuv's replacement interface requires all four
callbacks, and libuv calls the reallocation one for its own storage. No
Hexal-generated code calls it.

Do not replace the process's global `malloc` symbols, use loader injection, or
force foreign C libraries through mimalloc. Global interposition differs across
object formats and linkers and can create mismatched allocation families.

The ownership rule remains simple:

- memory allocated by Hexal is released through Hexal;
- memory allocated by libuv after allocator installation is released by
  libuv through the installed callbacks; and
- memory allocated by a foreign library is released through that library's
  documented API unless its binding explicitly transfers ownership.

### 3. Heap lowering

Keep the existing generated operations stable:

```c
void *hex_heap_allocate(size_t size);
void *hex_heap_allocate_zeroed(size_t count, size_t size);
void hex_heap_free(void *pointer);
```

Its implementation changes only at the raw allocation boundary:

- `hex_heap_allocate` calls `mi_malloc` and retains Hexal's allocation-failure
  trap.
- `hex_heap_allocate_zeroed` retains the existing `ckd_mul` overflow
  distinction, then calls `mi_calloc` and retains the allocation-failure trap.
- `hex_heap_free` calls `mi_free`.
- Component-specific size checks remain at their current owning operations.
- Do not add wrappers for mimalloc operations that have no Hexal semantic
  adaptation.

Add two private generated-runtime operations, because the concurrency runtime
needs raw allocations whose failure is recoverable or has a site-specific
trap:

```c
void *hex_heap_allocate_or_null(size_t size);
void *hex_heap_allocate_zeroed_or_null(size_t size);
```

The first calls `mi_malloc`; the second calls `mi_calloc(1, size)`. Both return
`nullptr` on failure instead of trapping. These are Hexal semantic adaptations
-- the caller owns the failure policy -- not bare wrappers, so they do not
contradict the rule above. The zeroed form exists because Task, Channel, and
Mutex control blocks currently and deliberately rely on zero initialization;
rewriting them as manual field-by-field initialization would add risk without
removing a language or runtime concept.

### 3a. The concurrency runtime

`hexal/concurrency.c` currently makes 42 raw `malloc`, `calloc`, and `free`
calls that never reach the Heap boundary: root and spawned Task control blocks,
Task argument and result frames, fiber contexts, thread start records, Channel
control blocks and slot storage, Mutex control blocks, and the POSIX signal
alternate stack.

Those allocations move to the mimalloc boundary too. Leaving them native would
split one program across two allocator families, which is the condition
Decision 2 exists to avoid, and would exclude the allocations that motivate
this ADR most directly: a Task control block is allocated on one scheduler
worker and freed on another, which is exactly the cross-thread pattern the
Motivation cites.

They cannot use `hex_heap_allocate`, and the reason is a contract rather than a
preference. The Task-spawn path can fail at four general-allocation points --
argument frame, Task control block, result frame, and fiber context -- and
returns `nullptr`; the spawn site turns that into a recoverable `Error`.
`hex_heap_allocate` traps. Routing spawn through it would convert a recoverable
`Error` into a trap and change a language-visible failure contract this ADR
promises not to touch.

The rule is therefore:

- every concurrency allocation uses a nullable operation and leaves failure
  classification at its existing call site;
- the Task-spawn allocation chain and Channel and Mutex constructors continue
  to surface their existing recoverable failures;
- root scheduler, initial worker, and signal alternate-stack setup continue to
  use their exact site-specific traps;
- blocking-pool overflow-worker creation continues to fall back to the
  existing workers when native-thread creation fails;
- every existing `calloc(1, sizeof(...))` control-block allocation uses
  `hex_heap_allocate_zeroed_or_null(sizeof(...))`; ordinary byte regions and
  frames use `hex_heap_allocate_or_null`;
- only allocations whose established failure is the generic Heap allocation
  trap use `hex_heap_allocate`;
- every matching release uses `hex_heap_free`.

These distinctions are required: routing a site-specific failure through
`hex_heap_allocate` would replace messages such as `scheduler allocation
failed` with the generic `heap allocation failed` trap. Replacing established
zero initialization with manual field assignment would also make adding a
field to `hex_task`, `hex_chan`, or `hex_mutex` a latent initialization bug.

No allocation in `hexal/concurrency.c` calls the C allocation family directly
after this change. Fiber *stacks* are unaffected: they are `mmap`/`VirtualAlloc`
regions with a guard page, not heap allocations, and remain platform calls.

The source sweep covers both Windows and POSIX branches so the shared runtime
template has one allocator boundary. Only the selected Windows branch receives
runtime qualification in v1. The mechanically migrated POSIX branch is not a
supported-target claim; every future POSIX target must compile, run, and
qualify that branch before shipping. Retaining native allocation in dormant
branches would create a second architecture for later targets and make the
whole-file allocator invariant false.

### 4. Stash and Pool

Stash and Pool continue to obtain backing regions through Hexal's raw heap
boundary. They therefore use mimalloc indirectly without changing layout or
behavior.

Do not map either language type to a mimalloc heap or arena:

- a mimalloc arena is a backing virtual-memory region, not `Stash<T>`;
- a mimalloc heap does not implement Pool capacity, slot identity, liveness,
  or invalid-free diagnostics; and
- bulk heap destruction does not implement Stash's retained-block reset and
  deterministic reuse contract.

### 5. Libuv integration

When libuv is linked, install all four callbacks exactly once:

```c
uv_replace_allocator(mi_malloc, mi_realloc, mi_calloc, mi_free)
```

The runtime must:

- install them before every other libuv operation;
- use the pinned libuv v1 API, whose `uv_replace_allocator` returns `int`, and
  treat any non-zero result as runtime initialization failure;
- never change the callbacks while libuv owns live allocations;
- use the same pinned mimalloc binary for Hexal and libuv; and
- preserve libuv's requirement that the allocator is thread-safe.

The claim that this operation returns `void` is rejected: the current official
libuv v1 contract returns `0` on success and `UV_EINVAL` when a callback is
null. RFC 0145 must pin and qualify the consumed libuv revision before this
conditional phase executes; a later API change is handled there rather than
silently changing this failure contract.

The installation belongs to the program-wide runtime bootstrap, before Task or
event initialization. It is not repeated by individual components. A program
that uses Heap but not libuv performs no libuv initialization.

### 6. Generated-component and compiler boundary

- Runtime templates remain under `compiler/generator/packages/`.
- `hexal/heap.h` exposes no `mi_*` type and need not include `<mimalloc.h>`.
- `hexal/heap.h` declares the two nullable runtime operations because other
  component C files call them; they are not Hexal source APIs or foreign ABI.
- Selecting `hexal/concurrency.c` also selects the Heap component pair and
  makes `hexal/concurrency.c` include `hexal/heap.h`. No component may call a
  Heap runtime operation without selecting and including its owner.
- Runtime implementation files may include `<mimalloc.h>`.
- No generated module header exposes mimalloc.
- The compiler emits references and symbolic dependency metadata; it does not
  copy mimalloc source into `CompilationResult.Files`.
- Add one public, string-backed dependency identity:

  ```go
  type RuntimeDependency string

  const RuntimeMimalloc RuntimeDependency = "mimalloc"
  ```

- Extend `CompilationResult` with `Dependencies []RuntimeDependency`.
- `Dependencies` is sorted, duplicate-free, path-free, and deterministic. A
  failed compilation returns a non-nil empty slice. Callers must reject an
  unknown identity rather than ignore it.
- The compiler selects `RuntimeMimalloc` when generated-component discovery
  selects any Hexal-owned dynamic allocation or, later, libuv. The driver does
  not infer dependencies by searching generated text or artifact names.
- RFC 0055's driver maps `RuntimeMimalloc` to the checked-in header root,
  `src/static.c`, qualified compile definitions, object compilation, and final
  link input. Those filesystem paths never enter `Project` or generated C.
- The core compiler remains string-in/string-out and performs no dependency
  discovery, target probing, compilation, linking, or filesystem access.

## Demand and linkage

Mimalloc is linked when either condition holds:

- the generated program selects a Hexal component that performs dynamic
  allocation; or
- the program selects libuv.

A scalar-only program that selects neither emits no mimalloc include or link
requirement. Demand is program-wide and deterministic.

The generated `hexal/heap.c` includes `<mimalloc.h>` only when selected. The
driver supplies the vendored include root while compiling that artifact and
compiles the mimalloc object once for the build. Generated module files and
public component headers never name the third-party header.

If libuv is selected without a language-visible Heap operation, runtime
bootstrap still installs mimalloc before libuv initialization. This is a
backend dependency; it does not make a Heap value reachable in the language.

## Failure and diagnostics

- Preserve every current Hexal allocation-size and allocation-failure message.
- Mimalloc error text, assertions, and statistics are not stable language
  diagnostics.
- Release builds do not convert invalid-free behavior into a new promise.
  Hexal's static ownership rules and component-specific runtime checks remain
  authoritative.
- Debug or secure mimalloc modes may detect additional misuse, but programs
  must not depend on those detections.

## Configuration

- v1 has one reviewed release configuration for `x86_64-windows-gnu`.
- Debug and secure allocator configurations are deferred until a separate
  backend profile specifies and qualifies them; they do not change language
  semantics.
- Do not expose mimalloc environment variables, options, heap handles, or
  statistics as language builtins in this ADR.
- Do not add a source-level allocator selector.

### Thread lifecycle

Hexal creates its scheduler and blocking-pool threads directly through
`_beginthreadex` and `pthread_create`, detached and long-lived, rather than
through any wrapper mimalloc could hook. Whether mimalloc's lazy per-thread
initialization and its TLS-destructor teardown are sufficient for threads
created that way is platform-dependent, so it is qualified per profile rather
than assumed.

The qualified profile records one of two outcomes:

- lazy initialization and TLS teardown suffice, and no explicit call is
  emitted; or
- the profile requires the thread entry point to call mimalloc's thread
  initialization on entry and its thread-done operation before returning.

The second form is a target-profile build fact, not a language rule, and it never
appears in a module header. A profile whose behaviour is unqualified blocks
that target rather than defaulting to either answer. Long-lived
detached threads make this worth settling before release: an unreclaimed
per-thread heap is a slow leak that no functional test observes.

## Accepted costs

- Every dynamically allocating or libuv-backed program links another static
  dependency.
- Binary and vendored-source size increase.
- Each future supported profile needs independent build and runtime
  qualification.
- Allocator upgrades can change performance and memory use even when language
  behavior is unchanged; the pinned source digest and configuration are
  therefore part of the complete build-dependency identity and any future ADR
  0164 object-cache key. They do not alter Zig's existing backend identity.
- Explicit prefixed calls do not automatically accelerate allocations made by
  arbitrary foreign libraries.
- A single allocator cannot make unsafe ownership, stale pointers, double
  frees, or races statically safe.

## Rejected alternatives

### Keep only each platform's allocator

Smallest dependency set, but leaves server behavior and diagnostics dependent
on the host C runtime. Retain as the measurement baseline, not the selected
runtime.

### rpmalloc

Smaller and attractive for thread-local workloads, but its first-class heap API
is not thread-safe. That is a poor foundation for Task-migrating allocator
objects and adds an ownership restriction Hexal does not need.

### jemalloc

Strong server allocator with mature profiling and tuning, but a larger
configuration and portability surface than Hexal presently needs. Keep it as a
benchmark comparator if measured workloads expose a mimalloc weakness.

### TCMalloc

Introduces a C++ implementation and Bazel-centered build surface for a C23
runtime without providing a required semantic advantage.

### Conservative garbage collection

Changes lifetime, pause, reachability, and resource-release semantics. It is
incompatible with Hexal's explicit allocation and cleanup model.

### Replace Stash or Pool with mimalloc types

Rejected because the names resemble each other but the contracts do not.

## Required measurements

Use identical generated programs with native allocation and mimalloc. Record:

- allocation-heavy List, Dict, String, Stash, Pool, Task, and Channel
  workloads, plus a repeatable request-lifecycle allocation pattern that does
  not claim an HTTP stack exists;
- single-thread and scheduler-worker scaling;
- cross-thread allocate/free traffic;
- throughput and tail latency;
- peak resident memory and retained memory after a burst;
- repeated thread-churn retention, sampled in fixed batches until memory use
  either plateaus within a recorded bound or demonstrates continuing growth;
- cold and warm build time attributable to compiling `src/static.c`;
- executable and vendored-source size; and
- debug/secure-mode overhead separately from release mode.

Measurements are evidence for configuration and later optimization. They must
not weaken the semantic validation gates below.

## Detailed implementation plan

### Phase 1: qualify the candidate before compiler integration

1. Resolve the official `v3.5.1` tag to its complete commit identity and obtain
   its source archive from the official upstream release over HTTPS. This is a
   maintainer action performed once during implementation with required network
   approval; Hexal users never perform it.
2. Save the downloaded archive under `.tmp/`, compute its SHA-256 before
   extraction, and record its upstream URL, byte length, digest, tag, complete
   commit identity, and acquisition date. Never use GitHub's moving branch
   archive or an unversioned URL.
3. Extract only beneath a fresh `.tmp/` directory. Reject absolute archive
   paths, `..` traversal, every symbolic or hard-link entry, non-regular
   payloads, duplicate paths, and case-folded path collisions before writing
   an entry.
4. Verify the expected upstream license before compiling. Stage the unmodified
   candidate under `.tmp/`; do not patch or format third-party source.
5. Record the supported single-source dialect, compile definitions, include
   roots, and warnings for this revision from its upstream build files and
   documentation. Do not guess flags from another release.
6. Use the installed Zig 0.16.0 backend to compile the candidate
   `src/static.c` for `x86_64-windows-gnu` and link it into a standalone
   allocation probe.
7. Compile, link, and run allocation, zero-allocation, reallocation,
   cross-thread-free, and failure-injection probes on the qualified target.
8. Determine from the candidate revision's supported lifecycle contract and the
   cross-thread and repeated-thread-churn probes whether Hexal-created threads
   require explicit mimalloc thread entry/exit calls. Record and test the
   chosen target-profile fact.
9. Measure the native allocator and candidate with identical workloads,
   including cold and warm candidate-object compile time. Do not claim a
   performance improvement that the measurements do not show.
10. Stop and return this ADR to Open Discussion if the candidate fails a
   semantic or cross-thread probe, shows unbounded thread-churn retention, or
   cannot be built and statically linked through the qualified backend. A flat
   or slower performance result does not silently change this architectural
   decision; record it explicitly before proceeding so the accepted cost is
   visible.
11. Determine the transitive file closure required by `src/static.c`, then
    vendor that source closure, its public/internal/configuration headers, and
    license under `third_party/mimalloc/`. Preserve upstream relative paths;
    exclude examples, benchmarks, binaries, generated build products, and
    unrelated packaging files.
12. Generate `VENDORING.md` with the provenance and qualified configuration,
    plus `MANIFEST.sha256` containing every embedded dependency payload's
    logical path and file digest in bytewise path order. The manifest excludes
    itself because a file cannot contain its own digest; it includes the
    license and `VENDORING.md`. Generate both from verified inputs; do not
    hand-edit hashes.
13. Compile the pruned vendored tree independently. A missing transitive include
    or source file fails vendoring rather than being fetched or recovered from
    the earlier extraction tree.
14. Add the embedded-source Go package. Its embedded filesystem contains
    exactly `MANIFEST.sha256` plus the regular files named by that manifest,
    and no source outside `third_party/mimalloc/`.
15. Reproduce the same compile/link/run probes from embedded bytes materialized
    into a fresh staging tree. Delete all acquisition and qualification scratch
    data under `.tmp/` on success, failure, or early rejection; no downloaded
    archive or extracted tree remains after the implementation turn.

### Phase 2: add dependency demand

1. Add `RuntimeDependency`, `RuntimeMimalloc`, and the deterministic
   `CompilationResult.Dependencies` slice.
2. Initialize success and failure results with non-nil dependency slices and
   preserve the field through every result-construction path.
3. Add one program-wide mimalloc demand fact to generated-component discovery.
4. Select it for every dynamic Hexal component and, once RFC 0145 exists in
   code, for libuv.
5. Sort and deduplicate the final dependency slice before returning it.
6. Keep scalar-only and otherwise allocation-free artifacts unchanged.
7. Add focused compiler tests for positive demand, negative demand,
   deterministic ordering, failed compilation, and unknown-driver dependency
   rejection.

### Phase 3: replace the Heap backend

1. Change `compiler/generator/packages/heap.c` to use the direct `mi_*` API.
2. Preserve `hexal/heap.h`, the stateless `hex_heap` token, checked size
   arithmetic, and exact trap messages.
3. Add the vendored header include only to the owning implementation artifact;
   keep `hexal/heap.h` and module headers independent of mimalloc.
4. Verify every generated runtime allocation still routes either through the
   Heap boundary or through a deliberately direct mimalloc operation named by
   this ADR.
5. Remove direct `malloc`, `calloc`, `realloc`, and `free` calls that existed
   solely as Hexal-owned general allocation. Do not rewrite foreign-library
   ownership paths.

### Phase 4: migrate concurrency-owned allocation

1. Inventory every direct allocation and release in `hexal/concurrency.c` by
   object purpose and current failure behavior; do not use a stale numeric
   count as a move instruction.
2. Route recoverable and site-specific failures through
   the matching nullable operation, preserving each caller's null check, Error
   construction, or exact trap.
3. Route each existing `calloc(1, sizeof(...))` control block through
   `hex_heap_allocate_zeroed_or_null`; do not replace zero initialization with
   a hand-maintained field list.
4. Route every matching ordinary release through `hex_heap_free`.
5. Retain `VirtualAlloc` and `mmap` for guarded fiber stacks.
6. Assert that no direct C allocation-family call remains in the component and
   that all existing failure classifications and messages remain unchanged.

### Phase 5: teach the driver to compile and link mimalloc

1. Map only `RuntimeMimalloc` to the embedded vendored snapshot.
2. Add an ordinary pure-Go test that walks the embedded filesystem, rejects an
   unmanifested or missing payload, recomputes every payload digest, and proves
   that its sorted inventory is exactly the manifest's paths plus the manifest
   file itself.
3. Materialize the snapshot beneath the current isolated staging root and
   verify its fixed path inventory and bytes against the vendoring record.
4. Compile the staged `src/static.c` as its own target object with the
   qualified Zig command, staged include root, mimalloc definitions, and source
   dialect.
5. Add the object exactly once to the final link invocation.
6. Reject an unknown dependency identity before invoking Zig.
7. Keep paths and commands in driver state; do not put them in `Project`,
   `CompilationResult.Files`, or generated source.
8. Include the mimalloc source digest and compile configuration in the build
   record and the eventual ADR 0164 object-cache key; do not modify Zig's
   backend identity to represent a separate dependency.
9. Leave repeated compilation as the uncached v1 baseline. ADR 0164 owns any
   later cache key, locking, integrity check, and publication rule.
10. Prove the installed path by building with a standalone `hexal.exe` from a
    directory containing no source checkout and with network access disabled.
    The build may resolve only installed Zig through `PATH`; mimalloc comes
    solely from embedded bytes.

### Phase 6: connect libuv after RFC 0145

1. Add the single allocator-install operation to program bootstrap.
2. Order it before Task, event-loop, native-thread, synchronization, and every
   other libuv initialization call.
3. Pass exactly `mi_malloc`, `mi_realloc`, `mi_calloc`, and `mi_free`.
4. Trap deterministically if installation fails.
5. Prove no second installation or later allocator change exists.
6. Reconcile RFC 0145 before implementing this phase. The absence of libuv
   does not block Phases 1 through 5 or base RFC closure; when RFC 0145 lands,
   this phase becomes a required extension of the completed allocator policy.

### Phase 7: validate and synchronize

1. Run focused generator tests for includes, calls, demand, and bootstrap
   ordering.
2. Run the ordinary pure-Go suite.
3. Run external C23 compile/link/run fixtures against
   `x86_64-windows-gnu` through the installed Zig 0.16.0 backend.
4. Teach the tagged external-C harness to consume `RuntimeMimalloc`, compile
   the staged vendored source with each participating toolchain's qualified
   third-party-source flags, and link its object exactly once. This is coverage,
   not a claim that GCC or Clang is a supported Hexal backend.
5. Inspect the produced PE import table with Go's `debug/pe` and assert that no
   mimalloc DLL is imported; also run the executable without such a DLL beside
   it.
6. Compare manifest movement with the exact demand set; do not regenerate
   unrelated entries.
7. Verify the completed RFC 0052 and RFC 0055 contracts remain true; do not
   edit their archived files.
8. With explicit user approval, update `docs/reference.md` once behavior is
   stable. Record only the observable allocator contract, not mimalloc setup or
   package instructions.
9. Remove or update status entries only after all gates pass.

## Validation

This section is exhaustive. The RFC is complete only when all items pass.

- The qualified mimalloc `v3.5.1` full commit identity, its required source and
  headers, license material, and reviewed compile definitions are checked in
  under `third_party/mimalloc/`; its vendoring record names the upstream location,
  archive digest, retained files, and patches; builds neither discover nor
  download another copy.
- The vendored source is embedded in `hexal.exe`; an installed compiler finds
  no repository-relative or environment-dependent path and materializes the
  exact bytes only below its isolated build staging root.
- User builds invoke only the installed Zig backend for mimalloc; they do not
  invoke Git, CMake, a package manager, a downloader, or a mimalloc installer.
- An ordinary pure-Go test proves that the embedded payload set exactly matches
  the paths in `MANIFEST.sha256`, that every payload has its recorded digest,
  and that the manifest is the only embedded file not hashing itself.
- The installed Zig 0.16.0 backend compiles the pinned `src/static.c` for
  `x86_64-windows-gnu`, links it statically with generated objects, and
  produces no mimalloc shared-library dependency; the PE import table contains
  no mimalloc DLL.
- The qualified target passes allocation, zeroing, cross-thread-free, and
  release probes. A reallocation probe may exercise the vendored object, but
  no generated Hexal code calls `mi_realloc`.
- `CompilationResult.Dependencies` is non-nil, sorted, duplicate-free,
  deterministic, contains only compiler-owned symbolic identities, and is
  empty on compilation failure.
- Every allocating program reports `RuntimeMimalloc` exactly once; an
  allocation-free scalar program does not report it; the driver rejects an
  unknown dependency identity before invoking Zig.
- `hexal/heap.h` and every generated module header expose no `mi_*` name and do
  not include `<mimalloc.h>`.
- `hex_heap_allocate`, `hex_heap_allocate_zeroed`, and `hex_heap_free` retain
  their signatures and exact Hexal traps while calling the matching `mi_*`
  operations.
- `hex_heap_allocate_or_null` returns `nullptr` on failure, traps on nothing,
  and zeroes nothing.
- `hex_heap_allocate_zeroed_or_null` calls `mi_calloc(1, size)`, returns
  `nullptr` on failure, traps on nothing, and zeroes the complete allocation.
- `hexal/concurrency.c` contains no direct `malloc`, `calloc`, or `free` call.
  Every recoverable or site-specific failure uses the matching nullable
  operation and keeps its null check; every Task, Channel, and Mutex control
  block that currently uses `calloc` retains complete zero initialization.
- Task spawn under allocation failure still yields a recoverable `Error`, never
  a trap. This is the contract that decided which operation each site uses.
- Channel and Mutex construction retain their recoverable allocation failures;
  scheduler/root setup retains its exact traps; blocking-pool overflow growth
  retains its native-thread-creation fallback.
- Fiber stacks remain `mmap`/`VirtualAlloc` regions with their guard page and
  reach no allocator operation.
- Zeroed allocation retains its explicit `ckd_mul` overflow diagnostic before
  calling `mi_calloc`.
- Stash and Pool representation, capacity, reset, reuse, liveness, free, and
  destroy tests pass unchanged semantically. Text changes are limited to the
  selected `hexal/heap.c` allocator calls/includes and the new dependency
  metadata; their public headers and non-allocation contracts remain unchanged.
- List, Dict, String, Task, Channel, and Mutex ownership and cleanup tests pass
  unchanged semantically.
- No Hexal-owned general allocation continues to call the C allocation family
  unless a named platform or foreign-ownership boundary requires it. After
  Decision 3a the concurrency runtime is no longer such an exception.
- The qualified target records whether mimalloc thread initialization and
  teardown are explicit or left to supported lazy initialization and TLS
  teardown. Repeated fixed-size thread-churn batches demonstrate memory
  retention reaching a recorded plateau rather than continuing to grow. An
  unqualified target is not supported.
- No global malloc-symbol override, loader injection, source-level allocator
  choice, mimalloc heap exposure, or mimalloc arena exposure exists.
- Once RFC 0145 is implemented, a libuv-backed program installs all four
  mimalloc callbacks exactly once and before its first other `uv_*` call. This
  conditional gate does not block the base allocator integration while libuv
  is absent.
- A Heap-using program without libuv makes no libuv call.
- A scalar-only program selecting neither allocation nor libuv has no mimalloc
  dependency and keeps its existing generated artifacts byte-identical.
- Foreign allocation remains paired with the foreign library's release API;
  no test frees a foreign allocation through Heap merely because mimalloc is
  linked.
- Ordinary `go test ./...` requires no installed mimalloc or C toolchain.
- External C23 fixtures compile, link, and run with the object built from the
  pinned vendored source under every participating coverage toolchain.
- The qualification record includes cold and warm `src/static.c` compilation
  time, total Hexal build-time delta, binary size, and runtime measurements.
- A standalone installed `hexal.exe` builds an allocating program with no
  repository checkout and no network access; only Zig is resolved through
  `PATH`.
- Manifest changes occur only in artifacts that select Hexal dynamic
  allocation or libuv and are reviewed by artifact family.

## Qualification outputs

- Qualified `v3.5.1` full commit identity, official source URL, archive byte
  length and SHA-256, vendored-file manifest, and license record.
- Exact `x86_64-windows-gnu` single-source compile definitions.
- Whether the qualified Windows thread lifecycle needs explicit mimalloc
  entry/exit calls under the pinned revision.
- Measured native-versus-mimalloc performance and binary-size record.

The dependency-metadata representation, installed-Zig build path, static
linkage, explicit `mi_*` integration, absence of global interposition, and
preservation of Heap/Stash/Pool semantics are settled. Selecting and qualifying
the exact third-party revision is implementation work, not a language-design
question.

## Implementation readiness

Implementation-ready. Start by selecting and qualifying the pinned revision;
then execute Phases 2 through 5 in order. Phase 6 is conditional on RFC 0145
being implemented and otherwise remains an explicit consumer requirement for
that RFC. No language-surface or backend-architecture decision remains.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. During implementation, and
only with explicit user approval, verify every allocation and ownership rule.
The reference should continue to describe Hexal semantics, not name mimalloc,
unless backend identity becomes an observable compatibility contract.

