# libuv build input

- Upstream: https://github.com/libuv/libuv
- Release: v1.52.1
- Annotated tag object: 48dbd851fe4ad7d1252b75ac907c725a437e1634
- Peeled commit: 1cfa32ff59c076ffb6ed735bbc8c18361558661f
- Source archive: https://dist.libuv.org/dist/v1.52.1/libuv-v1.52.1.tar.gz
- Archive size: 1354296 bytes
- Archive SHA-256: 66d511b9e6e334c0e62279eb234fbfb2b3110b1479c09b95b44c7afca8cff9e7
- Qualified target: x86_64-windows-gnu
- C dialect: C11
- System libraries: psapi, user32, advapi32, iphlpapi, userenv, ws2_32, dbghelp, ole32, shell32
- Build output: the driver compiles the qualified source list and creates `libuv.a` inside each isolated build staging directory before static linking.
- Integration rule: build inputs are embedded from this pinned submodule; user builds do not invoke Git, CMake, or a downloader.
