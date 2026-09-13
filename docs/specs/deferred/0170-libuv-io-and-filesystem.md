# RFC 0170: libuv IO and Filesystem

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: implement existing IO and future filesystem operations through the
  libuv filesystem family
- Depends on: ADR 0145, RFC 0168, and RFC 0169
- Coordinates with: ADR 0055 and the reference's current IO contracts
- Does not add: async syntax, exposed descriptors, or `uv_fs_t`

## Summary

Use `uv_fs_*` as the sole backend for every matching file and filesystem
operation. Inside a Task, use callback completion and park the fiber. Outside a
Task, call the synchronous callback-null form of the same API.

Existing IO remains synchronous-looking:

```hexal
count := try input.read(buffer, 4096)
try output.write(buffer.slice(0, count))
try output.close()
```

Future path, directory, and metadata APIs require exact child decisions here
before implementation.

## Libuv ownership

Use the corresponding libuv operation exclusively for:

- open and close;
- read, write, scatter/gather IO, sync, datasync, truncate, and sendfile;
- stat, lstat, fstat, statfs, access, chmod, ownership, and timestamps;
- rename, unlink, copy, links, and symbolic links;
- directory create, remove, scan, and cleanup;
- real-path and filesystem metadata queries; and
- temporary file and directory creation where the pinned release supplies it.

The public design must consider efficient operations that preserve those
facilities rather than forcing all work through a Hexal byte-copy loop:

- file-to-file copy through `uv_fs_copyfile`, including optional copy-on-write
  behavior where supported;
- file-to-descriptor transfer through `uv_fs_sendfile`;
- scatter/gather IO using multiple submitted buffers;
- temporary files and directories through `uv_fs_mkstemp` and
  `uv_fs_mkdtemp`; and
- filesystem capacity and limits through `uv_fs_statfs`.

These are candidate APIs, not permission to expose platform flags verbatim.
The settled Hexal operations must state overwrite, partial-transfer, cleanup,
fallback, and unsupported-capability behavior.

`IO.seek` retains the smallest native seek operation inside `uv_queue_work`
because libuv has no general seek request. This is an explicit no-match
exception, not a parallel filesystem backend.

## Semantic direction

- Preserve current `IO.read`, `IO.write`, `IO.seek`, and `IO.close` results.
- Preserve shared native-cursor behavior when IO handles are copied.
- Preserve partial reads and writes, EoS, transfer limits, capability checks,
  explicit close, and double-close diagnostics.
- Never expose libuv error numbers. Translate them into owned Hexal `Error`
  values at the operation boundary.
- Keep request and submitted buffer storage live through final callback.
- Use the same backend for print and descriptor IO.
- Never make filesystem operations a core-compiler filesystem responsibility;
  runtime IO and the build driver's host access remain separate layers.

## Required sweep

Remove direct Windows, POSIX, and C-runtime implementations for operations now
owned by `uv_fs_*`, including their duplicated errors, headers, feature tests,
and platform branches. Retain only the proven seek work function and facilities
for operations absent from libuv.

## Detailed implementation outline

1. Inventory every existing IO native operation and map it to libuv.
2. Build the common filesystem request context over RFC 0169's request base.
3. Migrate close, read, write, and print while preserving byte-level behavior.
4. Move seek through `uv_queue_work` and delete its old pool route.
5. Settle Path, open-mode, permission, metadata, and directory types.
6. Add copy, sendfile, scatter/gather, temporary storage, and statfs through
   focused operations that preserve their native efficiency.
7. Add the remaining filesystem operations in cohesive API groups.
8. Delete every superseded native path and verify generated-component demand.
9. Run real generated-C tests for ordinary files, standard handles, pipes,
   partial transfers, EoS, errors, and cleanup.

## Open design questions

1. Is a filesystem path represented by `String`, a distinct `Path`, or another
   type?
2. What is the smallest open-mode and permission model that remains C-capable?
3. Which metadata fields are portable guarantees and which are target data?
4. How are directory iteration and entry lifetimes represented?
5. Which imported Windows handle classes map exactly to libuv file or stream
   handles?
6. Does root shutdown wait for or cancel outstanding filesystem requests?
7. Does file-to-descriptor transfer accept any writable IO or only qualified
   file/socket classes?
8. Which copy-on-write and overwrite choices belong in the portable copy API?

## Validation direction

The final exhaustive Validation section must cover existing IO conformance,
synchronous and Task-aware paths, every mapped filesystem family, cursor
sharing, partial transfers, request cleanup, buffer lifetime, capability
errors, file copy and its failure cleanup, sendfile partial progress,
scatter/gather ordering, temporary-path ownership, statfs field mapping,
platform limitations, no duplicate backend, and demand-driven linkage.

## Reference synchronization

Do not edit `docs/reference.md` from this deferred proposal. The approved
implementation must update IO contracts and add only the settled public
filesystem surface.
