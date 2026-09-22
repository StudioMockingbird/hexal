# utf8proc module

Hexal uses the official utf8proc source through the Git submodule at
`modules/utf8proc`.

- Repository: https://github.com/JuliaStrings/utf8proc
- Release tag: v2.11.3
- Tag object: e5e799221b45bbb90f5fdc5c69b6b8dfbf017e78 (lightweight tag)
- Pinned commit: e5e799221b45bbb90f5fdc5c69b6b8dfbf017e78
- Release archive: https://github.com/JuliaStrings/utf8proc/archive/refs/tags/v2.11.3.tar.gz
- Release archive size: 202535 bytes
- Release archive SHA-256: abfed50b6d4da51345713661370290f4f4747263ee73dc90356299dfc7990c78
- License: MIT/Expat and Unicode data terms, retained in `modules/utf8proc/LICENSE.md`
- Unicode data version: 17.0.0

The submodule is not patched or reformatted. utf8proc is one translation unit:
`utf8proc.c` includes the generated `utf8proc_data.c`, and `utf8proc.h` is the
public header, so each qualified archive contains exactly one object.

## Qualified builds

| Target pack | Qualification compiler | Target triple |
| --- | --- | --- |
| `x86_64-linux-gnu` | Clang 23.1.1 | `x86_64-linux-gnu` |
| `x86_64-windows-gnu-ucrt` | Clang 22.1.8 | `x86_64-w64-windows-gnu` |

The qualified Linux build compiles it with:

```text
clang -std=c11 -O2 -DNDEBUG -fPIC -pthread -DUTF8PROC_STATIC \
  -I modules/utf8proc -c modules/utf8proc/utf8proc.c -o utf8proc.o
ar rcs utf8proc_v2.11.3/utf8proc.a utf8proc.o
```

The qualified Windows build uses:

```text
clang --target=x86_64-w64-windows-gnu -std=c11 -O2 -DNDEBUG -fPIC -pthread \
  -DUTF8PROC_STATIC -I modules/utf8proc \
  -c modules/utf8proc/utf8proc.c -o utf8proc.o
llvm-ar rcs utf8proc_v2.11.3/utf8proc.a utf8proc.o
```

`UTF8PROC_STATIC` selects the static declarations; neither archive carries a DLL
import table and the Windows archive needs no additional system library. The
release header and license are copied unchanged into each target runtime pack.
The driver materializes the checked-in archive and header from the selected
target pack and never builds utf8proc, searches for a system copy, or consults
pkg-config.
