# Linux native runtime libraries

These archives are the checked-in `x86_64-linux-gnu` runtime pack: the prebuilt
static inputs the driver consumes instead of rebuilding libuv and mimalloc.
`manifest.json` records the closed layout, every file's SHA-256, the runtime
ABI, and the target system libraries; `lib/` is the repository source of truth
and the Go `lib` package embeds `x86_64-linux-gnu/` into `bin/hexal`.

The matching public headers are checked in beside each archive:

```text
lib/
  BUILD.md
  x86_64-linux-gnu/
    manifest.json
    libuv_v1.52.1/
      libuv.a
      include/
      LICENSE
      LICENSE-docs
      LICENSE-extra
    mimalloc_v3.5.1/
      mimalloc.a
      include/
      LICENSE
```

Pack production is an explicit maintainer operation, never compiler setup, a
test, or a user build. The archives are produced once for the qualified target
below and checked in; ordinary builds only materialize demanded entries from
the embedded copy.

## Build identity

- Hexal target profile: `x86_64-linux-gnu`
- Clang toolchain target triple: `x86_64-linux-gnu`
- Host and probe environment: x86-64 Linux (WSL), glibc 2.44
- Producer compiler: Clang 23.1.1
- Archiver: GNU ar (binutils) 2.45.0
- Optimization: `-O2 -DNDEBUG -fPIC -pthread`
- CPU policy: target-portable x86-64; no `-march=native`
- Defined baseline: glibc 2.31 or newer

`-fPIC` is required because generated executables link position-independent by
default; an archive of non-PIC objects fails the link.

The two archives must be rebuilt together if the target, libc baseline,
toolchain, or optimization policy changes.

## mimalloc_v3.5.1

- Source: `modules/mimalloc` at commit
  `34fbd7e7cd4627424490afe19b20f8066bfc537d` (`v3.5.1`)
- Source file: `src/static.c` (single amalgamated translation unit)
- Public headers: `mimalloc_v3.5.1/include/` (copied unchanged)
- Compile command:

```text
clang -std=c11 -O2 -DNDEBUG -fPIC -pthread -DMI_BUILD_RELEASE \
  -I modules/mimalloc/include -c modules/mimalloc/src/static.c -o mimalloc.o
ar rcs mimalloc_v3.5.1/mimalloc.a mimalloc.o
```

- Size: `348216` bytes
- SHA-256: `18ae1e534ebfd4f83141526ca9bbb392aed919f0cc0d359448ee98441d336fb6`

## libuv_v1.52.1

- Source: `modules/libuv` at commit
  `1cfa32ff59c076ffb6ed735bbc8c18361558661f` (`v1.52.1`)
- C dialect: C11
- Compile definitions: `_FILE_OFFSET_BITS=64`, `_LARGEFILE_SOURCE`,
  `_GNU_SOURCE`, `_POSIX_C_SOURCE=200112`
- Source files (the upstream CMake Linux list): the 12 `src/*.c` base files,
  the 18 `src/unix/*.c` common files, `src/unix/proctitle.c`,
  `src/unix/linux.c`, `src/unix/procfs-exepath.c`,
  `src/unix/random-getrandom.c`, and `src/unix/random-sysctl-linux.c`
- Public headers: `libuv_v1.52.1/include/` (copied unchanged)
- Compile command (once per source):

```text
clang -std=c11 -O2 -DNDEBUG -fPIC -pthread \
  -D_FILE_OFFSET_BITS=64 -D_LARGEFILE_SOURCE -D_GNU_SOURCE -D_POSIX_C_SOURCE=200112 \
  -I modules/libuv/include -I modules/libuv/src -c <source> -o <object>
ar rcs libuv_v1.52.1/libuv.a <objects>
```

- Size: `335006` bytes
- SHA-256: `4c72c7508907d1bb72c8247f645fdf9ebe04cda81790080a390bf3117be676a1`

## System libraries

`manifest.json` declares `pthread`, `dl`, and `rt` on the libuv dependency, in
that order, and none on mimalloc. They are the union the native combined probe
required; on glibc 2.44 all three resolve from the C library itself, so the
declaration is retained as the target's portable link set rather than a
measured necessity on this host.

## Verification

The combined probe compiles, links, and runs one program using both archives:

```text
clang -std=c23 -D_POSIX_C_SOURCE=200809L combine.c \
  -I lib/x86_64-linux-gnu/libuv_v1.52.1/include \
  -I lib/x86_64-linux-gnu/mimalloc_v3.5.1/include \
  lib/x86_64-linux-gnu/libuv_v1.52.1/libuv.a \
  lib/x86_64-linux-gnu/mimalloc_v3.5.1/mimalloc.a \
  -lpthread -ldl -lrt -o combine
./combine
```

It calls `mi_malloc`/`mi_free` and `uv_version`, and exits 0. `hexal doctor`
performs the same combined-archive probe plus full manifest verification.

## Retired Windows pack

`lib/x86_64-windows-gnu-ucrt/` is the previous Zig/MinGW runtime pack. It is no
longer embedded, selected, validated, or linked by the driver: this release
qualifies no native Windows build path, and `x86_64-windows-gnu-ucrt` remains
only a core C-generation target. A later Windows-focused specification owns
either requalification with Clang/MinGW-w64/UCRT or removal of these bytes.
