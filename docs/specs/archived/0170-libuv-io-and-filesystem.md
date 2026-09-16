# RFC 0170: libuv File

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented. `docs/reference.md` documents the File and
  FileMode surface
- Created: 2026-09-13
- Updated: 2026-09-13
- Scope: add regular-file open, read, write, seek, flush, and close over libuv
- Depends on: RFC 0145, RFC 0168, and RFC 0169
- Coordinates with: ADR 0055 and the reference's current IO contracts
- Does not add: async syntax, exposed descriptors, `uv_fs_t`, named arguments,
  a `Path` type, directory iteration, metadata, path mutation, filesystem
  watching, or build-driver filesystem access

## Summary

Add a small regular-file surface backed by `uv_fs_*`. Inside a Task,
submit a callback request and park only that Task. Outside a Task, use libuv's
documented callback-null synchronous form.

This RFC does not migrate current standard-stream IO:

- current Windows IO stores a native `HANDLE`, while `uv_fs_*` consumes a CRT
  file descriptor;
- current IO read, write, seek, close, and print therefore retain the RFC 0145
  path: direct native calls outside a Task and `uv_queue_work` inside a Task;
- current IO Error text and native cursor-sharing behavior remain unchanged;
- IO-only and print-only programs continue to avoid libuv linkage; and
- a future explicit representation migration requires its own RFC and measured
  justification.

The public filesystem handle is a distinct `File` over `uv_file`. File failures
use stable portable Error headers.

## Backend rules

- New path-based operations use the corresponding `uv_fs_*` operation when
  libuv supplies one.
- A synchronous call passes no callback, reads its result, consumes any result
  data, calls `uv_fs_req_cleanup`, and then releases request storage.
- A Task call keeps its request, submitted buffers, paths, and result storage
  live through the final callback. The callback publishes the complete result
  before waking the Task.
- Each request calls `uv_fs_req_cleanup` exactly once after its result-owned data
  is no longer needed, on success and failure.
- No libuv callback runs Hexal user code.
- Libuv error numbers remain private backend evidence. Public code receives an
  Error with a stable portable header.
- `IO.seek` retains its current native operation; libuv has no general seek
  request.
- Filesystem requests share libuv's global worker pool with DNS, random, and
  `uv_queue_work`. The first implementation accepts this contention and adds no
  Hexal pool-size setting.

## Generated component

Reachable File use selects one program-wide pair:

```text
hexal/file.h
hexal/file.c
```

- `file.h` exposes no libuv or platform name. File lowers there to
  `{ intptr_t desc, uint8_t access }`; `desc` stores the numeric `uv_file`
  value without exposing its typedef.
- `file.c` alone includes `<uv.h>` and the target headers needed by the direct
  seek exception.
- FileMode lowers to private fixed constants. Its public identity remains the
  protected Hexal ADT.
- File alone selects `RuntimeLibuv` and mimalloc but not the event component.
- File alone invokes RFC 0169's native bootstrap before its first libuv call;
  it does not rely on scheduler initialization to install mimalloc.
- File combined with reachable Task scheduling selects the event component and
  emits both synchronous and Task-parking paths. This is the same conservative
  program-wide selection rule current IO uses.

## Path contract

The initial design uses `String` for paths rather than adding `Path`:

- reject an embedded NUL before submission;
- pass UTF-8 bytes to libuv;
- perform no lexical normalization, canonicalization, implicit absolute-path
  conversion, or case folding;
- preserve the chosen operation's libuv/host symlink behavior; and
- keep the submitted path bytes live through request completion.

An embedded NUL is an ordinary `File.open` failure: return Error header
`invalid path` and message `file open failed` before creating a request or
changing native state. It is not a compile-time diagnostic, because a path may
be computed at runtime.

The runtime filesystem and ADR 0055 build driver remain separate. The core
compiler never reads or writes host files.

## File surface

```text
File.open(path: String, mode: FileMode) -> File | Error
File.read(into: List<Byte>, max: Size)   -> Size | EoS | Error
File.write(from: Slice<Byte>)            -> Size | Error
File.seek(to: Seek)                      -> Size | Error
File.flush()                             -> Nil | Error
File.close()                             -> Nil | Error

type FileMode is Read | Write | Append | ReadWrite | CreateNew end
```

- `Read()` opens an existing file read-only and fails when it is absent.
- `Write()` opens write-only, creates when absent, and truncates when present.
- `Append()` opens write-only, creates when absent, and places every write at
  the then-current end under the target's append semantics.
- `ReadWrite()` opens an existing file for reading and writing without
  truncation and fails when it is absent.
- `CreateNew()` creates a new write-only file and fails when the path exists.
- New POSIX files request mode `0666` and remain subject to the process umask.
  Windows uses the equivalent ordinary-file permissions available through the
  libuv CRT path.
- No mode exposes native flag integers. A later concrete use case may add an
  options object without changing these five canonical modes.
- Read is permitted by Read and ReadWrite. Write and flush are permitted by
  Write, Append, ReadWrite, and CreateNew. Seek and close are permitted by all
  five modes. Capability checking precedes the zero-length transfer fast path.
- The backend flags are exact: Read uses `O_RDONLY`; Write uses
  `O_WRONLY | O_CREAT | O_TRUNC`; Append uses
  `O_WRONLY | O_CREAT | O_APPEND`; ReadWrite uses `O_RDWR`; CreateNew uses
  `O_WRONLY | O_CREAT | O_EXCL`.
- Read and write pass libuv offset `-1`, so the operation uses and advances the
  descriptor's shared current position. This is what makes copied File values
  observe one cursor. Append additionally relies on `O_APPEND`; it does not
  emulate append with a separate seek.
- File read, write, seek, EoS, partial-transfer, capability, copied-cursor, and
  explicit-close behavior matches the corresponding current IO contract.
- `flush` uses `uv_fs_fsync` and returns success only after libuv reports
  completion.
- `seek` uses `_lseeki64` on Windows and `lseek` on POSIX because libuv has no
  seek request. It executes directly: repositioning a descriptor is not sent
  through the shared blocking-work pool merely for API uniformity.
- A failed native seek passes its captured `errno` through
  `uv_translate_sys_error` before applying the same portable header table.
  Source code never observes the native or libuv number.

Defer until a concrete use case establishes the public contract:

- metadata and `stat`/`lstat`;
- rename, remove, links, and path mutation;
- directory create, remove, and iteration;
- scatter/gather IO;
- `sendfile`;
- copy-on-write flags;
- temporary file and directory creation;
- ownership and permission mutation;
- filesystem capacity through `statfs`; and
- platform-only metadata.

Libuv availability alone does not justify source-level surface. In particular,
unsupported `statfs` fields are reported as zero by libuv, which cannot express
Hexal availability without a separate contract.

## Operation rules

- Reads and writes may complete partially and return the completed byte count.
- No ordinary read or write silently loops to complete the whole request.
- A separate write-all operation may loop and must report failure after any
  partial progress according to its own contract.
- End of a regular file returns `EoS`, consistent with existing IO.
- Every File produced by this RFC owns its descriptor. Closing is explicit and
  transitions the aliased external state once.
- Copying a file handle aliases one native cursor and one close state; it does
  not duplicate the native descriptor.
- No operation retries or changes an interruption result merely to imitate a
  direct POSIX call. The implementation must probe the pinned libuv behavior
  and document the portable Hexal result rather than promising the old native
  error text for this new API.

## Settled file handle

```hexal
file := try File.open("notes.txt", FileMode.Read())
count := try file.read(buffer, 4096)
try file.close()
```

- `File` represents regular files opened through libuv.
- Existing `IO` remains the standard-stream and imported-native-handle type.
- Future `TCP`, `UDP`, `Pipe`, and `TTY` remain distinct types matching their
  different lifecycle and operation sets.
- C interoperability may expose explicit unsafe adapters later; it does not
  distort the safe file representation now.
- `File` lowers to a compact value containing `uv_file` and access capability.
  Every constructor in this RFC creates an owned descriptor, so no redundant
  per-value ownership flag is stored. It is not a pointer to a universal
  allocated IO control block.
- Copying a File aliases its native cursor and external close state under the
  same checker-assisted external-state rules as current IO.

This is the smallest representation and follows libuv's own separation between
filesystem descriptors and persistent stream handles.

Reusing current IO was rejected because it would need to distinguish Windows
native handles from libuv CRT descriptors. A universal allocated tagged handle
was rejected because it adds allocation and alias machinery while erasing the
useful distinction between files, sockets, pipes, and terminals.

## Portable Error headers

Filesystem code must distinguish expected outcomes without parsing prose or
native numeric codes:

```hexal
result := File.open("settings.toml", FileMode.Read())

match result is
| File then use(result)
| Error then
    if result.header == "not found" then
        create_defaults()
    elseif result.header == "permission denied" then
        report_denied()
    else
        report(result)
    end
end
```

- File uses one short ASCII header for each portable category. The mapping is
  normative and identical on every target:

| Libuv result | Error header |
| --- | --- |
| `UV_ENOENT` | `not found` |
| `UV_EACCES`, `UV_EPERM` | `permission denied` |
| `UV_EEXIST` | `already exists` |
| `UV_EINVAL` from open, `UV_ENAMETOOLONG`, `UV_ELOOP` | `invalid path` |
| `UV_ENOTDIR` | `not a directory` |
| `UV_EISDIR` | `is a directory` |
| `UV_ENOTEMPTY` | `directory not empty` |
| `UV_EROFS` | `read only` |
| `UV_EBUSY` | `busy` |
| `UV_EINTR` | `interrupted` |
| `UV_ECANCELED` | `cancelled` |
| `UV_ENOSYS`, `UV_ENOTSUP` | `unsupported` |
| every other negative result | `filesystem error` |

- Mapping is operation-aware: `UV_EINVAL` means `invalid path` only for open.
  The same code from read, write, flush, close, or the native seek exception
  falls through to `filesystem error`; it must not be mislabelled as a path
  failure.
- Message is a static operation-specific String such as `file open failed`,
  `file read failed`, `file write failed`, `file seek failed`, `file flush
  failed`, or `file close failed`.
- Native/libuv numeric codes and the path remain backend diagnostic evidence;
  they are not stored in the portable Error. This avoids hidden allocation,
  borrowed path lifetime, and target-dependent source behavior.
- Existing IO headers remain unchanged; this contract initially applies only
  to File.
- This uses Error's existing classification-shaped field and adds no type,
  syntax, representation field, or prerequisite RFC.
- Hexal match patterns do not currently include arbitrary Strand/String
  literals, so source uses ordinary equality and `if`/`elseif`. Adding text
  patterns merely for Error headers is out of scope.
- Misspelling a header is not diagnosed and the compiler cannot check that all
  categories were handled. This is the accepted simplicity cost. Adding
  `ErrorKind`, operation-specific failure ADTs, or text-literal match patterns
  is rejected for this RFC as disproportionate language surface.

## Required sweep

- State explicitly that there is no existing path-based Hexal filesystem
  implementation to remove.
- Do not delete the current native IO backend; this RFC does not replace it.
- Remove only newly superseded code introduced while implementing this RFC.
- Keep filesystem watching in RFC 0175 and driver filesystem access in ADR
  0055.

## Implementation plan

This plan is executable in the listed order.

### Phase 1: settle shared contracts

1. Treat the distinct compact `File` representation as fixed.
2. Implement the fixed portable Error-header table.
3. Treat the signatures and five FileMode variants in this RFC as exhaustive.
4. Add grammar only if the chosen API requires new syntax; the recommended API
   requires none.
5. Add protected identities and placement rules in `compiler/types`; implement
   File call checking beside current IO checking without sharing the two
   representations.

### Phase 2: add the generated component

1. Add demand-driven `hexal/file.h` and `hexal/file.c`.
2. Add the stack-resident private request context and exact cleanup owner
   without exposing `uv_fs_t`.
3. Implement synchronous callback-null and Task-parking submission paths.
4. Map libuv failures into Error values using the fixed header table and static
   operation message.
5. Add lowering and demand discovery in focused generator files; do not put
   File templates back into Go string literals.

### Phase 3: implement the minimum surface

1. Implement open/close and partial read/write.
2. Implement direct seek and libuv flush.
3. Reject embedded NUL before any libuv call.
4. Add public-pipeline cases in `compiler/tests/integration/file_test.go`,
   generated-component structural tests beside the generator, tagged runtime
   fixtures in `compiler/tests/c23validation`, and compact workbench snippets
   for the settled surface.

### Phase 4: prove lifetimes and races

1. Exercise immediate submission failure and delayed completion.
2. Exercise operation completion racing the Task park commit.
3. Prove exactly-once wake, cleanup, and native close.
4. Exercise copied handles sharing cursor and close state.

### Phase 5: integrate and sweep

1. Select the File pair, libuv, mimalloc, native bootstrap, and event component
   according to the exact demand rules above.
2. Verify current IO-only and print-only artifact sets remain unchanged.
3. Measure worker-pool contention, generated size, link time, and allocations.
4. Run generated C through every qualified target/toolchain gate.
5. Update `docs/reference.md` once after behavior stabilizes and with explicit
   user approval.

## Validation

This section is exhaustive.

### Types and checking

- `File` and `FileMode` are protected and cannot be redeclared.
- Every FileMode unit variant requires call-shaped construction.
- File admits exactly the six operations and signatures listed above.
- File is valid in the same positions as IO; placement does not expose its
  descriptor or libuv type.
- A statically known capability mismatch rejects at the call; an escaped or
  unknown handle performs the runtime capability check.
- Locally proved use after close and repeated close reject. This RFC provides
  no borrowed-File constructor or adapter; any later unsafe adapter must define
  its own close authority.
- Every deferred operation listed above is absent and rejected as an unknown
  File operation.
- Read, write, flush, seek, and close admit exactly the FileMode capabilities
  listed by this RFC; zero-length transfer does not bypass the check.

### Modes and paths

- `Read()` succeeds only for an existing readable file.
- `Write()` creates an absent file and truncates an existing file.
- `Append()` creates an absent file and appends without overwriting existing
  bytes.
- `ReadWrite()` preserves an existing file and fails when absent.
- `CreateNew()` creates an absent file and returns `already exists` otherwise.
- Created POSIX files request `0666` before umask.
- Empty paths follow libuv/host behavior; embedded NUL returns exactly
  `invalid path: file open failed` before request creation or native state
  change.
- Relative paths remain relative to the process working directory. No path is
  normalized, case-folded, canonicalized, or made absolute.

### Transfers and state

- Zero-length read/write returns `Size(0)` without a libuv request.
- Positive reads append at most `max`, preserve prior List contents, return
  short counts, and return `EoS` only when zero bytes are read at file end.
- Writes issue one operation and return its partial count; ordinary write does
  not become write-all.
- Start, Current, and End seeks return the resulting non-negative position and
  preserve the current IO overflow and invalid-position diagnostics.
- Flush completes only after `uv_fs_fsync` succeeds.
- Copies share one cursor and external close state. Close invalidates every copy
  even when close reports failure and is never retried by Hexal.
- Concurrent use requires external synchronization; no cross-Task ordering or
  compound-write atomicity is promised.
- Root completion does not wait for a detached Task performing File work.
  Process exit may abandon that request under the existing detached-Task
  contract; this RFC adds no global filesystem shutdown.

### Errors

- Each listed libuv result maps to exactly the specified header on every
  target; an unlisted negative value maps to `filesystem error`.
- Each operation supplies exactly its static message and performs no failure-
  path allocation.
- File Error headers contain no operation spelling, path, native number,
  `errno=`, or `winerr=` fragment.
- Existing IO retains its current target-specific diagnostic headers.

### Requests and generated C

- Synchronous non-Task calls use the callback-null libuv form and call
  `uv_fs_req_cleanup` exactly once after consuming the result.
- Task calls park only the caller, publish the complete result before exactly
  one wake, and keep request, path, buffer, and result storage live through the
  callback and cleanup.
- Submission failure does not park or mutate File state.
- No libuv callback runs user code.
- File seek uses only the stated minimal native operation; every other File
  operation uses its specialized libuv filesystem request.
- Native seek failure is translated through `uv_translate_sys_error` and then
  through the same portable-header mapper; raw `errno` is never exposed.
- No public generated header contains a `uv_*`, Windows, POSIX, CRT descriptor,
  or platform-conditional signature.

### Demand and regression

- Reachable File use selects its generated component, `RuntimeLibuv`, and
  mimalloc exactly once. File without Task selects no event component; File
  combined with Task selects it exactly once.
- File-only root C calls `hex_runtime_native_init()` before the first File
  operation and emits no scheduler initialization.
- Current IO/print without Task remains byte-identical and selects no libuv.
- Existing Task-aware IO and print behavior remains unchanged.
- Worker-pool saturation does not block scheduler-worker progress.
- Ordinary Go tests require no external toolchain.
- Tagged generated-C tests run every mode, failure header, transfer edge,
  copied-handle case, synchronous path, and Task path under each qualified
  toolchain.
- Existing snippet-manifest entries do not change; the manifest gains entries
  only for new File snippets.

## Reference synchronization

Do not edit `docs/reference.md` from this spec without explicit user approval.
An approved implementation must
add only the settled File/FileMode surface and leave current IO contracts unchanged.
