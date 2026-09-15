# Hexal Status

Open work only: TODOs and open bugs. Nothing here records completed work —
a spec's `Status:` header is the record that something is done, and
`reference.md` is the record of what the language means.

Every entry names its owning spec. An item without a spec either gets one or
gets deleted.

## Open TODOs

### Implementation-ready

| Work | Spec |
| --- | --- |
| Repair qualified generic types and defining-module specialization of exported generics | [0190](specs/0190-cross-module-generic-correctness.md) |

### Coordination umbrella; not independently executable

| Work | Disposition | Spec |
| --- | --- | --- |
| libuv capability ownership and child-RFC coordination | Umbrella only; child surfaces remain independently gated | [0168](specs/0168-libuv-backed-runtime-and-io-semantics.md) |

### Implementation-ready completion tracks

| Work | Spec |
| --- | --- |
| Complete runtime execution tests for every Task park/commit/wake race and destruction owner | [0183](specs/0183-compiler-runtime-completion.md) |
| Expand tagged generated-C coverage to every reachable stable runtime-trap family | [0183](specs/0183-compiler-runtime-completion.md) |
| Add executable UBSan coverage across the runnable generated-C fixture catalog | [0183](specs/0183-compiler-runtime-completion.md) |
| Replace wholesale helper-family emission with deterministic demand-driven emission | [0183](specs/0183-compiler-runtime-completion.md) |
| Compile, link, and run representative generated programs on every target before claiming that target as supported | [0183](specs/0183-compiler-runtime-completion.md) |

### Design settled; implementation blocked

| Work | Blocked by | Spec |
| --- | --- | --- |
| Add typed C binding modules for functions, records, opaque types, constants, globals, pointers, and explicit text/buffer bridges | Open identity and handwritten-ABI decisions | [0039](specs/0039-c-interop-compiler-core.md) |
| Compile and link command-line-supplied C sources, objects, archives, and system libraries | RFC 0039 plus open environment, argument-fence, and build-identity decisions | [0192](specs/0192-command-line-c-build-inputs.md) |
| Automatically generate typed binding modules for reachable C-header imports | RFC 0039, RFC 0192, and open frontend/header-ownership/name/cost decisions | [0193](specs/0193-automatic-c-header-bindings.md) |
| Prove end-to-end automatic import and static linking of an unmodified Raylib package | RFC 0039, RFC 0192, and RFC 0193 | [0209](specs/deferred/0209-raylib-external-package-conformance-plan.md) |
| Establish the standard-library module boundary and migrate compiler-owned capability namespaces | RFC 0190 | [0186](specs/0186-standard-library-boundary.md) |
| Program entry and exit: immutable process arguments, UInt8 root return status, cleanup ordering, and target entry ABI | RFC 0186 | [0182](specs/0182-program-entry-and-exit.md) |
| Program path queries, Size-valued available parallelism, and secure entropy fill | RFC 0186 | [0178](specs/0178-libuv-os-services.md) |

## Deferred ideas

Open ideas under discussion live in `docs/specs/deferred/`, with a README
explaining the directory. They are deliberately not listed here: this board
is scheduled work, and enumerating unscheduled ideas alongside it is what made
them read as commitments. Nothing there is authoritative, and a deferred spec
that disagrees with `docs/reference.md` is wrong.

## Open bugs

A bug is real whether or not its owning spec is scheduled.

| Bug | Owning spec |
| --- | --- |
| Exported generic functions are specialized in the importing module's environment, breaking defining-module generic type resolution and misattributing declaration diagnostics | [0190](specs/0190-cross-module-generic-correctness.md) |
| `reference.md` names POSIX x86-64 as a supported Task target although RFC 0052 has qualified only Windows x64 | [0168](specs/0168-libuv-backed-runtime-and-io-semantics.md); reference correction requires explicit user approval |
| `reference.md`'s Pointers and nullability section states "Arithmetic, indexing, ... are unavailable", contradicting closed RFC 0156's unsafe-gated `Ptr.offset`/indexing/`.cast<U>()` | [0156](specs/archive/0156-fenced-pointer-arithmetic.md); reference correction requires explicit user approval per that RFC's own text |

## Known coverage gaps

Not bugs — deliberate limits worth remembering when reading a green test run.

- **Networking (closed RFC 0172) serializes contending TCP callers by
  rejection, not by a FIFO wait queue.** A second concurrent read, write, or
  accept on one connection or listener while the first is still in flight
  returns Error with kind `Busy` immediately, instead of joining the queue
  RFC 0172's text specifies. Building genuine FIFO Task-parking
  synchronization for this was judged disproportionate to a first
  implementation; the common case (one caller per connection or listener at
  a time) is unaffected and is exercised end to end -- including a real TCP
  loopback connect/listen/accept/read/write/close exchange -- by the tagged
  C23 suite.
- **Processes and IPC (closed RFC 0173) makes the identical Busy-not-FIFO
  trade Networking makes, for the identical reason.** A second concurrent
  read or write on one Pipe while the first is still in flight returns Error
  with kind `Busy` immediately rather than joining a queue; the common case
  (one caller per Pipe at a time) is exercised end to end -- a real
  `uv_spawn` child, exit-status wait, and piped-stdout read/write/close --
  by the tagged C23 suite (`process-spawn-wait-runs`, `process-pipe-echo-runs`).
- **Signals (closed RFC 0176) routes any collection specialized over `Signal` (List, Array, Slice,
  Dict, Pool) to the consuming module's own header instead of the shared collection component, and
  excludes such a collection from eager equality-helper generation.** `hexal/signal.h` needs
  `hexal/error.h`, which needs `hexal/string.h`, which needs `hex_slice_UInt8` -- but
  `hexal/slice.h` is positioned ahead of all three specifically so they can rely on it already
  being complete, so no shared component can include `hexal/signal.h` without inverting that
  dependency direction. Routing the specialization to module-owned rendering (where
  `hexal/signal.h` is already `#include`d) sidesteps the conflict entirely, at the cost of
  `List<Signal> == List<Signal>` and its Array/Slice/Dict/Pool equivalents not being generated;
  bare `Signal == Signal` is unaffected. File, TcpConnection, and Process have the identical
  layering conflict for their own element collections and are not fixed here. Exercised end to end
  -- a real subscription racing a concurrent `close()` against a parked `next()` -- by the tagged
  C23 suite (`signals-subscribe-compiles`, `signals-close-wakes-waiter-runs`).
- **Runtime traps are verified for a curated dozen, not the full derived
  inventory.** `compiler/tests/c23validation` now runs a real, tagged
  (`go test -tags c23`) suite: `TestC23Suite` compiles every fixture under
  GCC, Clang, and `zig cc`, runs the ones with a zero-exit expectation and
  asserts their exact stdout, and runs the ones with a non-zero expectation
  and asserts their exact `[Runtime Error]` stderr text. It currently covers
  division/remainder-by-zero, conversion overflow, empty-list pop, missing
  Dict key, list/array index-out-of-bounds, array/list/string slice bounds,
  malformed UTF-8, and RuneCursor exhaustion, plus exact-output coverage for
  List/Dict/String round-trips, `print`'s output forms and evaluation order,
  `try`/`errdefer`/`defer` unwind ordering, float-to-integer truncation,
  signed-MIN overflow wrapping, text/Strand/RuneCursor conformance, and
   `Atomic<T>`'s full operation set, plus exact-output coverage for root yield
   (single and repeated), spawn/join, Channel send/receive/close, Mutex
   contention, and root completion with no child, joined children, and a
   detached CPU child. `TestC23SnippetCatalogCompiles` separately
   Tier-1-compiles every workbench snippet under all three toolchains with no
   hand-listed fixture per snippet. What remains unverified:
  the rest of `reference.md`'s trap inventory (shift count, close failure,
  Mutex misuse, task stack overflow, and others) has no fixture yet, and
  `print`'s output forms are not exhaustive over every printable type. The
  UBSan execution over every runnable host fixture does not exist yet and is
  owned by RFC 0183. ASan remains separately deferred under RFC 0185 because
  the installed Windows Zig backend cannot link its runtime and Task fibers
  lack sanitizer switch annotations.
- **Compiling real generated C surfaced multiple generator defects across the
  snippet catalog.** Every failure reproduced so far is now fixed and
  re-verified compiling clean under all three toolchains, most recently (RFC
  0131, closed 2026-08-27) handle-valued Array/View element storage,
  handle-aware nested equality and for-in binders, generic specialization
  prototype ordering, flow-narrowed union returns and `try` operands, and the
  ADT equality `abort()` missing `<stdlib.h>`; see the archived spec for the
  full root-cause account. A full untargeted sweep of the complete 140-snippet
  catalog now runs to completion and passes: `go test -count=1 -timeout=10m
  -parallel=8 -tags c23 -run TestC23SnippetCatalogCompiles
  ./compiler/tests/c23validation` finishes in 151s with 140/140 snippets
  passing under all three toolchains — before RFC 0140 (closed 2026-08-27)
  fixed a redundant-toolchain-discovery-per-snippet cost and parallelized the
  loop (plus two related correctness bugs the fix required: a compile-cache
  data race and a cache-entry-outliving-its-`t.TempDir()` bug, both latent
  and not triggered by today's catalog, per the archived spec), this same
  sweep ran past 50 minutes without asserting a single failure and never
  completed. Earlier defects fixed and verified
  compiling and running clean under all three toolchains include `hexal/list.h` and
  `hexal/array.h` omitting `hexal/string.h` for a String element (and
  `hexal/view.h`'s equivalent forward-declaration, needed instead of a full
  include because `hexal/string.h` itself unconditionally needs
  `hex_view_UInt8`); `print`'s selection of `hexal/io.c` not carrying the
  same error/list/view/heap dependency propagation a direct stream operation
  gets; a Go-template `{{0}}` rendering as literal `0` instead of the
  intended C `{0}` struct initializer in `hexal/io.c`; `Mutex` declared by
  value instead of by pointer wherever it is not a bare local (object
  members, union payloads); `Atomic<T>.new`'s constructor casting its
  argument to the `_Atomic` type inside a plain-typed return (tolerated by
  gcc, rejected by Clang and zig cc); a redundant `const` doubling when
  `hex_dict_find` returns a value type that is itself already a pointer
  (`Dict<K, String>`); and every `if`/`while` condition that is itself a bare
  comparison or logical expression being wrapped in one redundant extra pair
  of parentheses, which is flagged, not merely stylistic, under
  `-Werror=parentheses-equality`. The corpus-wide sweep that found all of
  this was `go test -tags c23 -run TestC23SnippetCatalogCompiles`, run once
  over the full 140-snippet catalog; it had never been run to completion
  under a real toolchain before.
- The generator emits helper families wholesale — equality, print, union,
  heap, io — so a small program's C contains many unused `static` helpers.
  Demand-driven helper emission would remove the dead code. The external C23
  suite's four `unused-*` warning suppressions are labelled Debt against this
  entry; narrowing them is this item's job, not the suite's.
- **The redesigned Task park/commit/wake protocol is unverified at runtime**,
  for the same reason as every other generated-C claim: no test executes
  generated C. What is verified textually (exact generated-C structure,
  asserted in `compiler/generator/concurrency_component_test.go`): `hex_task`
  carries exactly one atomic park phase, one nullable pending link, and one
  lifecycle mutex with no superseded `state`/`wake_error` field; the three
  transition helpers (`hex_task_wake`, `hex_task_commit_park`,
  `hex_task_resume_commit`) are defined exactly once each; every wait-family
  registration writes its pending link before its release phase store; and a
  Mutex waiter's generated code returns directly on a transferred
  `wake_result` instead of re-entering acquisition. Unverified at runtime:
  immediate yield, join completion, Channel wake, and Mutex wake never run
  one fiber on two workers and never lose a wake; completion before and
  after dispatcher park commit each publish exactly once; Channel close
  wakes every registered waiter exactly once, including waiters whose fiber
  switches are not yet committed; a resumed Channel operation can recheck
  and re-park without stale-waker ABA; a contended Mutex transfers ownership
  without trapping its selected waiter; a join cannot reclaim the target
  fiber before its completion switch returns to the dispatcher; and join
  during `completing`, join after `done`, detach completion, and root
  shutdown each use their defined destruction owner.
- To verify; import block must always be at the top of the mocule. export block must always be at the end. import, export and unsafe can only be at root level.
