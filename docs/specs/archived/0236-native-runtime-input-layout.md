# RFC 0236: Native Runtime Input Layout

- Kind: Feature Specification (Rust-Style RFC)
- Status: Proposed
- Created: 2026-09-22
- Updated: 2026-09-22
- Origin: the checked-in native inputs under `lib/` duplicate every dependency's
  headers and licenses once per target profile, although only the static archive
  actually differs between profiles. RFC 0213 and RFC 0214 own the
  target-qualified static runtime pack this restructures
- Depends on: the current `lib/` pack contract (embedded pack,
  `<target-profile>/manifest.json`, `include_root`, `archive`, `license_file`,
  `files`), RFC 0055 (build and runtime-pack inputs), and the current
  target-profile matrix
- Does not update: `docs/reference.md`. This changes where the compiler's own
  native inputs live, not the language, so no language rule moves. It also does
  not touch `modules/`, which is out of scope

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

## Migration

1. `git mv` each `lib/<profile>/<lib>_<version>/include` and license files to
   `lib/<lib>_<version>/`, and each archive to
   `lib/<lib>_<version>/static/<profile>/<lib>.a`.
2. Rewrite both `manifest.json` files' `dependencies[]` paths and `files` keys to
   the `lib/`-relative form; digests do not change, because no file's bytes
   change.
3. Update `lib/pack.go`'s embed set and `internal/driver/runpack.go`'s root
   resolution and validation.
4. Rewrite the per-dependency sections of `lib/BUILD.md` to record the archive
   path under `static/<profile>/`, and state that headers and licenses are
   shared across profiles rather than copied per profile.

## Validation

This section is exhaustive.

- Every shipped profile's manifest validates under the `lib/`-rooted contract:
  dependencies in the required order, `include_root`, `archive`, and
  `license_file` sharing one `<lib>_<version>/` prefix, and every `files` key
  well formed.
- Every declared path resolves from the single embedded root, and every shipped
  byte matches its recorded digest.
- The driver rejects each of: a missing file, a digest mismatch, a path that
  escapes `lib/`, a repeated path, and a dependency whose three paths do not
  share one `<lib>_<version>/` prefix.
- No include file and no license file appears in more than one profile
  directory: `find lib -name uv.h` and its equivalents report exactly one hit
  per profile-independent file.
- Only the static archive differs between the two profiles for each dependency:
  the include trees and licenses compare byte-identical across profiles.
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
