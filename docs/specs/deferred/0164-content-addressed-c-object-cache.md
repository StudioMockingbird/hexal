# ADR 0164: Content-Addressed C Object Cache

- Kind: Architecture Decision Record (ADR)
- Status: Implementation-ready; blocked on ADR 0055 and a future stable backend
  identity
- Created: 2026-09-11
- Updated: 2026-09-12
- Depends on: ADR 0055's deterministic per-translation-unit object pipeline and
  a stable backend identity deliberately deferred by RFC 0052
- Does not update `docs/reference.md`: this is driver behavior, not a language
  or compiler-output contract

## Decision

Add a project-local, content-addressed cache for C object files produced from
Hexal-generated translation units.

The cache avoids repeating unchanged `zig cc -c` work. It does not skip the
Hexal compiler, cache diagnostics, infer dependencies from timestamps, or claim
incremental Hexal compilation.

The first version is deliberately local and bounded in responsibility:

- one immutable entry per object-input identity;
- exact content and toolchain identities, never timestamps;
- safe concurrent builds of the same project;
- atomic publication;
- corruption detection and automatic rebuild; and
- no eviction policy beyond explicit removal of the project build directory.

## Goals

- Compile an unchanged generated translation unit zero times after its first
  successful cache publication.
- Recompile only translation units whose semantic C inputs changed.
- Never reuse an object built by a different backend, target profile, or
  material option set.
- Never expose a partial or corrupt object as a cache hit.
- Preserve deterministic link ordering and output behavior.
- Keep all cache logic outside the core compiler.

## Non-goals

- Incremental lexing, parsing, checking, module resolution, or C generation.
- Skipping `compiler.Compile`.
- A user-wide, machine-wide, shared, remote, or distributed cache.
- Cache eviction, size quotas, background cleanup, or least-recently-used data.
- Timestamp-based freshness.
- Negative caching of compilation failures.
- C preprocessor dependency discovery for arbitrary foreign C.
- Reusing objects across target profiles even when they appear ABI-compatible.
- A language-visible cache setting.
- Caching objects produced by RFC 0052's installed `PATH` backend.

## Storage layout

The cache lives under the project's existing driver-owned build area:

```text
<root>/build/.hexal/
    cache/v1/objects/ab/<full-sha256>.obj
    cache/v1/objects/ab/<full-sha256>.json
    cache/v1/locks/<full-sha256>.lock
    staging/<build-id>/...
```

`v1` is the cache schema version. Changing key semantics or metadata format
uses a new schema directory rather than interpreting old entries differently.

The cache directory persists across builds. A staging directory is private to
one build and is removed after success or failure. Deleting `build/.hexal/` is
always a valid complete cache reset.

## Object identity

Each generated `.c` file receives one canonical object key:

```text
SHA-256(
    cache schema version
    backend identity
    target-profile identity
    normalized material compile options
    translation-unit logical key
    translation-unit bytes
    ordered transitive generated-header keys and bytes
)
```

Every field is length-delimited before hashing. Concatenation without framing
is invalid.

### Backend and target identity

Use an exact stable backend identity plus RFC 0052's target identity. A changed
backend distribution, profile schema, target, or qualified runtime
configuration produces a different key. RFC 0052's installed `PATH` backend has
no such identity, so the driver bypasses this cache completely until a future
backend-distribution design provides one.

### Compile options

Include every option that may change object contents or semantics, including
the C dialect, optimization, debug, preprocessor, ABI, visibility, and codegen
options.

Exclude staging-directory spelling and other absolute paths that are not
semantic inputs. The driver passes deterministic logical source mappings and
does not enable options that embed the temporary build directory in objects.

### Generated-header closure

The driver computes dependencies only among compiler-returned artifacts:

1. Read quoted includes from the generated translation unit.
2. Resolve them against `CompilationResult.Files` using normalized logical
   artifact keys.
3. Traverse quoted generated-header includes recursively, visiting each
   generated artifact at most once. A guarded include cycle therefore closes
   normally instead of recursing forever or failing cache discovery.
4. Reject a missing, escaping, or ambiguous generated include as a
   compiler/artifact failure.
5. Sort the resulting unique header keys and hash each key and exact content.

System-header contents are represented by backend and target-profile identity;
they are not read or hashed separately.

This scanner is not a general C preprocessor. Generated includes are
compiler-controlled and must use literal quoted paths. Foreign C dependency
discovery belongs to the future foreign-build design.

## Entry metadata

The JSON metadata beside each object contains:

```text
schema version
object key
object byte length
object SHA-256
translation-unit logical key
backend identity
target-profile identity
```

Metadata is diagnostic and integrity data. The object key is still derived
from the complete canonical input above; metadata never substitutes for key
calculation.

## Lookup

For each translation unit, in deterministic logical-key order:

1. Compute the complete object key.
2. If neither object nor metadata exists, report a miss.
3. If only one exists, treat the entry as corrupt.
4. Parse metadata and require the expected schema and object key.
5. Verify the cached object's length and SHA-256.
6. On success, use the immutable cached object as the link input without
   invoking `zig cc -c`.
7. On corruption, remove that exact entry under its key lock and rebuild once.

A second validation failure after rebuilding is a cache/filesystem failure;
the driver does not loop indefinitely.

## Miss and publication

On a miss:

1. Acquire the per-key lock.
2. Recheck the entry after acquiring the lock; another process may have
   published it.
3. Compile into a unique temporary object inside the build staging directory.
4. Require successful process exit and a regular non-empty object file.
5. Compute the object digest and complete metadata.
6. Write temporary object and metadata siblings in the cache directory.
7. Flush and close both files.
8. Publish the object and then metadata with atomic same-directory renames.
9. Release the lock.

Metadata is the commit marker. An object without its matching metadata is never
a hit. A process interrupted before metadata publication leaves, at worst, an
uncommitted object that the next lookup removes and rebuilds.

Compilation failures are returned normally and publish no cache entry.

## Concurrency and locking

Different keys compile concurrently. The same key has one publisher.

The driver uses a native exclusive file lock at
`cache/v1/locks/<key>.lock`. Waiting is blocking and contains no polling or
sleep loop. After acquiring the lock, every contender rechecks the cache before
compiling.

A process crash releases the native lock. A leftover lock file is only the
stable object used for locking; its existence does not mean the lock is held
and does not require stale-lock timestamps.

Cache entries are immutable after publication. No build edits an object or
metadata file in place.

## Corruption and failures

- Missing half-entry: remove the remaining half under lock and rebuild.
- Invalid JSON, schema, key, length, or digest: remove both files under lock and
  rebuild once.
- Permission or lock failure: report a cache/filesystem failure; do not compile
  through an unverifiable shared state.
- Compiler or `zig cc` failure: do not publish.
- Link failure: retain valid object entries; linking does not invalidate them.
- Cleanup failure for an uncommitted temporary file is reported but cannot
  create a hit.

The cache is an optimization, but silently accepting corruption is not.

## Invalidation

Invalidation is entirely key-driven:

- `.c` content change: that translation unit misses.
- Private implementation change in another `.c`: unrelated units still hit.
- Generated header change: every translation unit whose generated-header
  closure includes it misses.
- Backend, target profile, or material compile-option change: every affected
  unit misses.
- Link-only option or link-input change: objects may hit; the executable is
  relinked.
- Source timestamp or absolute project path change with identical canonical
  content: objects still hit.

The driver never deletes old entries merely to invalidate them. Different
inputs produce different immutable keys. Explicit cache cleanup reclaims old
entries later.

## Build and diagnostic behavior

`BuildResult` records one cache outcome per translation unit:

```text
hit
miss-compiled
corrupt-rebuilt
waited-then-hit
```

A cache hit records no fake compiler command. A miss records the actual command
through ADR 0055's `CommandResult`. Link ordering remains the deterministic
logical-key order, regardless of hit/miss order or parallel completion.

## Validation

This section is exhaustive for ADR 0164.

- The first build misses, compiles every generated translation unit once, and
  publishes one valid entry per unit.
- An identical second build invokes zero C compilations and links the validated
  cached objects.
- Key serialization is length-delimited and deterministic.
- Absolute project and staging paths do not affect keys.
- A private `.c` change invalidates only that translation unit and relinks.
- A generated header change invalidates exactly its transitive dependents.
- Backend, target-profile, C-dialect, ABI, optimization, debug, preprocessor,
  visibility, or codegen-option changes invalidate affected objects.
- A link-only option change recompiles zero translation units and relinks.
- Source timestamp changes with identical content cause no miss.
- An installed `PATH` backend performs ordinary uncached compilation and reads
  or publishes no persistent object entry.
- Generated-header discovery rejects missing, ambiguous, and escaping generated
  includes; a guarded cycle is traversed once and every participating header is
  included in the key.
- System headers are covered by backend/profile identity and are not hashed
  from host paths.
- Two processes requesting one missing key produce exactly one compilation;
  the waiter rechecks and reports `waited-then-hit`.
- Different missing keys may compile concurrently.
- A compiler failure, process failure, cancellation, or interruption publishes
  no committed entry.
- Missing object/metadata halves and invalid metadata, length, or digest rebuild
  once and never count as hits.
- A second validation failure reports a cache/filesystem error without looping.
- Published object and metadata files are immutable.
- Link failure retains valid object entries.
- Hit/miss/concurrent completion order never changes final link ordering.
- Deleting `build/.hexal/` completely resets the cache.
- Ordinary compiler tests remain filesystem-, cache-, and toolchain-free.
- `docs/reference.md` is unchanged.

## Implementation plan

### Phase 1: key and dependency model

1. Define the versioned, length-delimited key encoder.
2. Add the generated quoted-include closure walker over
   `CompilationResult.Files`.
3. Normalize material compile options separately from link-only options.
4. Add deterministic key and precise invalidation unit tests.

### Phase 2: storage and integrity

1. Add the project-local `cache/v1` layout.
2. Define metadata encoding with deterministic field order.
3. Implement complete-entry validation and corruption classification.
4. Implement temporary sibling writes and metadata-last atomic publication.

### Phase 3: concurrent publication

1. Add the native Windows per-key file lock.
2. Recheck after lock acquisition.
3. Compile one publisher per key while permitting different-key concurrency.
4. Add same-key and different-key process-level concurrency tests.

### Phase 4: driver integration

1. Compute keys after RFC 0052/0055 resolve backend, profile, and commands.
2. Route hits directly to deterministic link inputs.
3. Compile and publish misses through the existing translation-unit stage.
4. Record cache outcomes without fabricating command executions.
5. Retain cache entries across link failures and staging cleanup.

### Phase 5: conformance

1. Implement every Validation item.
2. Run pure-Go tests without invoking Zig where a fake backend suffices.
3. Run the external Windows gate for real-object hit/miss equivalence.
4. Verify clean, cached, and mixed builds produce equivalent executables.
5. Confirm `docs/reference.md` needs no change.

## Deferred work

- Explicit cache-clean CLI command.
- Size accounting and eviction.
- User-wide or shared cache.
- Remote cache protocol and trust.
- Foreign C dependency discovery.
- Incremental Hexal compilation.
