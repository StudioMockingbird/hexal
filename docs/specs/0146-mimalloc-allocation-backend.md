# RFC 0146: mimalloc Allocation Backend

- Kind: Architecture Decision Record (ADR)
- Status: Draft; architecture proposed, dependency qualification not started
- Created: 2026-09-08
- Scope: use a pinned mimalloc v3 build as the backing allocator for Hexal-owned
  dynamic storage and libuv's internal allocations
- Depends on: RFC 0052 (C compiler backend) and RFC 0055 (filesystem/build
  driver)
- Coordinates with: RFC 0110 (ownership), RFC 0145 (libuv runtime), and the
  current allocation contracts in `docs/reference.md`
- Does not change: Hexal syntax, `Heap`, `Stash<T>`, `Pool<T>`, collection,
  String, ownership, lifetime, failure, or thread-safety contracts

## Summary

Use one pinned, statically linked mimalloc v3 build as the implementation of
Hexal-owned general allocation. Keep every language-level allocator contract in
Hexal:

- `Heap` remains a stateless token selecting one thread-safe default allocator.
- `Stash<T>` remains a typed growable bump allocator with reset and destroy.
- `Pool<T>` remains typed fixed-capacity reusable-slot storage.
- Lists, Dicts, Strings, Channels, Mutexes, Tasks, Stashes, and Pools retain
  their current ownership and cleanup rules.

Mimalloc supplies raw storage. It does not become a Hexal type and its heaps or
arenas are not exposed as language allocators.

When RFC 0145 selects libuv, install the same mimalloc functions through
`uv_replace_allocator` before the first other libuv call. This unifies
Hexal-owned and libuv-internal general allocation without pretending that
foreign libraries share Hexal ownership.

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

This RFC does not claim that allocator replacement is automatically faster.
Qualification includes application-shaped measurements against each target's
native allocator. The design adopts mimalloc for one controlled runtime and
consistent target behavior; benchmark results determine configuration, not
whether Hexal exposes another allocator choice.

## Decision

### 1. Dependency and version

- Pin one exact mimalloc v3 source revision in each target-pack release.
- Build it separately under its supported source dialect and flags.
- Link it statically. A normal Hexal program has no runtime dependency on a
  mimalloc shared library.
- Qualify the exact revision independently on all six initial profiles:
  `x86_64-linux`, `aarch64-linux`, `x86_64-windows`, `aarch64-windows`,
  `aarch64-macos`, and `x86_64-macos`.
- Ship its required license and attribution material.
- Do not silently use a system-installed mimalloc. An external override is a
  future driver policy and must prove version and ABI compatibility.

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

Keep the generated API stable:

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

Add one further operation, because the concurrency runtime needs a raw
allocation whose failure is recoverable:

```c
void *hex_heap_allocate_or_null(size_t size);
```

It calls `mi_malloc` and returns `nullptr` on failure instead of trapping. This
is a Hexal semantic adaptation -- recoverable failure rather than a trap -- not
a bare wrapper, so it does not contradict the rule above. It performs no
zeroing; a caller needing zeroed storage keeps its existing explicit
initialization.

### 3a. The concurrency runtime

`hexal/concurrency.c` currently makes 42 raw `malloc`, `calloc`, and `free`
calls that never reach the Heap boundary: Task control blocks, Task argument
and result frames, fiber contexts, thread start records, and the POSIX signal
alternate stack.

Those allocations move to the mimalloc boundary too. Leaving them native would
split one program across two allocator families, which is the condition
Decision 2 exists to avoid, and would exclude the allocations that motivate
this RFC most directly: a Task control block is allocated on one scheduler
worker and freed on another, which is exactly the cross-thread pattern the
Motivation cites.

They cannot use `hex_heap_allocate`, and the reason is a contract rather than a
preference. `hex_task_spawn` returns `nullptr` when any of its three
allocations fails, and the spawn site turns that into a recoverable `Error`.
`hex_heap_allocate` traps. Routing spawn through it would convert a recoverable
`Error` into a trap and change a language-visible failure contract this RFC
promises not to touch.

The rule is therefore:

- allocations whose failure is a recoverable `Error` -- spawned Task control
  blocks, argument and result frames, fiber contexts, and thread start records
  -- use `hex_heap_allocate_or_null` and keep their existing null checks;
- allocations whose failure already traps -- the root Task control block and
  the signal alternate stack -- use `hex_heap_allocate` and drop their local
  null check in favour of its trap, preserving the existing message;
- every matching release uses `hex_heap_free`.

No allocation in `hexal/concurrency.c` calls the C allocation family directly
after this change. Fiber *stacks* are unaffected: they are `mmap`/`VirtualAlloc`
regions with a guard page, not heap allocations, and remain platform calls.

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
- treat a non-zero installation result as runtime initialization failure;
- never change the callbacks while libuv owns live allocations;
- use the same pinned mimalloc binary for Hexal and libuv; and
- preserve libuv's requirement that the allocator is thread-safe.

The installation belongs to the program-wide runtime bootstrap, before Task or
event initialization. It is not repeated by individual components. A program
that uses Heap but not libuv performs no libuv initialization.

### 6. Generated-component and compiler boundary

- Runtime templates remain under `compiler/generator/packages/`.
- `hexal/heap.h` exposes no `mi_*` type and need not include `<mimalloc.h>`.
- Runtime implementation files may include `<mimalloc.h>`.
- No generated module header exposes mimalloc.
- The compiler emits references and dependency metadata; it does not copy
  mimalloc source into `CompilationResult.Files`.
- RFC 0055 supplies the selected target pack's header root, archive, and link
  inputs.
- The core compiler remains string-in/string-out and performs no dependency
  discovery, target probing, compilation, linking, or filesystem access.

## Demand and linkage

Mimalloc is linked when either condition holds:

- the generated program selects a Hexal component that performs dynamic
  allocation; or
- the program selects libuv.

A scalar-only program that selects neither emits no mimalloc include or link
requirement. Demand is program-wide and deterministic.

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

- Release target packs use one reviewed configuration shared by all ordinary
  Hexal programs for that profile.
- Debug and secure allocator builds may be separate backend profiles; they do
  not change language semantics.
- Do not expose mimalloc environment variables, options, heap handles, or
  statistics as language builtins in this RFC.
- Do not add a source-level allocator selector.

### Thread lifecycle

Hexal creates its scheduler and blocking-pool threads directly through
`_beginthreadex` and `pthread_create`, detached and long-lived, rather than
through any wrapper mimalloc could hook. Whether mimalloc's lazy per-thread
initialization and its TLS-destructor teardown are sufficient for threads
created that way is platform-dependent, so it is qualified per profile rather
than assumed.

Each profile's qualification records one of two outcomes:

- lazy initialization and TLS teardown suffice, and no explicit call is
  emitted; or
- the profile requires the thread entry point to call mimalloc's thread
  initialization on entry and its thread-done operation before returning.

The second form is a per-profile build fact, not a language rule, and it never
appears in a module header. A profile whose behaviour is unqualified blocks
that profile's target pack rather than defaulting to either answer. Long-lived
detached threads make this worth settling before release: an unreclaimed
per-thread heap is a slow leak that no functional test observes.

## Accepted costs

- Every dynamically allocating or libuv-backed program links another static
  dependency.
- Binary and target-pack size increase.
- Each supported profile needs independent build and runtime qualification.
- Allocator upgrades can change performance and memory use even when language
  behavior is unchanged; the pinned revision is therefore part of target-pack
  identity.
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

- allocation-heavy List, Dict, String, Stash, Pool, Task, Channel, and HTTP-like
  request workloads;
- single-thread and scheduler-worker scaling;
- cross-thread allocate/free traffic;
- throughput and tail latency;
- peak resident memory and retained memory after a burst;
- executable and target-pack size; and
- debug/secure-mode overhead separately from release mode.

Measurements are evidence for configuration and later optimization. They must
not weaken the semantic validation gates below.

## Detailed implementation plan

### Phase 1: qualify and package

1. Select an exact mimalloc v3 release or commit and record its license.
2. Build a static archive for each RFC 0052 target profile using the profile's
   compiler, runtime, object format, and linker.
3. Record headers, archive digest, compile definitions, consumer flags, target
   identity, and mimalloc version in target-pack metadata.
4. Compile, link, and run allocation, zero-allocation, reallocation,
   cross-thread-free, and failure-injection probes on every runnable target.
5. Verify the Windows AArch64 and GNU-runtime combination directly rather than
   inferring it from another Windows build.
6. Capture the native-allocator baseline and the required measurements.

### Phase 2: add dependency demand

1. Add one program-wide mimalloc dependency fact to generated-component
   discovery.
2. Select it for every dynamic Hexal component and for libuv.
3. Keep scalar-only and otherwise allocation-free artifacts unchanged.
4. Add target-pack dependency metadata without adding host paths to `Project`
   or generated C.

### Phase 3: replace the Heap backend

1. Change `compiler/generator/packages/heap.c` to use the direct `mi_*` API.
2. Preserve `hexal/heap.h`, the stateless `hex_heap` token, checked size
   arithmetic, and exact trap messages.
3. Verify every generated runtime allocation still routes either through the
   Heap boundary or through a deliberately direct mimalloc operation named by
   this RFC.
4. Remove direct `malloc`, `calloc`, `realloc`, and `free` calls that existed
   solely as Hexal-owned general allocation. Do not rewrite foreign-library
   ownership paths.

### Phase 4: connect libuv

1. Add the single allocator-install operation to program bootstrap.
2. Order it before Task, event-loop, native-thread, synchronization, and every
   other libuv initialization call.
3. Pass exactly `mi_malloc`, `mi_realloc`, `mi_calloc`, and `mi_free`.
4. Trap deterministically if installation fails.
5. Prove no second installation or later allocator change exists.
6. Update RFC 0145's implementation and validation text to consume this
   allocator policy rather than rejecting the hook.

### Phase 5: validate and synchronize

1. Run focused generator tests for includes, calls, demand, and bootstrap
   ordering.
2. Run the ordinary pure-Go suite.
3. Run external C23 compile/link/run fixtures against every runnable target
   pack.
4. Compare manifest movement with the exact demand set; do not regenerate
   unrelated entries.
5. Update RFCs 0052 and 0055 with target-pack and driver ownership.
6. With explicit user approval, update `docs/reference.md` once behavior is
   stable. Record only the observable allocator contract, not mimalloc setup or
   package instructions.
7. Remove or update status entries only after all gates pass.

## Validation

This section is exhaustive. The RFC is complete only when all items pass.

- All six initial target packs contain one pinned, statically linkable
  mimalloc v3 archive, matching headers, license material, build flags, digest,
  target identity, and ABI evidence.
- Every runnable target passes allocation, zeroing, cross-thread-free, and
  release probes. A reallocation probe may exercise the vendored archive, but
  no generated Hexal code calls `mi_realloc`.
- `hexal/heap.h` and every generated module header expose no `mi_*` name and do
  not include `<mimalloc.h>`.
- `hex_heap_allocate`, `hex_heap_allocate_zeroed`, and `hex_heap_free` retain
  their signatures and exact Hexal traps while calling the matching `mi_*`
  operations.
- `hex_heap_allocate_or_null` returns `nullptr` on failure, traps on nothing,
  and zeroes nothing.
- `hexal/concurrency.c` contains no direct `malloc`, `calloc`, or `free` call.
  Every recoverable-failure allocation uses `hex_heap_allocate_or_null` and
  keeps its null check; the root Task and signal alternate stack use
  `hex_heap_allocate` and retain their exact existing trap messages.
- Task spawn under allocation failure still yields a recoverable `Error`, never
  a trap. This is the contract that decided which operation each site uses.
- Fiber stacks remain `mmap`/`VirtualAlloc` regions with their guard page and
  reach no allocator operation.
- Zeroed allocation retains its explicit `ckd_mul` overflow diagnostic before
  calling `mi_calloc`.
- Stash and Pool representation, capacity, reset, reuse, liveness, free, and
  destroy tests pass unchanged except for legitimate generated backend text.
- List, Dict, String, Task, Channel, and Mutex ownership and cleanup tests pass
  unchanged semantically.
- No Hexal-owned general allocation continues to call the C allocation family
  unless a named platform or foreign-ownership boundary requires it. After
  Decision 3a the concurrency runtime is no longer such an exception.
- Every target pack records, per profile, whether mimalloc thread
  initialization and teardown are explicit or left to lazy initialization and
  TLS destructors. An unqualified profile ships no target pack.
- No global malloc-symbol override, loader injection, source-level allocator
  choice, mimalloc heap exposure, or mimalloc arena exposure exists.
- A libuv-backed program installs all four mimalloc callbacks exactly once and
  before its first other `uv_*` call.
- A Heap-using program without libuv makes no libuv call.
- A scalar-only program selecting neither allocation nor libuv has no mimalloc
  dependency and keeps its existing generated artifacts byte-identical.
- Foreign allocation remains paired with the foreign library's release API;
  no test frees a foreign allocation through Heap merely because mimalloc is
  linked.
- Ordinary `go test ./...` requires no installed mimalloc or C toolchain.
- External C23 fixtures compile, link, and run with the pinned target-pack
  archive.
- Manifest changes occur only in artifacts that select Hexal dynamic
  allocation or libuv and are reviewed by artifact family.

## Open implementation inputs

- Exact mimalloc v3 revision.
- Per-target static-build definitions and archive form.
- Release, debug, and secure configuration profiles.
- Driver dependency-metadata representation.
- Measured native-versus-mimalloc performance and binary-size record.

These inputs do not reopen the language API, explicit `mi_*` integration,
static default linkage, absence of global interposition, or preservation of
Heap/Stash/Pool semantics.

## Implementation readiness

The architecture is ready for dependency qualification. Code implementation is
blocked on selecting and qualifying the pinned revision in RFCs 0052 and 0055.
No language-surface decision remains.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. During implementation, and
only with explicit user approval, verify every allocation and ownership rule.
The reference should continue to describe Hexal semantics, not name mimalloc,
unless backend identity becomes an observable compatibility contract.

