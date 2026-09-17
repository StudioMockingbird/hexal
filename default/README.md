# Default Hexal project

This is the small project created by `hexal init`. It prints a greeting whose
bytes and length come from C code.

## What the sample shows

- `main.hex` uses `Hello from c "main.h"` to import declarations from a C
  header.
- `main.c` owns the greeting and returns a pointer to its static bytes plus
  the byte length declared in `main.h`.
- A foreign function call and pointer-to-slice conversion are placed inside
  `unsafe do ... end`, because Hexal cannot prove the lifetime and bounds of
  memory owned by C.
- `Slice<Byte>.from_pointer` views the C bytes without copying. The sample
  then creates an owned Hexal `String`, prints it, and frees it through the
  manually managed `Heap`.

The C string includes its newline, so the output is:

```text
Hello, world!
```

## Compile

From the repository root, use the rebuilt compiler and an installed Clang 18+
compiler. The runtime pack is embedded in `bin/hexal`:

```text
go build -o bin/hexal ./cmd/hexal
bin/hexal build default/main.hex \
  -out default/hello \
  -cc /usr/bin/clang \
  -target x86_64-linux-gnu \
  -c-source main.c \
  -c-include .
```

Then run it:

```text
default/hello
```

`-c-source main.c` compiles and links the implementation. `-c-include .`
allows both the C source and generated C to find `main.h`. Because the
positional filepath selects `default` as the source root, these foreign-input
paths resolve relative to `default`.
