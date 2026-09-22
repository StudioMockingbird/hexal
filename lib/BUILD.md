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
    utf8proc_v2.11.3/
      utf8proc.a
      include/utf8proc.h
      LICENSE.md
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

The three archives must be rebuilt together if the target, libc baseline,
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

## utf8proc_v2.11.3

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

## System libraries

`manifest.json` declares `pthread`, `dl`, and `rt` on the libuv dependency, in
that order, and none on mimalloc. They are the union the native combined probe
required; on glibc 2.44 all three resolve from the C library itself, so the
declaration is retained as the target's portable link set rather than a
measured necessity on this host.

## Verification

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

## Windows GNU/UCRT utf8proc pack

`lib/x86_64-windows-gnu-ucrt/utf8proc_v2.11.3/` contains the target-qualified
utf8proc archive, public header, and upstream license. The Windows pack is
checked in but is not embedded or selected by the current driver; this record
qualifies the dependency payload independently of the driver-selection work.

### Build identity

- Hexal target profile: `x86_64-windows-gnu-ucrt`
- Clang toolchain target triple: `x86_64-w64-windows-gnu`
- Host and probe environment: x86-64 Windows, MinGW-w64/UCRT
- Producer compiler: Clang 22.1.8
- Archiver: LLVM `llvm-ar` 22.1.8
- Optimization: `-O2 -DNDEBUG -fPIC -DUTF8PROC_STATIC`
- CPU policy: target-portable x86-64; no `-march=native`

### utf8proc_v2.11.3

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
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 -DNDEBUG -fPIC -pthread \
  -DUTF8PROC_STATIC -I modules/utf8proc \
  -c modules/utf8proc/utf8proc.c -o utf8proc.o
llvm-ar rcs utf8proc_v2.11.3/utf8proc.a utf8proc.o
```

- Archive size: `349998` bytes
- Archive SHA-256:
  `cccf77623c664d67aedb9113ce410c93c3effeeeb19b32d56a3e2a127822311c`
- Header SHA-256:
  `a4e498b7392c383cf3b22e662da21e0b48f1264806235d87b5d8bd166232f658`
- License SHA-256:
  `3b510150d34f248a221bb88e1d811238d6c6c18b51231822c42974c39bb07256`

The Windows qualification probe includes `utf8proc.h`, links the archive, and
decodes U+1F600 with `utf8proc_iterate`; it exits 0 and prints `utf8proc-ok`.

The existing libuv and mimalloc Windows artifacts remain the prior pack inputs;
their requalification and driver embedding are separate from this utf8proc
payload record.
