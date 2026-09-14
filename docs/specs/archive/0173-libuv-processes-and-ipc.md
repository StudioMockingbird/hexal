# RFC 0173: libuv Processes and IPC

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented with one documented deviation, the same trade
  RFC 0172 made for TCP. `Process.start`/`wait`/`terminate`/`close` and
  `Pipe.read`/`write`/`shutdown`/`close`, plus `ProcessOptions`, `Environment`,
  `EnvironmentVariable`, `ProcessStream`, `ExitStatus`, and `StartedProcess`,
  are in place over the shared generation-checked handle registry and its
  common libuv ErrorKind mapper. Deviation: this implementation admits one
  active read and one active write at a time per Pipe; a second concurrent
  call of the same kind returns Error with kind `Busy` immediately rather
  than joining the FIFO wait queue this RFC's text specifies, accepted for
  the same tractability reason RFC 0172 accepted it for TCP. Verified by
  `go test ./...`, `go vet ./...`, and the tagged C23 suite (including a
  real `uv_spawn` child process, exit-status wait, and piped-stdout
  read/write/close exchange) running under GCC, Clang, and `zig cc`.
  `docs/reference.md` documents the public surface, the error table,
  component selection, and this deviation
- Created: 2026-09-13
- Updated: 2026-09-14
- Scope: define child-process lifecycle, child standard-stream pipes, exit
  observation, and process control over libuv
- Depends on: ADR 0145, RFC 0168, RFC 0169, RFC 0181, and RFC 0180
- Coordinates with: RFC 0172 for shared stream
  semantics, and RFC 0176 for portable signal identities
- Does not add: shell-language parsing, async syntax, named local IPC, IPC
  handle passing, spawn-time detachment, or raw process IDs as the primary
  ownership model

## Summary

Use `uv_spawn`, `uv_process_t`, and `uv_pipe_t` as the sole portable backend
for processes and child standard streams. Named local IPC and handle passing
remain separately gated follow-ons.

Conceptual source shape:

```hexal
fun run_worker(heap: Heap): ExitStatus | Error do
    arguments := List<String>(heap)
    arguments.push("--once")
    options := ProcessOptions(
        program = "worker",
        arguments = arguments,
        environment = Environment.Inherit(),
        working_directory = nil,
        input = ProcessStream.Inherit(),
        output = ProcessStream.Pipe(),
        error = ProcessStream.Inherit(),
    )
    started := try Process.start(options)
    status := try started.process.wait()
    try started.process.close()
    return status
end
```

These are the settled names and value shapes. The example deliberately uses
named fields only in struct/ADT construction; Hexal does not have named
function arguments.

## First implementation boundary

The first implementation contains:

- direct executable invocation with an argument vector and no shell;
- an optional working directory;
- exactly two environment modes: inherit the parent environment or replace it
  completely;
- ignored, inherited, or newly created child stdin/stdout/stderr;
- Task-parking exit observation with one cached immutable result;
- explicit process termination; and
- explicit close.

Named local IPC endpoints and IPC handle passing are deferred. The first child
pipe type exists only for process standard streams; a later focused RFC may
generalize the same libuv stream representation into public local IPC.

The first implementation has no wait deadline and no public Task cancellation.
Closing is the only cancellation request for pipe operations; process
termination remains a distinct explicit operation.

## Public surface

All names in this section are compiler-protected builtins. They remain
compiler-owned until a future standard-library split moves declarations out of
the compiler without changing their contracts.

```text
type Environment is union
    | Inherit
    | Replace as values: List<EnvironmentVariable> end
end

type EnvironmentVariable is struct
    name: String,
    value: String,
end

type ProcessStream is Ignore | Inherit | Pipe end

type ProcessOptions is struct
    program: String,
    arguments: List<String>,
    environment: Environment,
    working_directory: String | Nil,
    input: ProcessStream,
    output: ProcessStream,
    error: ProcessStream,
end

type ExitStatus is union
    | Exited as code: Int64 end
    | Terminated
end

type StartedProcess is struct
    process: Process,
    input: Pipe | Nil,
    output: Pipe | Nil,
    error: Pipe | Nil,
end

Process.start(options: ProcessOptions) -> StartedProcess | Error
Process.wait()                         -> ExitStatus | Error
Process.terminate()                    -> Nil | Error
Process.close()                        -> Nil | Error

Pipe.read(into: List<Byte>, max: Size) -> Size | EoS | Error
Pipe.write(from: Slice<Byte>)          -> Nil | Error
Pipe.shutdown()                        -> Nil | Error
Pipe.close()                           -> Nil | Error
```

- `program` names the executable. `arguments` contains only arguments after
  argument zero; the runtime supplies `program` as argument zero.
- No shell parses `program` or any argument. Embedded NUL in a program,
  argument, working directory, environment name, or environment value returns
  InvalidInput before native submission.
- A relative `program` uses the host's ordinary executable search. Hexal does
  not normalize or interpret it. Search order is platform behavior: Windows
  may consider the current directory before PATH, unlike a POSIX `execvp`
  search. Security-sensitive callers use an absolute program path.
- `Environment.Inherit()` copies the parent environment at spawn.
  `Environment.Replace(values = ...)` supplies exactly the List entries and
  does not merge them with the parent environment. Values are typed
  EnvironmentVariable records in source order; no `NAME=value` parsing surface
  exists.
- Environment names must be non-empty and contain neither `=` nor NUL.
- Duplicate names return InvalidInput before submission. POSIX compares names
  byte-for-byte. Windows compares names case-insensitively according to its
  environment-name rules. Entry order has no semantic effect.
- `working_directory = nil` inherits the parent's current directory.
- `ProcessStream.Ignore()` connects no parent-facing stream;
  `ProcessStream.Inherit()` uses the matching parent standard stream; and
  `ProcessStream.Pipe()` creates one parent-facing Pipe.
- `StartedProcess.input`, `.output`, and `.error` are non-Nil exactly where the
  matching option was `Pipe()`. Input is writable; output and error are
  readable. An operation contrary to that capability returns
  PermissionDenied.
- Exit code is preserved as libuv's signed 64-bit result. `Terminated()` means
  the target reported signal termination; v1 does not expose a platform signal
  number.
- `Process.close()` and `Pipe.close()` are valid `defer` and `errdefer`
  cleanup calls.

`Process`, `Pipe`, `ProcessOptions`, `StartedProcess`, `Environment`,
`EnvironmentVariable`, `ProcessStream`, and `ExitStatus` cannot be redeclared
or shadowed.

## Handle, allocation, and snapshot contract

`Process` and `Pipe` use RFC 0180's generation-checked copied handles and valid
storage positions. Their control blocks, requests, argument/environment
snapshots, and waiter metadata use the private mimalloc-backed runtime
allocator. `Process.start` therefore takes no source-level Heap.

Before enqueueing the spawn command, `Process.start` validates and snapshots
the program, argument vector, optional working directory, environment, and
stdio choices. Later source collection mutation cannot change the submitted
process. Every snapshot is released on spawn failure or after `uv_spawn` no
longer needs it.

## Semantic direction

- Arguments are passed as an argument vector; no implicit shell exists.
- Environment and working directory are explicit options.
- Newly created standard streams use a dedicated long-lived `Pipe` handle over
  `uv_pipe_t`. Current `IO` and `File` representations do not contain libuv
  stream handles and are not changed by this RFC. V1 has no option that accepts
  an existing IO or File value.
- Only Process/Pipe operations that create a native resource or can park select
  the handle runtime, scheduler, event component, libuv, and native bootstrap.
  Constructing or inspecting ProcessOptions, ProcessStream, Environment,
  ExitStatus, or other inline option values selects only their type-definition
  component. There is no synchronous non-Task process or pipe operation path.
- Waiting parks the current Task and does not block a scheduler worker.
- Process exit status and terminating signal remain distinguishable where the
  platform provides both.
- Closing a process handle does not imply killing a live process.
- Process termination, waiting, and handle cleanup have separate explicit
  contracts. V1 has no detached process option.
- Pipes use the same pull-read, FIFO write-all, borrowed-buffer, full-duplex,
  and close-wakes-waiters contracts as TCP connections in RFC 0172.
- A caller must concurrently drain a requested stdout/stderr Pipe while a child
  may write to it. Waiting for child exit before reading can deadlock once the
  operating-system pipe buffer fills; Hexal adds no unbounded capture buffer.
- IPC handle passing remains deferred until it has an ownership contract.

### Process lifecycle direction

- `wait()` may be called by multiple Tasks and after process exit. Every
  successful call returns the same cached immutable `ExitStatus`.
- Closing a live Process never kills it and never closes the native libuv
  process handle early. It invalidates the public handle while private runtime
  state remains until the exit callback has reaped the child and the close
  callback releases the native handle.
- Closing after exit releases the public handle through the same terminal close
  path.
- Root completion does not implicitly terminate or wait for children. A child
  remains an external process if root exits without waiting or terminating it.
- `ExitStatus` is a closed ADT with distinct normal-exit and signal-exit
  variants. Signal exit is produced only when the target reports it.
- `terminate()` is the one v1 termination request. It requests prompt process
  termination through `uv_process_kill(process, SIGTERM)`. On POSIX this sends
  catchable SIGTERM; on Windows libuv implements SIGTERM with forceful
  `TerminateProcess`. It is not promised to run a portable graceful shutdown
  protocol and has no separate forceful companion.
- Exit and close share one slot lock. If exit completion linearizes first, an
  already parked `wait()` receives the cached ExitStatus. If close linearizes
  first, active waiters receive Closed and a later exit callback performs only
  private reaping and cleanup.
- Every child retains private runtime state until the exit callback reaps it.
  Public close never abandons the native reaping obligation while the Hexal
  process remains alive.

## Errors

Process and Pipe operations first apply RFC 0180's portable libuv ErrorKind
mapper. Local and fallback kinds are:

| Condition | ErrorKind |
| --- | --- |
| invalid option or embedded NUL | InvalidInput |
| Pipe direction/capability mismatch | PermissionDenied |
| closed Process or Pipe, including a close-cancelled Pipe operation | Closed |
| other spawn/process/wait/terminate failure | Other(header = `process error`) |
| other Pipe failure | Other(header = `pipe error`) |

Messages name the failed operation and follow RFC 0181's one settled diagnostic
detail representation. They never embed command text, arguments, or paths.
Every Error carries the Hexal call site's source location.

## Libuv ownership

- `uv_spawn` owns process creation and standard-stream setup.
- `uv_process_t` owns exit notification and safe process-target identity.
- `uv_process_kill` owns process signalling where supported.
- `uv_pipe_t` owns child standard-stream pipes.
- libuv stream operations own pipe reads, writes, shutdown, and close.

No direct `CreateProcess`, `fork`/`exec`, or `waitpid` backend exists today or
may coexist with the matching libuv operation. This RFC adds no named-pipe or
Unix-domain-socket public endpoint.

On POSIX, runtime initialization ignores SIGPIPE as required by RFC 0172's
common stream contract. A child-pipe write to a closed reader therefore
returns BrokenPipe rather than terminating the Hexal process; imported C code
observes the same process-wide disposition.

## Detailed implementation plan

### Phase 1: types and demand

1. Register every protected type, variant, member, constructor, method, and
   result above through the existing builtin registry.
2. Add checker rules for exact options, stdio capabilities, handle storage,
   method results, and diagnostics.
3. Select the process type-definition component from reachable option/ADT use.
   Select its C runtime, RFC 0180's handle component, event bridge, scheduler,
   libuv, and native bootstrap only from reachable Process or Pipe operations.

### Phase 2: option snapshots and spawn

1. Validate embedded NULs, environment names, target-specific duplicate names,
   and option combinations before native submission.
2. Snapshot strings, arguments, environment entries, and stdio descriptors into
   private mimalloc-backed storage.
3. Translate the snapshot to `uv_process_options_t` without a shell.
4. Create requested `uv_pipe_t` handles, submit `uv_spawn`, and return one
   `StartedProcess` only after all source-visible handles are valid.
5. Close every partially initialized native handle and release every snapshot
   on each failure path.

### Phase 3: Process lifecycle

1. Store exit completion once and wake every waiter.
2. Make concurrent and later `wait()` calls return the cached immutable result.
3. Implement the single SIGTERM `terminate()` operation with the documented
   POSIX/Windows distinction.
4. Implement public close with private post-close retention until exit and
   native close callbacks finish.
5. Implement root completion without implicit wait or kill; emit no detached
   process flag or unref path.

### Phase 4: Pipe operations

1. Implement capability-checked pull reads with pre-reserved List storage.
2. Implement FIFO write-all while borrowing source bytes until return.
3. Implement write-half shutdown and generation-checked close.
4. Reuse the common stream-operation state shape from networking; do not route
   child pipes through current IO/File or a worker pool.

### Phase 5: sweep and conformance

1. Verify no direct process, wait, or alternate pipe backend exists.
2. Add the exhaustive tests below and generated-C ownership/order assertions.
3. Run ordinary Go tests, vet, and applicable C23 validation.
4. Regenerate snippet hashes only for newly added Process snippets; no existing
   entry may change.
5. Update `docs/reference.md` only during approved implementation and remove
   this RFC's status entry when it closes.

## Validation

Validation is exhaustive for this RFC.

- Every protected name rejects redeclaration and shadowing.
- Arguments preserve exact boundaries, order, empty strings, and UTF-8 bytes;
  no shell expansion occurs; embedded NUL rejects before submission. On
  Windows, this guarantee applies to standard CRT/CommandLineToArgvW-compatible
  children; arbitrary custom command-line parsers are outside the contract.
- Relative program lookup follows the documented host search, including the
  Windows current-directory difference; absolute program invocation works.
- Environment inherit and exact typed-list replacement work; invalid names and
  the first target-defined duplicate reject before submission. Entry order does
  not affect the child's environment.
- Nil and explicit working directories behave as specified.
- Ignore, inherit, and Pipe work independently for stdin/stdout/stderr; the
  three StartedProcess fields are Nil exactly when no pipe was requested.
- Pipe direction misuse returns PermissionDenied before native submission.
- Spawn success publishes only fully valid handles. Every staged failure closes
  partial libuv handles, frees snapshots, and leaves no child or zombie.
- Normal exit preserves the signed code; signal exit returns `Terminated()`.
- Concurrent, repeated, and post-exit waits return the same cached value. An
  exit-winning close race returns that value to active waiters; a close-winning
  race returns Closed and still reaps the child privately.
- `terminate()` has one terminal outcome and reports unsupported/native failure
  without adding `kill()`.
- Closing a live Process does not kill it or close its native handle before
  reaping; public copies become closed immediately; native state is eventually
  released exactly once.
- Root exit performs no implicit child join or kill; no detached option, flag,
  or process-unref path exists.
- Pipe read, write-all, full-duplex, buffering, capability, shutdown, and close
  behavior matches the settled common stream contract.
- A child that fills an unread stdout/stderr Pipe is not hidden behind an
  unbounded buffer; concurrently draining or closing that Pipe permits
  progress.
- Every common and component fallback ErrorKind is exact, source-located, and
  target-independent.
- Process and Pipe waits occupy no scheduler worker; no operation uses the
  filesystem worker pool.
- Reachability emits exactly one process and handle component; absence emits
  none; public generated headers contain no `uv_*` type.
- Named local IPC, IPC handle passing, raw process IDs, wait deadlines, Task
  cancellation, and a second termination operation are absent.
- No direct `CreateProcess`, `fork`/`exec`, `waitpid`, named-pipe, Unix-socket,
  or alternate process backend exists.
- `go test ./...`, `go vet ./...`, and applicable tagged C23 validation pass.

## Reference synchronization

Do not edit `docs/reference.md` from this draft proposal. An approved
implementation adds only settled process and IPC contracts.
