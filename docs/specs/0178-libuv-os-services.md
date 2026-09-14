# RFC 0178: Program Environment and Secure Random

- Kind: Feature Specification (Rust-Style RFC)
- Status: Design settled; detailed implementation specification not started
- Created: 2026-09-13
- Updated: 2026-09-14
- Scope: expose the minimum program-environment facts and secure system random
  bytes needed by ordinary command-line and server programs
- Depends on: RFC 0168 and the implemented RFCs 0169, 0170, 0171, 0180,
  and 0181
- Does not add: a general OS-reflection namespace, mutable process-global
  environment, process title, system metrics, deterministic pseudo-randomness,
  or a process-exit surface

## Summary

Replace the former libuv API catalog with a focused v1:

- process command-line arguments as immutable process-lifetime data;
- cryptographically secure random bytes;
- current, home, temporary, and executable paths; and
- available parallelism.

Process exit status is a genuine missing foundational capability, but its
unwind and Task semantics are independent of libuv OS queries. It requires a
separate focused RFC rather than being hidden here.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Command-line arguments | Pick up | Every CLI needs them; generated `main(void)` currently makes them unavailable. |
| Secure random byte fill | Pick up | Required for identifiers, randomized hashing, protocols, and cryptographic libraries. |
| Read-only environment lookup | Skip in v1 | Even reads are not thread-safe against process-global mutation by imported C; arguments and files cover initial configuration without adding an unsafe global-state contract. |
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
Program.arguments() -> Slice<String> | Error
Program.current_directory(heap: Heap) -> String | Error
Program.home_directory(heap: Heap) -> String | Error
Program.temporary_directory(heap: Heap) -> String | Error
Program.executable_path(heap: Heap) -> String | Error
Program.available_parallelism() -> UInt32

Random.fill(into: Slice<mut Byte>) -> Nil | Error
```

- `Program` and `Random` are protected compiler-owned namespace types and have
  no values.
- Arguments are immutable process-lifetime Strings viewed through one read-only
  Slice. The runtime owns their backing storage; callers do not free it.
- Argument order and spelling preserve the host invocation after target-specific
  conversion to valid UTF-8. An argument that cannot be represented as valid
  UTF-8 makes `arguments()` return `ErrorKind.InvalidInput()`.
- Path results are owned Hexal Strings allocated from the caller's Heap.
- Available parallelism is a non-zero estimate, matching libuv's contract. It
  does not expose scheduler worker count or mutate scheduler policy.

The signatures are settled for v1.

## Command-line argument backend

- Generated entrypoints change from `main(void)` to a form that receives the
  host invocation where the target ABI supplies it.
- POSIX preserves `argc`/`argv` order and copies or indexes their bytes into
  process-lifetime Hexal String headers after validating UTF-8.
- Windows reads the Unicode command line and converts its arguments from UTF-16
  to UTF-8 through the smallest qualified native path. `uv_setup_args` stores
  argv for libuv; it does not perform this conversion and is not claimed as the
  Windows argument backend.
- Argument storage is initialized once before module statements and before
  scheduler startup. Failure is recorded for `Program.arguments()` rather than
  silently substituting an empty list.
- Programs that never reach `Program.arguments()` may still need the ABI-level
  `argc`/`argv` entrypoint spelling, but allocate no argument snapshot solely for
  unused source functionality.

## Secure random contract

- `Random.fill` fills the entire destination or returns Error; short success is
  impossible.
- An empty Slice succeeds immediately, touches no memory, and submits no libuv
  request.
- The Slice remains exclusively writable by the call until completion.
- Inside a Task, use asynchronous `uv_random` and park only that Task. Libuv
  explicitly permits entropy acquisition to wait indefinitely; Hexal must not
  block a scheduler worker on that possibility.
- Outside a Task, use the documented synchronous `uv_random` form.
- On failure, destination contents are unspecified. Callers must not treat them
  as random data.
- Failure uses the common libuv ErrorKind mapper with fixed message
  `secure random fill failed`; no native code or backend text is retained.
- Random use selects libuv and the native bootstrap. Task-aware use additionally
  selects the event bridge; synchronous-only use does not.
- There is no prior entropy backend to remove.

## Path-query contract

- Use `uv_cwd`, `uv_os_homedir`, `uv_os_tmpdir`, and `uv_exepath` respectively.
- Home-directory and temporary-directory queries share one private runtime
  mutex because libuv documents them as not thread-safe. Concurrent foreign
  mutation of the process environment while either query executes is an unsafe
  C-interop conflict; Hexal exposes no environment mutation in v1.
- Retry only the documented `UV_ENOBUFS` sizing protocol, with checked size
  arithmetic and a bounded retry if the process-global value changes between
  sizing and retrieval.
- Copy the successful UTF-8 bytes into the caller's Heap before releasing
  temporary runtime storage.
- Perform no normalization, canonicalization, symlink resolution, separator
  rewriting, or case folding.
- Home and temporary directory are observations, not security guarantees and
  not proof that a path exists or is writable.
- These synchronous libuv helpers do not select the event loop. They select
  libuv and the native bootstrap only.
- When executable-path support requires `uv_setup_args`, the generated
  entrypoint calls it exactly once before module statements and before any
  executable-path query. It does not transfer ownership of the immutable
  Hexal argument view.

## Namespace decision

- `Program` owns arguments, paths, and available parallelism.
- `Random` owns secure system entropy.
- There is no general `Os` namespace. A later focused capability adds its own
  cohesive type rather than extending an unbounded miscellaneous catalog.

## Process exit status: required separate work

Hexal currently always returns success from generated `main`. A separate RFC
must choose one source shape and define:

- whether ordinary `defer` actions run;
- whether pending print output is flushed;
- whether detached Tasks and native work are abandoned;
- the accepted source integer type and target conversion;
- behavior for values outside the portable exit-status range; and
- whether termination is a statement, a bottom-typed function, or the result
  of the root program.

This RFC neither adds `exit` syntax nor silently assigns exit meaning to an
existing root expression.

## Required implementation work

1. Add protected Program and Random metadata without new grammar.
2. Change generated entrypoint adapters to receive the host invocation and add
   demand-driven argument initialization.
3. Implement Windows UTF-16 argument conversion and POSIX UTF-8 validation.
4. Implement secure fill with synchronous and Task-parking libuv paths.
5. Implement the four sized path queries.
6. Expose available parallelism without coupling it to scheduler worker count.
7. Add exact ErrorKind/message mappings and ensure failure reporting performs no
   additional allocation.
8. Add demand-driven components and prove programs using none of the surface do
   not allocate snapshots or emit helpers for it.
9. Validate generated C and runtime behavior under every qualified target.
10. Update `docs/reference.md` only after behavior stabilizes and only with
    explicit user approval.

## Validation direction

The final exhaustive Validation section must cover:

- zero, one, empty, non-ASCII, invalid-encoding, and many arguments in order;
- Windows quoting/conversion and POSIX byte validation;
- secure random empty and non-empty fills, full success, failure, Task and non-
  Task paths, buffer lifetime, and unspecified failure contents;
- resizing races and exact ownership for every path query;
- non-zero available parallelism without changing scheduler configuration;
- exact demand selection and absence of unrelated OS-query APIs;
- no environment access or mutation, cwd mutation, process title, metrics, user/group,
  interface, time-of-day, or deterministic PRNG surface; and
- ordinary and tagged C23 gates with manifest movement confined to the exact
  programs selecting these capabilities.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation adds
only the settled Program and Random contracts after behavior stabilizes.
