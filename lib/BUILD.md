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
    yyjson_v0.13.0/
      yyjson.a
      include/yyjson.h
      LICENSE
    pcre2_v10.48/
      pcre2.a
      include/pcre2.h
      LICENSE
    llhttp_v9.4.3/
      llhttp.a
      include/llhttp.h
      LICENSE
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

### Linux yyjson_v0.13.0

- Source: `modules/yyjson` (git submodule) at commit
  `6447536015f3d600f3d65323b10976103b337ca7` (`0.13.0`, upstream release tag)
- Release: https://github.com/ibireme/yyjson/releases/tag/0.13.0
- Source file: `src/yyjson.c` (one translation unit), public header
  `src/yyjson.h`
- Public header: `yyjson_v0.13.0/include/yyjson.h` (copied unchanged)
- License: `yyjson_v0.13.0/LICENSE` (MIT)
- Compile definitions: `YYJSON_DISABLE_FILE=1`, `YYJSON_DISABLE_INCR_READER=1`,
  `YYJSON_DISABLE_UTILS=1` (all three are upstream compile-time switches);
  `YYJSON_FREESTANDING`, `YYJSON_DISABLE_NON_STANDARD`,
  `YYJSON_DISABLE_UTF8_VALIDATION` unset; reader and writer depth limits set:
- Depth limits: `YYJSON_READER_DEPTH_LIMIT=256`, `YYJSON_WRITER_DEPTH_LIMIT=256`
- Read flags (adapter): `YYJSON_READ_ALLOW_COMMENTS |
  YYJSON_READ_ALLOW_TRAILING_COMMAS`; write flags: defaults (compact)
- Compile command:

```text
clang --target=x86_64-linux-gnu -std=c11 -O2 -DNDEBUG -fPIC -pthread \
  -DYYJSON_DISABLE_FILE=1 -DYYJSON_DISABLE_INCR_READER=1 -DYYJSON_DISABLE_UTILS=1 \
  -DYYJSON_READER_DEPTH_LIMIT=256 -DYYJSON_WRITER_DEPTH_LIMIT=256 \
  -I modules/yyjson/src -c modules/yyjson/src/yyjson.c -o yyjson.o
ar rcs yyjson_v0.13.0/yyjson.a yyjson.o
```

- Size: `279086` bytes
- SHA-256: `b644a5ebb72230c72ecc2e1cca8345cb6042fcfe30d88d4011b6e4eebfeb4865`
- Header SHA-256: `c80cd7dc504f8c226c3e22adf4894e0161ee302bd85a2008f7ed7a620438e65b`
- License SHA-256: `7b14b8632bf3d5cb64c7a5f1ddfa9062e9c6eba38ac495a0897541d6658d3ad2`

### Linux pcre2_v10.48

- Source: `modules/pcre2` (git submodule) at commit
  `7978954dbd2efc6f2196869290553cf1871b4ce6` (`pcre2-10.48`, upstream release
  tag)
- Release: https://github.com/PCRE2Project/pcre2/releases/tag/pcre2-10.48
- 8-bit library only (`PCRE2_CODE_UNIT_WIDTH=8`); JIT compiled out
  (`SUPPORT_JIT` unset, so the `pcre2_jit_compile.c` stub section compiles and
  the sljit tree is not built)
- Compiled from the release's prepared manual-build inputs: `pcre2.h.generic`
  and `config.h.generic` (copied unchanged to the build staging as `pcre2.h`
  and `config.h`; produced not by patching the submodule) and
  `pcre2_chartables.c.dist` as the chartables unit. Compile-time switches:
  `SUPPORT_PCRE2_8`, `SUPPORT_UNICODE`; `SUPPORT_JIT`, 16/32-bit widths unset.
  Link size stays the default two-byte internal link (`LINK_SIZE 2` from
  `config.h.generic`); match limits are runtime context values, not build
  macros, and the adapter sets them from `compiler/config` at every call.
- Source files (31 translation units, the upstream libpcre2-8 list):
  `pcre2_auto_possess.c`, `pcre2_chkdint.c`, `pcre2_compile.c`,
  `pcre2_compile_cgroup.c`, `pcre2_compile_class.c`, `pcre2_config.c`,
  `pcre2_context.c`, `pcre2_convert.c`, `pcre2_dfa_match.c`, `pcre2_error.c`,
  `pcre2_extuni.c`, `pcre2_find_bracket.c`, `pcre2_jit_compile.c`,
  `pcre2_maketables.c`, `pcre2_match.c`, `pcre2_match_data.c`,
  `pcre2_match_next.c`, `pcre2_newline.c`, `pcre2_ord2utf.c`,
  `pcre2_pattern_info.c`, `pcre2_script_run.c`, `pcre2_serialize.c`,
  `pcre2_string_utils.c`, `pcre2_study.c`, `pcre2_substitute.c`,
  `pcre2_substring.c`, `pcre2_tables.c`, `pcre2_ucd.c`, `pcre2_valid_utf.c`,
  `pcre2_xclass.c`, and the chartables translation unit copied from
  `pcre2_chartables.c.dist`
- Public header: `pcre2_v10.48/include/pcre2.h` (the release's
  `pcre2.h.generic`, copied unchanged)
- License: `pcre2_v10.48/LICENSE` (the release's `LICENCE.md`, BSD-2-Clause
  AND BSD-3-Clause WITH PCRE2-exception)
- Compile command (once per source, plus the chartables unit):

```text
clang --target=x86_64-linux-gnu -std=c11 -O2 -DNDEBUG -fPIC -pthread \
  -DHAVE_CONFIG_H -DPCRE2_CODE_UNIT_WIDTH=8 -DSUPPORT_PCRE2_8 -DSUPPORT_UNICODE \
  -I <staging>/pcre2gen -I modules/pcre2/src -c <source> -o pcre2.o
ar rcs pcre2_v10.48/pcre2.a <objects>
```

- Size: `615746` bytes
- SHA-256: `5fb42b137c3d04e48bedbc0345c7de603f4ff85d9b069b4ef5d7293da1fa0991`
- Header SHA-256: `d59dad66a9e77e5ccffe35ad51c3ca6000ce9afe77a58fafa3d66b4845db9a61`
- License SHA-256: `4195c519dcfe4a4ffedc4b8ccc5d49e4dd02efd5ece6b69a4fba5d20080902a9`

### Linux llhttp_v9.4.3

- Source: `modules/llhttp` (git submodule) at commit
  `0e815792b167a9bd8ace259b95b7da953776c288` (`release/v9.4.3`, upstream release
  tag); the release branch ships the generated C, so no Node.js or TypeScript
  generator participates in the build
- Release: https://github.com/nodejs/llhttp/releases/tag/release%2Fv9.4.3
- Source files (three translation units): `src/api.c`, `src/http.c`,
  `src/llhttp.c`; public header `include/llhttp.h`
- Source digests, of the upstream blobs (LF), which the archive was compiled
  from rather than from an autocrlf working checkout: `src/api.c`
  `0f8590206fe2f264db2825401b5fb856b979ee1363dc88b06f36dc9577afb941`,
  `src/http.c`
  `a1f2b23168f8e9b5bfa464c89027d3ca259fc649ae3d7e72bfff997d1aeb5d72`,
  `src/llhttp.c`
  `391e7c99912abf3b1c9dd8a9c85fdeaac2663e9fa55b6f3871154d16fef8b892`
- Public header: `llhttp_v9.4.3/include/llhttp.h` (the release's, copied
  unchanged); only `hexal/http.c` includes it
- License: `llhttp_v9.4.3/LICENSE` (MIT)
- No system libraries and no allocator dependency: the caller owns every
  `llhttp_t`, and `llhttp_alloc`/`llhttp_free`, the only allocating entry
  points, are never called
- Compile command (once per source):

```text
clang --target=x86_64-linux-gnu -std=c11 -O2 -DNDEBUG -fPIC -pthread \
  -I modules/llhttp/include -c <source> -o llhttp.o
ar rcs llhttp_v9.4.3/llhttp.a <objects>
```

- Producer: Clang 23.1.1, GNU ar (binutils) 2.45.0
- Size: `115766` bytes
- SHA-256: `38ca8f7080aab8ab2efdcd73b1171c6e2bc6c4eb791e353fda7f261a93610543`
- Header SHA-256: `5bc82fa51b19aa8bee7d921038393fbfc74e8cc9bae0844c2d82c102fb8cfd68`
- License SHA-256: `628168d68bb5a8a17e0bbefb3bd74e326e1edc20583d76f52f457c82c921867d`

### Linux system libraries

`manifest.json` declares `pthread`, `dl`, and `rt` on the libuv dependency, in
that order, and none on mimalloc. They are the union the native combined probe
required; on glibc 2.44 all three resolve from the C library itself, so the
declaration is retained as the target's portable link set rather than a
measured necessity on this host.

### Linux verification

The combined probe compiles, links, and runs one program using all five
archives:

```text
clang -std=c23 -D_POSIX_C_SOURCE=200809L -DPCRE2_CODE_UNIT_WIDTH=8 combine.c \
  -I lib/x86_64-linux-gnu/libuv_v1.52.1/include \
  -I lib/x86_64-linux-gnu/mimalloc_v3.5.1/include \
  -I lib/x86_64-linux-gnu/utf8proc_v2.11.3/include \
  -I lib/x86_64-linux-gnu/yyjson_v0.13.0/include \
  -I lib/x86_64-linux-gnu/pcre2_v10.48/include \
  lib/x86_64-linux-gnu/libuv_v1.52.1/libuv.a \
  lib/x86_64-linux-gnu/mimalloc_v3.5.1/mimalloc.a \
  lib/x86_64-linux-gnu/utf8proc_v2.11.3/utf8proc.a \
  lib/x86_64-linux-gnu/yyjson_v0.13.0/yyjson.a \
  lib/x86_64-linux-gnu/pcre2_v10.48/pcre2.a \
  -lpthread -ldl -lrt -o combine
./combine
```

It calls `mi_malloc`/`mi_free`, `uv_version`, `utf8proc_version`,
`yyjson_version` (the release's version hex, 3328 for 0.13.0), and
`pcre2_config`'s compile plus one real `pcre2_compile`/`pcre2_match`, and exits
0. `hexal doctor` performs the same combined-archive probe plus full manifest
verification.

## Windows GNU/UCRT pack

`lib/x86_64-windows-gnu-ucrt/` holds a complete six-archive pack: libuv,
mimalloc, utf8proc, yyjson, PCRE2, and llhttp, each with its public headers and
upstream licenses. It is embedded in the compiler and `hexal doctor` can verify
it, while ordinary builds in this release select the Linux profile only.

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

### Windows yyjson_v0.13.0

- Source: `modules/yyjson` (git submodule) at commit
  `6447536015f3d600f3d65323b10976103b337ca7` (`0.13.0`)
- Release archive: `https://github.com/ibireme/yyjson/archive/refs/tags/0.13.0.tar.gz`
- Source file: `src/yyjson.c`; public header `src/yyjson.h`
- Public header: `yyjson_v0.13.0/include/yyjson.h` (copied unchanged)
- License: `yyjson_v0.13.0/LICENSE` (MIT)
- Compile definitions: `YYJSON_DISABLE_FILE=1`, `YYJSON_DISABLE_INCR_READER=1`,
  `YYJSON_DISABLE_UTILS=1`; depth limits `YYJSON_READER_DEPTH_LIMIT=256`,
  `YYJSON_WRITER_DEPTH_LIMIT=256`; `YYJSON_DISABLE_NON_STANDARD` and
  `YYJSON_DISABLE_UTF8_VALIDATION` unset
- Compile command:

```text
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 -DNDEBUG \
  -DYYJSON_DISABLE_FILE=1 -DYYJSON_DISABLE_INCR_READER=1 -DYYJSON_DISABLE_UTILS=1 \
  -DYYJSON_READER_DEPTH_LIMIT=256 -DYYJSON_WRITER_DEPTH_LIMIT=256 \
  -I modules/yyjson/src -c modules/yyjson/src/yyjson.c -o yyjson.o
llvm-ar rcs yyjson_v0.13.0/yyjson.a yyjson.o
```

- Archive size: `241580` bytes
- Archive SHA-256: `39606bc28ccace8a4aed0cb23b798c617f63b1ceb9901b8056288a70c3404d4f`
- Header SHA-256: `c80cd7dc504f8c226c3e22adf4894e0161ee302bd85a2008f7ed7a620438e65b`
- License SHA-256: `7b14b8632bf3d5cb64c7a5f1ddfa9062e9c6eba38ac495a0897541d6658d3ad2`

### Windows pcre2_v10.48

- Source: `modules/pcre2` (git submodule) at commit
  `7978954dbd2efc6f2196869290553cf1871b4ce6` (`pcre2-10.48`)
- Release archive:
  `https://github.com/PCRE2Project/pcre2/archive/refs/tags/pcre2-10.48.tar.gz`
- 8-bit library only; JIT compiled out; built from the release's
  `pcre2.h.generic` and `config.h.generic` placed unchanged in the build
  staging, with `pcre2_chartables.c.dist` as the chartables translation unit;
  compile-time switches `SUPPORT_PCRE2_8`, `SUPPORT_UNICODE`; link size 2
- Source files: the same 31 libpcre2-8 translation units listed under the
  Linux pcre2 entry
- Public header: `pcre2_v10.48/include/pcre2.h` (the release's
  `pcre2.h.generic`, copied unchanged)
- License: `pcre2_v10.48/LICENSE` (the release's `LICENCE.md`)
- Compile command (once per source, plus the chartables unit):

```text
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 -DNDEBUG \
  -DHAVE_CONFIG_H -DPCRE2_CODE_UNIT_WIDTH=8 -DSUPPORT_PCRE2_8 -DSUPPORT_UNICODE \
  -I <staging>/pcre2gen -I modules/pcre2/src -c <source> -o pcre2.o
llvm-ar rcs pcre2_v10.48/pcre2.a <objects>
```

- Archive size: `538862` bytes
- Archive SHA-256: `6d61a67c5d2d4d800d5c71ac7c4629a27996a0bf39631f0b594efb5dbf4adf5c`
- Header SHA-256: `d59dad66a9e77e5ccffe35ad51c3ca6000ce9afe77a58fafa3d66b4845db9a61`
- License SHA-256: `4195c519dcfe4a4ffedc4b8ccc5d49e4dd02efd5ece6b69a4fba5d20080902a9`

### Windows llhttp_v9.4.3

- Source: `modules/llhttp` (git submodule) at commit
  `0e815792b167a9bd8ace259b95b7da953776c288` (`release/v9.4.3`); the three
  source digests, the unchanged public header, the MIT license, and the
  no-system-library, no-allocator contract are the Linux entry's
- Producer: Clang 23.1.2, `llvm-ar`; COFF objects carry a build timestamp, so a
  rebuild from the same sources has the same size but different bytes
- Compile command (once per source):

```text
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 -DNDEBUG \
  -I modules/llhttp/include -c <source> -o llhttp.o
llvm-ar rcs llhttp_v9.4.3/llhttp.a <objects>
```

- Archive size: `81688` bytes
- Archive SHA-256: `db0a182f9d324d0a15eef74e06ae941027dad5191c8beb12d1210b27d4f16ca2`
- Header SHA-256: `5bc82fa51b19aa8bee7d921038393fbfc74e8cc9bae0844c2d82c102fb8cfd68`
- License SHA-256: `628168d68bb5a8a17e0bbefb3bd74e326e1edc20583d76f52f457c82c921867d`

### Windows yyjson + pcre2 reproducibility

Both archives were created with `llvm-ar rcs` from clean object directories on
2026-09-28. The exact source commits, toolchain, target, compile definitions,
source lists, sizes, and digests above are the build record; the staging trees
(staged headers, objects, and the build scripts) are not part of the
repository.

### Windows verification

The combined probe compiles, links, and runs one program against all five
archives, mirroring how a generated Hexal program consumes them:

```text
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 probe.c -DUTF8PROC_STATIC \
  -DPCRE2_STATIC -DPCRE2_CODE_UNIT_WIDTH=8 \
  -I modules/mimalloc/include -I modules/utf8proc -I modules/libuv/include \
  -I lib/x86_64-windows-gnu-ucrt/yyjson_v0.13.0/include \
  -I lib/x86_64-windows-gnu-ucrt/pcre2_v10.48/include \
  lib/x86_64-windows-gnu-ucrt/libuv_v1.52.1/libuv.a \
  lib/x86_64-windows-gnu-ucrt/mimalloc_v3.5.1/mimalloc.a \
  lib/x86_64-windows-gnu-ucrt/utf8proc_v2.11.3/utf8proc.a \
  lib/x86_64-windows-gnu-ucrt/yyjson_v0.13.0/yyjson.a \
  lib/x86_64-windows-gnu-ucrt/pcre2_v10.48/pcre2.a \
  -lpsapi -lshell32 -luser32 -ladvapi32 -lbcrypt -liphlpapi -luserenv \
  -lws2_32 -ldbghelp -lole32 -o probe.exe
```

It calls `mi_malloc`/`mi_free`, hands libuv the mimalloc allocator with
`uv_replace_allocator` exactly as the generated scheduler bootstrap does, runs
a real loop through `uv_loop_init`/`uv_run`/`uv_loop_close`, exercises a
Windows syscall path with `uv_exepath`, decodes U+1F600 with
`utf8proc_iterate`, checks `yyjson_version` (the release's version hex, 3328
for 0.13.0), and compiles a trivial pattern
with `pcre2_compile` and matches with `pcre2_match`. It exits 0 and prints
`pack-ok uv=1.52.1 mi=30501 utf8proc=2.11.3 yyjson=0.13.0 pcre2=10.48`, which
also confirms each archive carries the pinned upstream version.

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
