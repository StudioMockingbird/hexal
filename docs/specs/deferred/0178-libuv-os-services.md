# RFC 0178: libuv OS Services

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: define secure random bytes and focused portable OS/system queries over
  libuv
- Depends on: ADR 0145, RFC 0168, RFC 0169, and RFC 0170 for path types
- Does not add: a general platform-reflection API or stable exposure of every
  libuv structure

## Summary

Use libuv as the sole backend for every matching random, environment, path,
host, user, process, CPU, memory, network-interface, uptime, load, resource,
and time-of-day query that Hexal chooses to expose.

Candidate cohesive surfaces:

```hexal
try Random.fill(buffer)
home := try Os.home_directory()
host := try Os.hostname()
cpus := Os.available_parallelism()
```

Names and return types remain open.

## Capability groups

### Secure random

- `uv_random` supplies cryptographically strong bytes.
- Task calls use asynchronous completion; non-Task calls use the synchronous
  form.
- Short success is not part of the contract: fill all requested bytes or
  return `Error`.

### Environment and paths

Use libuv for environment get/set/unset, working-directory get/change,
executable path, home directory, temporary directory, and process title where
exposed.

### Host and user

Use libuv for hostname, uname-style platform information, password/user data,
process identity, and parent-process identity where exposed.

### CPU, memory, interfaces, and resources

Use libuv for available parallelism, CPU information, total/free/constrained
memory, resident-set size, uptime, load average, network interfaces, resource
usage, and process priority where exposed.

Loop-specific operational metrics remain internal to RFC 0169 unless a later
RFC demonstrates a stable user-facing need.

## Semantic direction

- The API exposes typed facts rather than raw libuv structs.
- Fallible queries return `Error`; infallibility is promised only where the
  qualified libuv contract supports it.
- Strings are owned Hexal Strings and remain valid after libuv storage cleanup.
- Returned collections own their contents.
- Security-sensitive random behavior is documented independently from
  deterministic pseudo-random algorithms.
- Platform absence is an explicit unsupported result, not fabricated data.
- Units, widths, optional fields, and overflow behavior are fixed per query.

## Required sweep

Remove direct operating-system and C-runtime implementations for every adopted
query. Keep no alternate entropy, environment, CPU, memory, interface, or host
backend on libuv-qualified targets.

## Detailed implementation outline

1. Inventory the pinned libuv miscellaneous and OS-query APIs.
2. Group only operations with cohesive types and ownership.
3. Settle Path, user, CPU, memory, interface, resource, and platform types.
4. Implement secure random first with synchronous and Task-aware paths.
5. Add environment/path, host/user, then resource/query groups.
6. Copy all returned variable storage into owned Hexal values before cleanup.
7. Delete every superseded platform implementation.
8. Validate values, failures, cleanup, thread safety, target support, and
   generated dependency demand.

## Open design questions

1. Which queries justify a stable public API rather than remaining internal?
2. Is the namespace `Os`, `System`, focused types, or another form?
3. How are target-specific missing fields represented without a builtin
   Option type?
4. Which process-global mutations are unsafe or prohibited under parallel
   Tasks?
5. Does deterministic pseudo-random generation belong in a separate pure
   library?

## Validation direction

The final exhaustive Validation section must cover full random fills, zero
length, entropy failure, synchronous and Task paths, owned returned storage,
every exposed query's units and errors, process-global mutation races,
unsupported targets, dependency demand, and absence of alternate OS backends.

## Reference synchronization

Do not edit `docs/reference.md` from this deferred proposal. An approved
implementation adds only settled OS-service contracts.
