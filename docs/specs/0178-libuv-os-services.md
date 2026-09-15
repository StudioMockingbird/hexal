# RFC 0178: Program Paths and Secure Entropy

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready after RFC 0186 and RFC 0182's entry adapter; design and execution plan
  settled, implementation not started
- Created: 2026-09-13
- Updated: 2026-09-15
- Scope: expose the minimum path facts and secure system random bytes needed by
  ordinary command-line and server programs
- Depends on: RFC 0168, RFC 0186, RFC 0182's entry adapter, and the implemented RFCs 0169, 0170, 0171,
  0180, and 0181
- Coordinates with: RFC 0182 for the shared `std/program` module and the one
  entry adapter that calls `uv_setup_args` before `uv_exepath`
- Does not add: a general OS-reflection namespace, mutable process-global
  environment, process title, system metrics, deterministic pseudo-randomness,
  or a process-exit surface

## Summary

Replace the former libuv API catalog with a focused v1:

- cryptographically secure random bytes;
- current, home, temporary, and executable paths; and
- available parallelism.

RFC 0182 owns process arguments and exit status. Its entrypoint, unwind, and
Task semantics are independent of these OS queries. Libuv documents that
`uv_setup_args` must be called before `uv_exepath` on every platform; this RFC
follows that public contract rather than the pinned source's current
platform-specific independence. Executable-path demand therefore selects RFC
0182's one entry adapter, and the two RFCs land together or in the order
0182, then 0178.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Command-line arguments | Separate RFC | RFC 0182 owns the host invocation and immutable argument snapshot. |
| Secure random byte fill | Pick up | Required for identifiers, randomized hashing, protocols, and cryptographic libraries. |
| Read-only environment lookup | Skip in v1 | Keep the v1 surface small. Focused home/temp queries and Process environment inheritance already cover demonstrated needs; Hexal exposes no environment mutation, and concurrent foreign mutation remains an unsafe C-interop conflict. |
| Current directory | Pick up | Basic path resolution for user programs; distinct from the in-memory compiler. |
| Home directory | Pick up | Common configuration/data location. |
| Temporary directory | Pick up | Common safe input to temporary-file policies; this RFC does not create files. |
| Executable path | Pick up | Needed for self-relative resources and launchers. |
| Available parallelism | Pick up | Already supplied by libuv and useful for explicit work partitioning. |
| Process exit status | Separate RFC | Must settle defer, print flush, detached Task, and valid-code behavior; not an OS-query detail. |
| Environment set/unset and enumeration | Skip in v1 | Libuv documents these operations as not thread-safe; imported C can also mutate the same process state. |
| Change current directory | Skip in v1 | Process-global mutation races with every relative-path operation. |
| Process title | Skip | Requires `uv_setup_args` ownership and process-global mutation for little initial value. |
| Hostname and uname/platform strings | Skip | No current consumer; target profiles already own compile-time platform identity. |
| User/password/group lookup | Skip | Platform-shaped identity and missing-field policy add substantial surface. |
| Detailed CPU information | Skip | Available parallelism covers the demonstrated scheduling use case. |
| Memory totals and constraints | Skip | Libuv return values carry platform-specific unknown/sentinel meanings. |
| Network interfaces | Skip | Belongs with networking and must reuse its Address representation. |
| Uptime and load average | Skip | Load average is fabricated as zero on Windows; no cohesive portable contract. |
| Resource usage and process priority | Skip | Operational tooling surface with target-specific fields and mutation policy. |
| Time of day | Skip | Implemented WallTime correctly uses C23 `timespec_get` under the C23-first rule. |
| Loop metrics | Skip | Internal performance work belongs to RFC 0144. |
| Deterministic pseudo-random generator | Skip | Pure algorithmic library; it must not be confused with secure system entropy. |

## Proposed source surface

```text
import
    Prog from "std/program",
    Ent from "std/entropy"
end

Prog.current_directory(heap: Heap) -> String | Error
Prog.home_directory(heap: Heap) -> String | Error
Prog.temporary_directory(heap: Heap) -> String | Error
Prog.executable_path(heap: Heap) -> String | Error
Prog.available_parallelism() -> Size

Ent.fill(into: Slice<mut Byte>) -> Nil | Error
```

- `current_directory`, `home_directory`, `temporary_directory`,
  `executable_path`, and `available_parallelism` are exported functions of
  `std/program`; `fill` is an exported function of `std/entropy`.
- `Prog` and `Ent` above are ordinary file-local import aliases. This RFC adds
  no protected `Program` or `Entropy` name.
- Path results are owned Hexal Strings allocated from the caller's Heap.
- Available parallelism is a non-zero estimate, matching libuv's contract. It
  does not expose scheduler worker count or mutate scheduler policy.

All signatures are settled for v1. Size follows Hexal's count convention rather
than exposing libuv's `unsigned int` return type.

RFCs 0178 and 0182 extend the same `std/program` core-library declaration table
and generated program component. RFC 0186 establishes that module boundary
first; neither RFC creates a namespace type or duplicate runtime component.

## Secure random contract

- `std/entropy.fill` fills the entire destination or returns Error; short success is
  impossible.
- An empty Slice succeeds immediately, touches no memory, and submits no libuv
  request.
- The caller must not read, write, grow, or free the destination storage from
  another Task until the call completes. This is the ordinary unsynchronized
  alias/data-race rule, not a new compiler-enforced borrow guarantee.
- Inside a Task, use asynchronous `uv_random` and park only that Task. Libuv
  explicitly permits entropy acquisition to wait indefinitely; Hexal must not
  block a scheduler worker on that possibility.
- Outside a Task, use the documented synchronous `uv_random` form.
- On failure, destination contents are unspecified. Callers must not treat them
  as random data.
- The pinned libuv implementation rejects a request larger than `0x7fffffff`
  bytes with `UV_E2BIG` (`src/random.c`), although its public `size_t` signature
  and documentation do not state that limit. The chunk size is therefore a Hexal
  runtime rule owned by this RFC, not a source-visible limit: a larger Slice is
  filled by sequential requests of at most `0x7fffffff` bytes, whatever a future
  libuv revision accepts. Success means every chunk
  completed; after any chunk fails, the entire destination remains unspecified.
- Failure uses the common libuv ErrorKind mapper with fixed message
  `secure random fill failed`; no native code or backend text is retained.
- Entropy-module use selects libuv and the native bootstrap. When the program
  selects the scheduler, every call uses the asynchronous event-bridge form,
  because the root module itself runs as a Task; a program without the
  scheduler uses only the synchronous form and selects no event bridge. The
  choice is compile-time and program-wide, matching existing IO, not a per-call-
  site classification.
- There is no prior entropy backend to remove.

## Path-query contract

- Use `uv_cwd`, `uv_os_homedir`, `uv_os_tmpdir`, and `uv_exepath` respectively.
- Home-directory and temporary-directory queries share one private runtime
  mutex because libuv documents them as not thread-safe. Concurrent foreign
  mutation of the process environment while either query executes is an unsafe
  C-interop conflict; Hexal exposes no environment mutation in v1.
- Initialize that mutex exactly once with `uv_once` before either query and
  destroy no process-lifetime synchronization object during ordinary return.
- For current, home, and temporary directory, retry the documented
  `UV_ENOBUFS` sizing protocol with checked size arithmetic. After initial
  sizing, permit at most two growth retries if the process-global value changes
  between sizing and retrieval. Continued `UV_ENOBUFS` returns
  `ErrorKind.Busy()` with fixed message `path changed during query`.
- `uv_exepath` does not provide that protocol: pinned backends can return
  success after filling only `capacity - 1` bytes. Treat that exact length as
  possible truncation, grow geometrically, and retry. Start at 4 KiB. Windows
  permits growth through 128 KiB, which exceeds the pinned backend's maximum
  32,768 UTF-16-code-unit path after UTF-8/WTF-8 expansion; other targets permit
  growth through 1 MiB. Saturation at the bound returns
  `ErrorKind.ResourceExhausted()` with fixed message
  `executable path unavailable`. Every growth calculation is checked.
- Copy the successful UTF-8 bytes into the caller's Heap before releasing
  temporary runtime storage.
- Validate every successful path on every target as UTF-8. POSIX can return
  arbitrary path bytes, and pinned Windows libuv converts ill-formed UTF-16 to
  WTF-8; either can therefore fail validation. Invalid bytes return
  `ErrorKind.InvalidPath()` with fixed message `path is not valid UTF-8`; no
  lossy replacement or byte-path surface is introduced.
- Perform no normalization, canonicalization, symlink resolution, separator
  rewriting, or case folding.
- Home and temporary directory are observations, not security guarantees and
  not proof that a path exists or is writable.
- Outside a Task, these libuv helpers execute synchronously. Inside a Task,
  every path query uses the existing `uv_queue_work` bridge and parks only that
  Task; in particular, home-directory lookup may consult the host account
  database and must not block a scheduler worker. The uniform bridge imposes a
  worker round trip on cheap queries; v1 accepts that cost to keep one Task
  execution rule.
- Reachable `std/program.executable_path` selects RFC 0182's entry adapter,
  which runs the native bootstrap and then `uv_setup_args` exactly once before
  any module statement or scheduler startup. On Windows the adapter keeps
  `int main(void)` and passes the MinGW CRT's `__argc`/`__argv`; on POSIX it
  widens to `int main(int argc, char **argv)`. This RFC adds no second entry
  path and no second `uv_setup_args` call.
- Native/query failures use the common ErrorKind mapper with fixed messages
  `current directory unavailable`, `home directory unavailable`,
  `temporary directory unavailable`, and `executable path unavailable`.
  Allocation failure is `ErrorKind.ResourceExhausted()` with the same
  operation-specific message. Empty successful paths are invalid and use
  `ErrorKind.InvalidPath()`.

## Module decision

- `std/program` owns paths and available parallelism here; RFC 0182 adds
  arguments to that module.
- `std/entropy` owns secure system entropy. `Random` remains available to a
  future deterministic pseudo-random library and is not a protected name here.
- There is no general `Os` namespace. A later focused capability adds its own
  cohesive type rather than extending an unbounded miscellaneous catalog.

## Process entry and exit boundary

RFC 0182 owns host arguments and process status. This RFC neither changes the
entry ABI independently nor adds an exit operation.

## Detailed implementation plan

### Phase 1: metadata and checked surface

1. Extend RFC 0186's `std/program` and `std/entropy` core-library tables with
   the six exported module functions above; add no protected identity.
2. Register the five program operations and entropy `fill` in the checker,
   including exact arguments, results, import-alias resolution, and fail-closed
   unknown-operation diagnostics.
3. Extend checked-call identities rather than recognizing source spellings in
   the generator.

### Phase 2: component discovery

1. Add focused generator discovery for program paths, available parallelism,
   and entropy independently.
2. Select libuv and native bootstrap for any operation here. When the program
   also selects the scheduler, select the existing event bridge and route every
   path and entropy call through it; otherwise emit only the synchronous forms.
3. Emit no program-query or entropy artifact when neither family is reachable.

### Phase 3: Program runtime

1. Add `program.h` and `program.c` under RFC 0186's
   `compiler/corelib/runtime/`; keep libuv and platform types private to the C
   file.
2. Implement current/home/temporary queries with their documented sizing and
   two-retry rule; implement executable path with success-saturation detection,
   bounded growth, and checked arithmetic. Apply all-target UTF-8 validation,
   caller-Heap copying, exact ErrorKind/message mapping, and the existing
   worker bridge inside Tasks.
3. Implement `std/program.available_parallelism()` as a checked conversion from
   libuv's guaranteed non-zero value to Size, with no scheduler-policy side
   effect.
4. Mark executable-path demand as requiring RFC 0182's entry adapter; assert
   one native bootstrap and one `uv_setup_args` call before the first
   `uv_exepath`, Windows `main(void)` with `__argc`/`__argv`, and POSIX
   `main(int argc, char **argv)`.

### Phase 4: Entropy runtime

1. Add `entropy.h` and `entropy.c` under RFC 0186's
   `compiler/corelib/runtime/`.
2. Implement empty success, synchronous non-Task fill, asynchronous Task fill,
   sequential request chunking, exact failure mapping, and result-before-wake
   publication.
3. Retain no pointer to the destination after the operation returns and perform
   no hidden destination allocation.

### Phase 5: conformance and documentation

1. Add focused checker tests and public integration tests for every Validation
   item below.
2. Add tagged C23 fixtures for direct and Task paths, exact errors, path
   encoding, entropy chunk boundaries, demand selection, and source mapping.
3. Measure generated size, link time, runtime allocations, and Task/non-Task
   latency independently for the program and entropy components.
4. Run ordinary and tagged gates on every qualified target; regenerate only
   manifest artifacts intentionally changed by this surface.
5. Update `docs/reference.md` only after behavior stabilizes and only with
   explicit user approval.

## Validation

This list is exhaustive:

- secure entropy empty and non-empty fills, full success, failure, Task and non-
  Task paths, the `0x7fffffff` chunk boundary, buffer synchronization, and
  unspecified failure contents;
- sizing races and exact ownership for current/home/temporary queries;
- executable-path success saturation, multiple growth steps, Windows 128-KiB
  and other-target 1-MiB bounds, and exact bounded failure;
- invalid POSIX UTF-8, invalid Windows WTF-8, empty path, exact failure
  kind/message, direct non-Task query, and Task-parking query;
- exactly-once home/temp mutex initialization and concurrent query
  serialization;
- non-zero available parallelism without changing scheduler configuration;
- Size is the exact available-parallelism result and Random remains an
  unprotected user-available name;
- exact demand selection and absence of unrelated OS-query APIs;
- no environment access or mutation, cwd mutation, process title, metrics, user/group,
  interface, time-of-day, or deterministic PRNG surface;
- executable-path demand runs exactly one native bootstrap and one
  `uv_setup_args` before the first query, keeps Windows `main(void)` through
  `__argc`/`__argv`, and shares the adapter with `arguments()` when both are
  reachable; a program reaching neither emits no adapter; and
- ordinary and tagged C23 gates with manifest movement confined to the exact
  programs selecting these capabilities.

POSIX items (invalid POSIX UTF-8 and POSIX entry behavior) are verified as
generated-text assertions over host-neutral output until a POSIX target profile
is qualified. No POSIX runtime branch is claimed as executed; a future POSIX
profile runs them through its own external gate.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation adds
only the settled `std/program` path/parallelism and `std/entropy` contracts
after behavior stabilizes. RFC 0182 owns argument and process-status
synchronization.
