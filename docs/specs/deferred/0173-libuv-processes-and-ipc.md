# RFC 0173: libuv Processes and IPC

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: define child-process lifecycle, standard streams, pipes, local IPC,
  exit observation, and process control over libuv
- Depends on: ADR 0145, RFC 0168, RFC 0169, and RFC 0170 for IO integration
- Does not add: shell-language parsing, async syntax, or raw process IDs as
  the primary ownership model

## Summary

Use `uv_spawn`, `uv_process_t`, and `uv_pipe_t` as the sole portable backend
for processes, process standard streams, named pipes, Unix-domain local IPC,
and handle passing where supported.

Conceptual source shape:

```hexal
process := try Process.start(
    program = "worker",
    arguments = ["--once"],
)
status := try process.wait()
try process.close()
```

Exact names and value shapes remain open.

## Semantic direction

- Arguments are passed as an argument vector; no implicit shell exists.
- Environment and working directory are explicit options.
- Standard input, output, and error use ordinary Hexal IO handles.
- Waiting parks the current Task and does not block a scheduler worker.
- Process exit status and terminating signal remain distinguishable where the
  platform provides both.
- Closing a process handle does not imply killing a live process.
- Process termination, waiting, detaching, and handle cleanup have separate
  explicit contracts.
- Pipes use the common stream bridge and backpressure rules.
- IPC handle passing is introduced only with an ownership contract.

## Libuv ownership

- `uv_spawn` owns process creation and standard-stream setup.
- `uv_process_t` owns exit notification and safe process-target identity.
- `uv_process_kill` owns process signalling where supported.
- `uv_pipe_t` owns anonymous/named pipe and local IPC transport.
- libuv stream operations own pipe reads, writes, shutdown, and close.

No direct `CreateProcess`, `fork`/`exec`, `waitpid`, named-pipe, or Unix-domain
socket backend remains for the same public operation.

## Detailed implementation outline

1. Settle Process, ProcessOptions, ExitStatus, pipe, and standard-stream types.
2. Implement option translation without shell interpretation.
3. Implement spawn failure and exit completion over RFC 0169.
4. Connect child standard streams to RFC 0170 IO.
5. Implement local pipes and IPC transport.
6. Add termination, detach, wait, close, and shutdown ownership.
7. Add handle passing only after transfer semantics are settled.
8. Delete every superseded platform process and pipe path.

## Open design questions

1. Which Process options are portable guarantees rather than target-specific
   escape hatches?
2. What is the exact ExitStatus representation?
3. Does dropping or closing a Process ever terminate it?
4. How are detached children reaped without leaking OS resources?
5. How are environment replacement and inheritance expressed?
6. Is IPC handle passing in v1, and what transfers ownership?

## Validation direction

The final exhaustive Validation section must cover argument preservation,
environment and working directory, all standard-stream modes, spawn errors,
exit and signal status, wait races, detach, termination, pipe backpressure,
IPC cleanup, root shutdown, no zombies, and no alternate native backend.

## Reference synchronization

Do not edit `docs/reference.md` from this deferred proposal. An approved
implementation adds only settled process and IPC contracts.
