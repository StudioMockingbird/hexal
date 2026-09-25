# Native runtime library build records

This file is the build record for every checked-in runtime pack. Two exist:
`x86_64-linux-gnu`, which the driver embeds and selects, and
`x86_64-windows-gnu-ucrt`, which is complete and checked in but has no driver
in this release. Each pack's record names the exact sources, toolchain, compile
commands, sizes, and digests that produced its bytes.

## Linux GNU pack

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
    utf8proc_v2.11.3/
      utf8proc.a
      include/utf8proc.h
      LICENSE.md
```

Pack production is an explicit maintainer operation, never compiler setup, a
test, or a user build. The archives are produced once for the qualified target
below and checked in; ordinary builds only materialize demanded entries from
the embedded copy.

### Linux build identity

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

The three archives must be rebuilt together if the target, libc baseline,
toolchain, or optimization policy changes.

### Linux mimalloc_v3.5.1

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

### Linux libuv_v1.52.1

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

### Linux utf8proc_v2.11.3

- Source: `modules/utf8proc` at commit
  `e5e799221b45bbb90f5fdc5c69b6b8dfbf017e78` (`v2.11.3`)
- Unicode data version: 17.0.0
- Source file: `utf8proc.c` (one translation unit; it includes the generated
  `utf8proc_data.c`)
- Public header: `utf8proc_v2.11.3/include/utf8proc.h` (copied unchanged)
- Compile command:

```text
clang -std=c11 -O2 -DNDEBUG -fPIC -pthread -DUTF8PROC_STATIC \
  -I modules/utf8proc -c modules/utf8proc/utf8proc.c -o utf8proc.o
ar rcs utf8proc_v2.11.3/utf8proc.a utf8proc.o
```

- Size: `352378` bytes
- SHA-256: `a3ced9efe7b33d143abd353c85dbd1fc7a2fd94f1e0ab0273fe19742db70a4f6`

### Linux system libraries

`manifest.json` declares `pthread`, `dl`, and `rt` on the libuv dependency, in
that order, and none on mimalloc. They are the union the native combined probe
required; on glibc 2.44 all three resolve from the C library itself, so the
declaration is retained as the target's portable link set rather than a
measured necessity on this host.

### Linux verification

The combined probe compiles, links, and runs one program using all three
archives:

```text
clang -std=c23 -D_POSIX_C_SOURCE=200809L combine.c \
  -I lib/x86_64-linux-gnu/libuv_v1.52.1/include \
  -I lib/x86_64-linux-gnu/mimalloc_v3.5.1/include \
  -I lib/x86_64-linux-gnu/utf8proc_v2.11.3/include \
  lib/x86_64-linux-gnu/libuv_v1.52.1/libuv.a \
  lib/x86_64-linux-gnu/mimalloc_v3.5.1/mimalloc.a \
  lib/x86_64-linux-gnu/utf8proc_v2.11.3/utf8proc.a \
  -lpthread -ldl -lrt -o combine
./combine
```

It calls `mi_malloc`/`mi_free`, `uv_version`, and `utf8proc_version`, and exits
0. `hexal doctor` performs the same combined-archive probe plus full manifest
verification.

## Windows GNU/UCRT pack

`lib/x86_64-windows-gnu-ucrt/` holds a complete three-archive pack: libuv,
mimalloc, and utf8proc, each with its public headers and upstream licenses. It
is checked in but is not embedded or selected by the current driver, which
qualifies one Linux pair; these records qualify the dependency payloads
independently of the driver-selection work.

All three archives were rebuilt from source with installed Clang on 2026-09-24
and share one build identity. They previously did not: libuv and mimalloc were
Zig 0.16.0 artifacts from the era of the Zig-powered Windows driver, and
utf8proc was a later Clang 22.1.8 build, so the pack mixed two toolchains. RFC
0217 removed Zig from the project, which left the Zig-built records describing a
procedure nobody could re-run. Rebuilding retires that split; no Zig artifact
remains in the pack.

Every size and digest in this section was verified against the checked-in bytes
after the rebuild, and every entry in `manifest.json` was re-verified in the
same pass.

### Windows build identity

- Hexal target profile: `x86_64-windows-gnu-ucrt`
- Clang toolchain target triple: `x86_64-w64-windows-gnu`
- Host and probe environment: x86-64 Windows, MinGW-w64/UCRT
- Producer compiler: Clang 23.1.2
  (`llvm-project` `85ac560262434c9ccfc0c183ec22d4138ed647fb`)
- Archiver: LLVM `llvm-ar` 23.1.2
- Optimization: `-O2 -DNDEBUG`
- CRT policy: dynamic UCRT system libraries; third-party code is archived
  statically
- CPU policy: target-portable x86-64; no `-march=native`

`-fPIC` is deliberately absent, unlike the Linux pack. Windows PE code is
position-independent by construction and Clang treats the flag as unused for
this target, so carrying it would record a flag that does nothing.

The three archives must be rebuilt together if the target, CRT, toolchain, or
optimization policy changes. Do not mix GNU/UCRT archives with MSVC/MSVCRT
archives.

### Windows utf8proc_v2.11.3

- Source: `modules/utf8proc` at commit
  `e5e799221b45bbb90f5fdc5c69b6b8dfbf017e78` (`v2.11.3`)
- Release archive: `https://github.com/JuliaStrings/utf8proc/archive/refs/tags/v2.11.3.tar.gz`
- Release archive size: `202535` bytes
- Release archive SHA-256:
  `abfed50b6d4da51345713661370290f4f4747263ee73dc90356299dfc7990c78`
- Unicode data: 17.0.0
- Source files: `utf8proc.c`; it includes the generated `utf8proc_data.c`
- Public header: `utf8proc_v2.11.3/include/utf8proc.h` (copied unchanged)
- License: `utf8proc_v2.11.3/LICENSE.md` (MIT/Expat and Unicode data terms)
- Compile command:

```text
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 -DNDEBUG \
  -DUTF8PROC_STATIC -I modules/utf8proc \
  -c modules/utf8proc/utf8proc.c -o utf8proc.o
llvm-ar rcs utf8proc_v2.11.3/utf8proc.a utf8proc.o
```

- Archive size: `349994` bytes
- Archive SHA-256:
  `b9517c1164c81ecdf10b728d19e19d87a99793b36418acd880b8a709ec820455`
- Header SHA-256:
  `a4e498b7392c383cf3b22e662da21e0b48f1264806235d87b5d8bd166232f658`
- License SHA-256:
  `3b510150d34f248a221bb88e1d811238d6c6c18b51231822c42974c39bb07256`

### Windows mimalloc_v3.5.1

- Source: `modules/mimalloc` at commit
  `34fbd7e7cd4627424490afe19b20f8066bfc537d` (`v3.5.1`)
- Source file: `src/static.c`
- Public headers: `mimalloc_v3.5.1/include/` (11 files, copied unchanged)
- Compile command:

```text
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 -DNDEBUG \
  -DMI_BUILD_RELEASE -DMI_WIN_INIT_USE_RAW_DLLMAIN \
  -I modules/mimalloc/include -c modules/mimalloc/src/static.c -o mimalloc.o
llvm-ar rcs mimalloc_v3.5.1/mimalloc.a mimalloc.o
```

- Archive: `mimalloc_v3.5.1/mimalloc.a`
- Size: `285880` bytes
- SHA-256: `ab6ed25b0a02bb5b0250484aae80cbcf6c38a4e293e6256f222614c72f722e9c`

### Windows libuv_v1.52.1

- Source: `modules/libuv` at commit
  `1cfa32ff59c076ffb6ed735bbc8c18361558661f` (`v1.52.1`)
- C dialect: C11
- Compile definitions: `WIN32_LEAN_AND_MEAN`, `_WIN32_WINNT=0x0A00`,
  `_CRT_DECLARE_NONSTDC_NAMES=0`, `_CRT_SECURE_NO_WARNINGS`
- Compile option: `-fno-strict-aliasing`
- Source files: the 37 Windows sources listed in `modules/LIBUV.md`
- Public headers: `libuv_v1.52.1/include/` (14 files, copied unchanged)
- Compile command (once per source):

```text
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 -DNDEBUG -fno-strict-aliasing \
  -DWIN32_LEAN_AND_MEAN -D_WIN32_WINNT=0x0A00 \
  -D_CRT_DECLARE_NONSTDC_NAMES=0 -D_CRT_SECURE_NO_WARNINGS \
  -I modules/libuv/include -I modules/libuv/src -c <source> -o <object>
llvm-ar rcs libuv_v1.52.1/libuv.a <objects>
```

- Archive: `libuv_v1.52.1/libuv.a`
- Size: `354654` bytes
- SHA-256: `ec5d328e445afb9cb7f8800214a9df699bc0c1b2cb78d4a047657d887303d027`

Two upstream libuv sources emit const-qualifier warnings under this Clang
(`uv-common.c` and `win/util.c`, around `cpu_info->model`). They are upstream
code and are not patched; the build does not use `-Werror` for third-party
sources.

### Windows verification

The combined probe compiles, links, and runs one program against all three
archives, mirroring how a generated Hexal program consumes them:

```text
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 probe.c -DUTF8PROC_STATIC \
  -I modules/mimalloc/include -I modules/utf8proc -I modules/libuv/include \
  libuv_v1.52.1/libuv.a mimalloc_v3.5.1/mimalloc.a utf8proc_v2.11.3/utf8proc.a \
  -lpsapi -lshell32 -luser32 -ladvapi32 -lbcrypt -liphlpapi -luserenv \
  -lws2_32 -ldbghelp -lole32 -o probe.exe
```

It calls `mi_malloc`/`mi_free`, hands libuv the mimalloc allocator with
`uv_replace_allocator` exactly as the generated scheduler bootstrap does, runs
a real loop through `uv_loop_init`/`uv_run`/`uv_loop_close`, exercises a
Windows syscall path with `uv_exepath`, and decodes U+1F600 with
`utf8proc_iterate`. It exits 0 and prints
`pack-ok uv=1.52.1 mi=30501 utf8proc=2.11.3`, which also confirms each archive
carries the pinned upstream version.

Those ten system libraries are the Windows counterpart of the Linux pack's
`pthread`, `dl`, and `rt`, and the driver must declare the same link set when
the Windows lane is re-qualified.

### Windows reproducibility

All three archives were created with `llvm-ar rcs` from a clean object
directory, by one script, in one pass. The exact source commits, toolchain,
target, compile definitions, source lists, archive sizes, and digests above are
the build record. Scratch objects, the build script, and the probe are not part
of the repository.

The three module sources were confirmed to be at their pinned commits before
the build: `modules/libuv` at `1cfa32ff`, `modules/mimalloc` at `34fbd7e7`,
`modules/utf8proc` at `e5e79922`. Headers and licenses were not regenerated,
because the sources did not move; only the archives changed.
