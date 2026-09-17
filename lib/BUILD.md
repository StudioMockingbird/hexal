# Windows native runtime libraries

These archives are the checked-in `x86_64-windows-gnu-ucrt` runtime pack: the
prebuilt static inputs the build driver consumes instead of rebuilding libuv
and mimalloc. `manifest.json` records its layout, hashes, and system libraries;
`lib/` is the repository source of truth and a distribution copies it
byte-for-byte.

The matching public headers are checked in beside each archive:

```text
lib/
  BUILD.md
  x86_64-windows-gnu-ucrt/
    manifest.json
    mimalloc_v3.5.1/
      mimalloc.a
      include/
      LICENSE
    libuv_v1.52.1/
      libuv.a
      include/
      LICENSE
```

Each library's headers and archive come from the same pinned source revision
below and must be consumed as a pair. The libuv directory also carries
`LICENSE-docs` and `LICENSE-extra`; every regular file is listed in the
manifest.

## Build identity

- Hexal target profile: `x86_64-windows-gnu-ucrt`
- Zig toolchain target: `x86_64-windows-gnu`
- CRT: UCRT through the bundled MinGW-w64 runtime
- Host: Windows x86-64
- Toolchain: Zig 0.16.0 (`zig cc`, Clang 21.1.0, LLD)
- MinGW-w64: the revision bundled by Zig 0.16.0
- Optimization: `-O3 -DNDEBUG`
- CRT policy: dynamic UCRT system libraries; third-party code is archived
  statically
- CPU policy: target-portable x86-64; no `-march=native`

The two archives must be rebuilt together if the target, CRT, toolchain, or
optimization policy changes. Do not mix GNU/UCRT archives with MSVC/MSVCRT
archives.

## mimalloc_v3.5.1

- Source: `modules/mimalloc` at commit
  `34fbd7e7cd4627424490afe19b20f8066bfc537d` (`v3.5.1`)
- Source file: `src/static.c`
- Public headers: `mimalloc_v3.5.1/include/` (11 files, copied unchanged)
- Compile command:

```text
zig cc -target x86_64-windows-gnu -std=c11 -O3 -DNDEBUG
  -DMI_BUILD_RELEASE -DMI_WIN_INIT_USE_RAW_DLLMAIN
  -I modules/mimalloc/include -c modules/mimalloc/src/static.c
  -o mimalloc.o
```

- Archive: `mimalloc_v3.5.1/mimalloc.a`
- Size: `1410684` bytes
- SHA-256: `10af4abf7966f1ba45b7526b3342ac1d8298d4005c12fcba074d5b75ecc35661`

## libuv_v1.52.1

- Source: `modules/libuv` at commit
  `1cfa32ff59c076ffb6ed735bbc8c18361558661f` (`v1.52.1`)
- C dialect: C11
- Compile definitions: `WIN32_LEAN_AND_MEAN`, `_WIN32_WINNT=0x0A00`,
  `_CRT_DECLARE_NONSTDC_NAMES=0`, `_CRT_SECURE_NO_WARNINGS`
- Compile option: `-fno-strict-aliasing`
- Source files: the 37 Windows sources listed in `modules/LIBUV.md`
- Public headers: `libuv_v1.52.1/include/` (14 files, copied unchanged)
- Archive: `libuv_v1.52.1/libuv.a`
- Size: `1668222` bytes
- SHA-256: `06b935291a98551dab37264ef770b642dafc2f927bbecd464cece5ebdb5d9a03`

The archive was validated by compiling and running a native probe that calls
`mi_malloc`, `mi_free`, and `uv_version` while linking both archives and the
required Windows libraries: `psapi`, `shell32`, `user32`, `advapi32`,
`bcrypt`, `iphlpapi`, `userenv`, `ws2_32`, `dbghelp`, and `ole32`.

## Reproducibility

The archives were created with `zig ar rcs` from a clean object directory.
The exact source commits, toolchain, target, compile definitions, source list,
archive sizes, and digests above are the build record. Scratch objects and the
probe are not part of the repository.
