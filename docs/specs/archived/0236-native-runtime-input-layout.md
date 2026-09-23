# RFC 0236: Native Runtime Input Layout

- Kind: Feature Specification (Rust-Style RFC)
- Status: Discarded, 2026-09-23, without implementation. The current
  per-profile layout stays: `lib/<target-profile>/<lib>_<version>/` with
  `include/`, the license files, and `<lib>.a` under each profile, the manifest
  at `lib/<target-profile>/manifest.json`, paths relative to that profile
  directory, and `lib/pack.go`'s single `//go:embed x86_64-linux-gnu`. Nothing
  in `lib/`, `internal/driver/runpack.go`, or the pack test fixtures changes,
  and RFC 0233 and RFC 0234 add their dependencies under the shape they already
  specify. Retained as the record of why. See Why this was discarded
- Created: 2026-09-22
- Updated: 2026-09-23
- Origin: the checked-in native inputs under `lib/` duplicate every dependency's
  headers and licenses once per target profile, although only the static archive
  actually differs between profiles. Closed RFC 0213 established the
  target-qualified static runtime pack and implemented RFC 0217 owns the current
  embed and backend path this restructures. RFC 0214 is Discarded and
  consolidated into RFC 0217; it is not an owner
- Depends on: the current `lib/` pack contract (embedded pack,
  `<target-profile>/manifest.json`, `include_root`, `archive`, `license_file`,
  `files`), ADR 0055 (build and runtime-pack inputs), and the current
  target-profile matrix
- Supersedes: nothing. This was discarded, so the per-profile pack layout
  recorded in closed RFC 0213, RFC 0217, and RFC 0227 remains current, and RFC
  0233 and RFC 0234 stand as written
- Does not update: `docs/reference.md`. This changes where the compiler's own
  native inputs live, not the language, so no language rule moves. It also does
  not touch `modules/`, which is out of scope

## Why this was discarded

The proposal below is sound and its premises were verified: the include trees
and license files are byte-identical across both shipped profiles, and only the
archives differ. The duplication is real. The benefit of removing it is not.

- **Git already stores it once.** The duplicated files are byte-identical, so
  they are one blob in the object store. The repository does not shrink.
- **The binary already carries one copy.** `lib/pack.go` embeds a single
  profile, so no shipped artifact contains the duplication in the first place.
  The binary does not shrink either.
- **The working-tree cost is about 548 KB** across 31 files, against a 5.5 MB
  `lib/`. That is the entire measured saving.

Against that: two manifests rewritten key by key, `lib/pack.go`'s one embed
pattern replaced by an enumerated set that must be edited whenever a dependency
or shipped profile changes, `runpack.go`'s root resolution, walk exemption, and
one diagnostic string, every synthetic fixture in `runpack_test.go`,
`c23validation/dependencies_test.go`'s own pack rooting, and `lib/BUILD.md`.
The embed set is the sharp edge: `go:embed` cannot exclude, so a directory-level
pattern would silently ship a non-shipped profile's archives.

The one property worth keeping from this proposal is **drift detection** —
nothing today would notice if a header diverged between profiles. That is a
test, not a layout: compare each dependency's include tree and license files
across profiles and fail on a difference. It costs about twenty lines, needs no
migration, and does not block RFC 0233. If that check is wanted, it belongs in a
spec of its own and not in a restructure.

Everything below is the discarded proposal, kept so the measurement and the
sequencing argument do not have to be redone.

## Decision summary

Hoist each native dependency above the target-profile directory. A dependency's
versioned directory becomes the unit that owns its shared headers and licenses,
and its per-target archives nest beneath it:

```text
lib/
  libuv_v1.52.1/
    include/                      shared: every target, one copy
    LICENSE  LICENSE-docs  LICENSE-extra
    static/
      x86_64-linux-gnu/libuv.a
      x86_64-windows-gnu-ucrt/libuv.a
  mimalloc_v3.5.1/
    include/
    LICENSE
    static/x86_64-linux-gnu/mimalloc.a
    static/x86_64-windows-gnu-ucrt/mimalloc.a
  utf8proc_v2.11.3/
    include/
    LICENSE.md
    static/x86_64-linux-gnu/utf8proc.a
    static/x86_64-windows-gnu-ucrt/utf8proc.a
  x86_64-linux-gnu/manifest.json
  x86_64-windows-gnu-ucrt/manifest.json
  BUILD.md  pack.go  accessor.go
```

Each include tree and each license file exists exactly once. Only the archive is
per target, which is the only input that is actually target-qualified.

## Layout rules

- A dependency directory is named `<lib>_<version>`, exactly as today.
- `lib/<lib>_<version>/include/` holds the upstream public headers, copied
  unchanged, shared by every target profile.
- `lib/<lib>_<version>/` holds every license file the dependency ships, at the
  dependency root rather than under a target directory.
- `lib/<lib>_<version>/static/<target-profile>/<lib>.a` holds that profile's
  static archive. The archive keeps the dependency's own file name under a
  profile directory, so the path is uniform and the profile is explicit.
- `lib/<target-profile>/manifest.json` keeps its role and location: one pack
  identity per profile, naming the exact dependencies, archives, include roots,
  licenses, system libraries, and file digests that profile ships.

### Shared headers are a constraint, not an observation

This layout can only express a dependency whose public headers are identical on
every target profile. That holds for libuv, mimalloc, and utf8proc today, and it
is a rule this RFC imposes going forward, not a fact it happens to record: a
dependency that generates a per-target `config.h` has no home here.

A future dependency that needs per-target headers must either have that header
generated into the compiler's own emitted C instead of the pack, or this layout
must grow a per-profile `include_overrides/<target-profile>/` beside the shared
tree. Neither is specified here. Qualifying a new dependency includes checking
its headers are profile-independent, and a dependency that fails that check
stops for a layout decision rather than being bent into this shape.

## Manifest contract

Every path a manifest names is relative to the `lib/` root, not to the profile
directory:

```json
{
  "name": "utf8proc",
  "include_root": "utf8proc_v2.11.3/include",
  "archive": "utf8proc_v2.11.3/static/x86_64-linux-gnu/utf8proc.a",
  "license_file": "utf8proc_v2.11.3/LICENSE.md",
  "system_libraries": []
}
```

- `include_root`, `archive`, and `license_file` continue to share one
  `<lib>_<version>/` prefix; that invariant is what ties a dependency's parts
  together and it is unchanged.
- `files` keys use the same `lib/`-relative form and cover every shipped byte of
  the dependency: the shared headers, the shared licenses, and this profile's
  archive.
- The profile identity stays on the manifest (`target_profile`), not on the
  paths of shared inputs, so two profiles' manifests name the same
  `include_root` and different `archive` paths.
- The schema's field names do not change; `format_version` does not change.

## Embedding and resolution

- The embedded filesystem is rooted at `lib/` and carries the shipped profile's
  manifest, every dependency's `include/` and license files, and the shipped
  profile's archives only. Non-shipped profiles stay in the tree, unembedded,
  exactly as the shipped-pack rule already requires.
- The driver resolves every manifest path against that one root. The
  per-profile `fs.Sub` indirection is removed: the manifest is read at
  `<target-profile>/manifest.json` and every payload path is read as written.
- Materialization is unchanged in shape: each demanded dependency's include tree
  and archive are copied into the driver's private staging directory, its
  license files are verified, and its system libraries are appended in manifest
  order.

### The embed set is enumerated, because `go:embed` cannot exclude

`go:embed` has no negation, so a directory-level pattern is wrong here: embedding
`libuv_v1.52.1` would pull in every profile's archive under `static/`, breaking
the shipped-profile rule silently and growing the binary by the non-shipped
archives. The shipped profile's set is therefore written out, one pattern per
shared tree, per license, and per shipped archive directory:

```go
//go:embed x86_64-linux-gnu/manifest.json
//go:embed libuv_v1.52.1/include libuv_v1.52.1/LICENSE libuv_v1.52.1/LICENSE-docs libuv_v1.52.1/LICENSE-extra
//go:embed libuv_v1.52.1/static/x86_64-linux-gnu
//go:embed mimalloc_v3.5.1/include mimalloc_v3.5.1/LICENSE
//go:embed mimalloc_v3.5.1/static/x86_64-linux-gnu
//go:embed utf8proc_v2.11.3/include utf8proc_v2.11.3/LICENSE.md
//go:embed utf8proc_v2.11.3/static/x86_64-linux-gnu
var runtimePacks embed.FS
```

This is a real cost the old layout did not carry: today the whole shipped pack is
one pattern, `//go:embed x86_64-linux-gnu`, and adding a dependency needs no edit
to `lib/pack.go`. Under this layout, adding a dependency adds two patterns and
changing the shipped profile rewrites every `static/` pattern. The compensating
guarantee is the Validation item below: an embed set that omits a manifest-listed
path fails `hexal doctor` on the first run, and one that includes an unlisted
path fails the same walk, so neither mistake is silent.

### Driver changes this restructure forces

- `verifyRuntimePack`'s completeness walk exempts exactly one path today,
  `manifest.json` at the filesystem root. The shipped manifest now lives at
  `<target-profile>/manifest.json`, so the exemption moves with it. Left
  unchanged, the walk reports the shipped manifest as a file "not listed in the
  manifest" and `hexal doctor` fails on the first run after migration.
- `validPackPath`'s rejection reads "escapes the target directory". The bounding
  root is now the pack root, so the message and the test asserting it
  (`TestManifestEscapingPathRejected`) both move to "escapes the pack root". The
  rule itself — no `.`, `..`, backslash, absolute form, or drive letter — is
  unchanged.
- `versionedPrefix` is unchanged and still decides the shared-prefix rule:
  `<lib>_<version>/static/<profile>/<lib>.a` keeps the same `<lib>_<version>/`
  first component as the include root and license path.
- `packInputs.ManifestDigest` is SHA-256 of the manifest's exact bytes and feeds
  `buildIdentity`. The manifest's bytes change, so every cached build
  invalidates once. No generated byte and no linker argument changes; only the
  cache key does.

## Sequencing (moot)

This was the argument that the restructure had to happen before RFC 0233
(yyjson, Implementation ready) and RFC 0234 (PCRE2), because each adds one
dependency under the per-profile shape and migrating later would cost five
dependencies instead of three. The conclusion drawn was "now or never." Never
was chosen: 0233 and 0234 land as written, and no dependency migrates.

## Migration (not performed)

1. `git mv` each `lib/<profile>/<lib>_<version>/include` and license files to
   `lib/<lib>_<version>/`, and each archive to
   `lib/<lib>_<version>/static/<profile>/<lib>.a`.
2. Rewrite both `manifest.json` files' `dependencies[]` paths and `files` keys to
   the `lib/`-relative form. No payload digest changes, because no payload
   file's bytes change; the risk the Validation section covers is a mispaired
   key and digest, not a stale hash. `format_version` and `runtime_abi_version`
   are untouched.
3. Replace `lib/pack.go`'s single embed pattern with the enumerated set above,
   and update `lib/accessor.go`'s doc comment, which currently documents the
   `fs.Sub` selection this RFC removes.
4. Update `internal/driver/runpack.go`: drop the `fs.Sub` in `runtimePackFS`,
   read the manifest at `<target-profile>/manifest.json`, move the
   `verifyRuntimePack` walk exemption to that path, and reword the
   `validPackPath` rejection to name the pack root.
5. Re-root the test fixtures that encode the current shape. They are not
   incidental; they are where the contract is asserted:
   - `internal/driver/runpack_test.go` builds synthetic packs with the manifest
     at the filesystem root and payload paths like `libuv_v1.52.1/libuv.a`.
     Every fixture moves to `<profile>/manifest.json` and
     `<lib>_<version>/static/<profile>/<lib>.a`, including the escaping-path,
     missing-file, corrupt-digest, and unlisted-file cases.
   - `compiler/tests/c23validation/dependencies_test.go` holds its own
     `packDirName` constant and a second `fs.Sub` into the profile directory.
     Both are removed in favour of the single root.
6. Rewrite the per-dependency sections of `lib/BUILD.md` to record the archive
   path under `static/<profile>/`, and state that headers and licenses are
   shared across profiles rather than copied per profile.
7. Close the provenance gap this move exposes. `lib/BUILD.md` is titled "Linux
   native runtime libraries" and its only Windows section records utf8proc, but
   the tree ships `x86_64-windows-gnu-ucrt` archives for libuv (1.67 MB) and
   mimalloc (1.41 MB) with no build identity, source commit, or compile command
   recorded. Hoisting makes each dependency's directory the unit that owns its
   record, so the missing Windows entries are written in the same change or
   explicitly deferred with a named owner.

## Validation (not performed)

This section was exhaustive for the proposal as designed. Nothing in it ran,
because the restructure was discarded before implementation.

- Every shipped profile's manifest validates under the `lib/`-rooted contract:
  dependencies in the required order, `include_root`, `archive`, and
  `license_file` sharing one `<lib>_<version>/` prefix, and every `files` key
  well formed.
- Every declared path resolves from the single embedded root, and every shipped
  byte matches its recorded digest.
- The driver rejects each of: a missing file, a digest mismatch, a path that
  escapes the pack root, a repeated path, and a dependency whose three paths do
  not share one `<lib>_<version>/` prefix.
- The shipped profile's manifest is not itself reported as an unlisted payload
  file: `hexal doctor` passes with the manifest at
  `<target-profile>/manifest.json`.
- Every shared header and license exists exactly once in the tree:
  `find lib -name uv.h` and its equivalents each report exactly one hit.
- The embed set is exact in both directions. A pattern omitted from
  `lib/pack.go` makes a manifest-listed path unreadable and fails verification;
  a pattern that admits a non-shipped profile's archive makes an unlisted file
  reachable and fails the completeness walk. Both are exercised.
- Before the move, every file about to be shared is proven identical across
  profiles: each dependency's include tree and license files compare
  byte-for-byte between `x86_64-linux-gnu` and `x86_64-windows-gnu-ucrt`, and
  only the archives differ. A mismatch stops the migration rather than silently
  electing one profile's copy as the shared one. After the move the property is
  structural — there is one copy — so this is a migration gate, not a standing
  test.
- `hexal doctor` passes for the shipped profile, and the archives it links are
  byte-identical to the ones linked before the restructure.
- The shipped profile embeds its own manifest, the shared include and license
  trees, and its own archives; a non-shipped profile's manifest and archives
  remain unembedded and are still present in the tree.
- Generated C, linker arguments, system-library order, and the snippet manifest
  are unchanged by the restructure.
- The compiler remains usable with no adjacent `lib/` tree: every native input it
  needs is embedded in the binary.

## Rejected alternatives

- **Keep one copy per profile and share nothing.** This is the status quo. It
  duplicates every header and license, and it hides the fact that only the
  archive is target-qualified, so a reviewer cannot tell which inputs are
  genuinely per-target.
- **Share the archive directory and separate the archive by file name only**
  (`static/libuv_x86_64-linux-gnu.a`). It makes the profile a suffix of a file
  name rather than a directory, so a profile cannot be added or dropped by
  moving one directory, and the manifest's `archive` paths no longer share a
  shape with anything else.
- **Keep `fs.Sub` per profile and point shared paths out of the subtree.** It
  requires `..` in a manifest path, which the pack-path validator exists to
  reject, and it would make the embedded filesystem's root ambiguous.
