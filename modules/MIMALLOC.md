# mimalloc module

Hexal uses the official mimalloc source through the Git submodule at
`modules/mimalloc`.

- Repository: https://github.com/microsoft/mimalloc
- Release tag: v3.5.1
- Tag object: 8e05dab9b9e38aa92ab6a6e137baefeaa9e45e40
- Pinned commit: 34fbd7e7cd4627424490afe19b20f8066bfc537d
- Release archive: https://github.com/microsoft/mimalloc/archive/refs/tags/v3.5.1.zip
- Release archive size: 1621941 bytes
- Release archive SHA-256: 8fd8b3cf3ed7a20fb972b48e35f5e59007c33f1929ea801e9b12542924a6367b
- License: MIT, retained in `modules/mimalloc/LICENSE`
- Qualification backend: Zig 0.16.0, target `x86_64-windows-gnu`

The submodule is not patched or reformatted. The parent repository embeds the
upstream `include` and `src` trees for the installed compiler. A source build
of Hexal requires the submodule to be initialized; a generated-program build
does not invoke Git, CMake, a downloader, or a package manager.

The qualified Windows build compiles only `src/static.c` with the installed
Zig backend using:

```text
zig cc -std=c11 -target x86_64-windows-gnu -O2 \
  -DMI_BUILD_RELEASE -DMI_WIN_INIT_USE_RAW_DLLMAIN \
  -I <staging>/dependencies/mimalloc/include \
  -c <staging>/dependencies/mimalloc/src/static.c \
  -o <staging>/objects/mimalloc.o
```

The driver materializes the source and include trees into the isolated build
staging directory, embeds the resulting object into the final static link,
and never changes the upstream source. `-O2` is the release optimization;
debug and sanitizer builds are separate qualification configurations, not the
shipped allocator object.
