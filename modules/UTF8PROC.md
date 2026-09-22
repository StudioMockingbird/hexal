# utf8proc module

Hexal uses the official utf8proc source through the Git submodule at
`modules/utf8proc`.

- Repository: https://github.com/JuliaStrings/utf8proc
- Release tag: v2.11.3
- Pinned commit: e5e799221b45bbb90f5fdc5c69b6b8dfbf017e78
- Unicode data: 17.0.0
- Release archive: https://github.com/JuliaStrings/utf8proc/archive/refs/tags/v2.11.3.tar.gz
- Release archive size: 202535 bytes
- Release archive SHA-256: abfed50b6d4da51345713661370290f4f4747263ee73dc90356299dfc7990c78
- License: MIT/Expat and Unicode data terms, retained in `modules/utf8proc/LICENSE.md`
- Qualified target: x86_64-windows-gnu-ucrt
- Qualification compiler: Clang 22.1.8, target `x86_64-w64-windows-gnu`

The upstream `utf8proc.c` translation unit includes the generated
`utf8proc_data.c`; the archive therefore contains one object. The qualified
Windows build uses:

```text
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 -DNDEBUG -fPIC -pthread \
  -DUTF8PROC_STATIC -I modules/utf8proc \
  -c modules/utf8proc/utf8proc.c -o utf8proc.o
llvm-ar rcs utf8proc_v2.11.3/utf8proc.a utf8proc.o
```

The release header and license are copied unchanged into the target runtime
pack. The archive is statically linked; it has no additional system-library
requirement.
