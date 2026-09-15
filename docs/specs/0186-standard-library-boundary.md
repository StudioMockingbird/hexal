# RFC 0186: Standard Library Boundary

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready after RFC 0190; design and execution plan
  settled, implementation not started
- Created: 2026-09-14
- Updated: 2026-09-15
- Scope: define what belongs to the language core versus the standard library,
  the stdlib import syntax, where stdlib code lives, and how current
  compiler-owned capabilities migrate
- Depends on: RFC 0190 and the current module, import/export, protected-name,
  generated artifact, and in-memory compiler contracts in `docs/reference.md`
- Coordinates with: RFC 0039 (C interop), RFC 0178 and RFC 0182 (`Program`,
  `Entropy`), RFC 0187 (build modes, dead-code removal), and ADR 0055 (driver)
- Does not add: packages, a package manager, third-party library resolution,
  project manifests, versioned stdlib releases, or C interop

## Problem

Every operating-system capability is currently a compiler-owned protected name.
The list grew from scalars and collections to `IO`, `Bytes`, `Seek`, `File`,
`FileMode`, `Duration`, `Instant`, `WallTime`, `Address`, `Dns`, `Tcp`,
`TcpConnection`, `TcpListener`, `Process`, `Pipe`, `ProcessOptions`,
`StartedProcess`, `Environment`, `EnvironmentVariable`, `ProcessStream`,
`ExitStatus`, `Signal`, `Signals`, `Terminal`, and `TerminalSize`. RFCs 0178 and
0182 add `Program` and `Entropy`.

Consequences:

- Every capability reserves global names and breaks any user program already
  declaring them (`File`, `Signal`, and `Process` are common domain names).
- Every capability is visible in every module whether or not it is used, which
  works against Hexal's small-surface goal.
- There is nowhere to put reusable code written in Hexal itself.
- The checker and reference grow a new special case for every library-shaped
  feature, even when the compiler needs no language rule for it.

The empty `stdlib/` and `libs/` directories exist in the repository but have no
contract.

## Goals

1. The language core stays small: only what the compiler must understand.
2. Library-shaped capabilities cost nothing, reserve no names, and are invisible
   until imported.
3. One obvious import form for stdlib modules, reusing the existing import block.
4. The compiler stays string-in/string-out and needs no filesystem access to
   find the stdlib.
5. Migration preserves generated behavior and runtime lowering. Added import
   lines may renumber later `#line` directives and therefore change affected C
   artifact hashes; only that source mapping and intentional stdlib artifact
   ownership may move.
6. Writing a stdlib module in Hexal and later reimplementing a core library in
   Hexal over C interop are both possible without changing importers, for every
   API a Hexal module can declare. Methods that mutate their receiver through a
   pointer (for example `Ptr<mut Bytes>.read`) cannot be user-declared under the
   struct-only value-receiver rule; a Hexal reimplementation would expose them
   as module functions (`Io.read(@stream, into, 16)`), which is an API change
   accepted for that future rewrite, not a v1 migration.

## Layer model

| Layer | Owner | Visibility | Implemented as |
| --- | --- | --- | --- |
| Language core | Compiler | Always visible; protected names | Checker rules, generator lowering, `hexal/` runtime components |
| Core library | Compiler, package `compiler/corelib` | Only through `import ... from "std/..."` | Compiler-owned declarations and C runtime templates; no Hexal source |
| Source stdlib module | `stdlib/` Hexal source | Only through `import ... from "std/..."` | Ordinary Hexal module embedded in the compiler |

"Standard library" means both kinds of `std` module. Whether a module is a core
library or Hexal source is an implementation detail: importers use one path
form and one access syntax for both, so a core library can later become Hexal
source without breaking anyone.

### Core admission rule

A name belongs to the language core only if at least one of these is true:

1. It has dedicated syntax or literal forms (scalars, `String` literals, Array
   literals, `Fun` types, `Ptr`).
2. The language needs a checker rule for it independently of any library:
   contextual literal typing, mutability layers, exhaustiveness, allocation and
   freed-state tracking, `try`/`errdefer`, or generic-constructor restrictions.
   A checker rule that exists only for one library type (for example, which
   positions an `IO` may occupy) does not make that type core; core libraries
   are compiled by the compiler and may carry such rules.
3. The generator must special-case its representation (handle layout, niches,
   tagged unions, collection specialization).
4. Another core construct depends on it (`try` needs `Error`; `for ... in` needs
   collections; `print` needs the text model; spawning needs `Task`).
5. It is a scheduler primitive whose correctness depends on runtime integration
   that no library can express (`Task`, `Channel`, `Mutex`, `Atomic`).

Everything else belongs in the stdlib. A stdlib module is a **core library** when
it needs compiler-generated C runtime support and **source** otherwise.

### Resulting core inventory

Stays core and protected:

- all scalars, `Size`, `Byte`, `Rune`, `String`, `Strand`, `RuneCursor`, `Nil`,
  `EoS`, `Unknown`;
- `Heap`, `Stash`, `Pool`, `Ptr`, `Slice`, `Array`, `List`, `Dict`, `Fun`;
- `Error` and `ErrorKind`;
- `Task`, `Channel`, `Mutex`, `Atomic`;
- operations `print`, `size_of`, `align_of`; and
- entry-module `return` (RFC 0182) as syntax.

Moves to stdlib. Value types keep their names; instance methods are unchanged:

| Module | Diagnostic alias | Exported types |
| --- | --- | --- |
| `std/io` | `Io` | `IO`, `Bytes`, `Seek` |
| `std/fs` | `Fs` | `File`, `FileMode` |
| `std/time` | `Time` | `Duration`, `Instant`, `WallTime` |
| `std/net` | `Net` | `Address`, `TcpConnection`, `TcpListener` |
| `std/process` | `Proc` | `Process`, `Pipe`, `ProcessOptions`, `StartedProcess`, `Environment`, `EnvironmentVariable`, `ProcessStream`, `ExitStatus` |
| `std/signal` | `Sig` | `Signal`, `Signals` |
| `std/terminal` | `Term` | `TerminalSize` |
| `std/program` | `Prog` | none |
| `std/entropy` | `Ent` | none |

All of these are core libraries in v1.

`Byte` (the scalar alias of `UInt8`) stays core. `Bytes` is a different type: the
in-memory byte stream created by `Bytes.over(buffer)`, which reads, writes, and
seeks over a `List<Byte>` like `IO` does over a native handle. It moves with `IO`.

`Seek` lives in `std/io` and is shared by `IO`, `Bytes`, and `File`. Code that
seeks a File therefore imports both modules:

```hexal
import
    Fs from "std/fs",
    Io from "std/io"
end

position := try file.seek(Io.Seek.Start(position = 16))
```

### Static operations become module functions

Hexal has no static methods: a user type cannot declare `Point.origin()`, and a
user module exposes a module function `Shapes.origin()` instead. Today's
compiler-owned static operations (`File.open`, `Tcp.connect`, ...) and the
fallible constructor `Signals(...)` are a form only the compiler can write. If
they moved unchanged, core libraries could never be reimplemented as Hexal
source without changing their API, and user and stdlib libraries would follow
two conventions.

Every static operation therefore becomes an exported module function. The module
is the namespace the type name used to provide. Types that existed only as
namespaces (`Dns`, `Tcp`, `Terminal`, `Program`, `Entropy`) are removed: they
have no values.

| Current | Module | New |
| --- | --- | --- |
| `Duration.nanoseconds(value)` | `std/time` | `nanoseconds(value) -> Duration` |
| `Duration.microseconds(value)` | `std/time` | `microseconds(value) -> Duration` |
| `Duration.milliseconds(value)` | `std/time` | `milliseconds(value) -> Duration` |
| `Duration.seconds(value)` | `std/time` | `seconds(value) -> Duration` |
| `Instant.now()` | `std/time` | `now() -> Instant` |
| `WallTime.now()` | `std/time` | `wall_time() -> WallTime \| Error` |
| `Task.sleep(duration)` | `std/time` | `sleep(duration)` |
| `IO.stdin()` | `std/io` | `stdin() -> IO \| Error` |
| `IO.stdout()` | `std/io` | `stdout() -> IO \| Error` |
| `IO.stderr()` | `std/io` | `stderr() -> IO \| Error` |
| `Bytes.over(buffer)` | `std/io` | `bytes_over(buffer) -> Bytes` |
| `File.open(path, mode)` | `std/fs` | `open(path, mode) -> File \| Error` |
| `Address.parse(text, port)` | `std/net` | `parse_address(text, port) -> Address \| Error` |
| `Dns.resolve(heap, host, service)` | `std/net` | `resolve(heap, host, service)` |
| `Tcp.connect(address)` | `std/net` | `connect(address) -> TcpConnection \| Error` |
| `Tcp.listen(address, backlog)` | `std/net` | `listen(address, backlog) -> TcpListener \| Error` |
| `Process.start(options)` | `std/process` | `start(options) -> StartedProcess \| Error` |
| `Signals(subscriptions)` | `std/signal` | `subscribe(subscriptions) -> Signals \| Error` |
| `Terminal.is_attached(stream)` | `std/terminal` | `is_attached(stream) -> Bool \| Error` |
| `Terminal.size(stream)` | `std/terminal` | `size(stream) -> TerminalSize \| Error` |
| `Program.*` (RFCs 0178, 0182) | `std/program` | `arguments()`, `current_directory(heap)`, `home_directory(heap)`, `temporary_directory(heap)`, `executable_path(heap)`, `available_parallelism()` |
| `Entropy.fill(into)` (RFC 0178) | `std/entropy` | `fill(into)` |

Parameters, results, Error kinds, messages, and runtime behavior are unchanged;
only the spelling moves. Instance methods (`file.read(...)`,
`connection.write(...)`, `signals.next()`, `duration.as_seconds()`) are
unchanged.

```hexal
import
    Fs from "std/fs",
    Net from "std/net",
    Time from "std/time"
end

opened := Fs.open("data.txt", Fs.FileMode.Read())
connection := Net.connect(address)
Time.sleep(Time.milliseconds(50))
```

## Import syntax

User modules and stdlib modules share one import block and one entry form,
`Alias from "<path>"`. Only the path differs:

| Path | Resolves to | Example |
| --- | --- | --- |
| Starts with `./` or `../` | a user module in the source map, relative to the importing module | `Shapes from "./graphics/shapes"` |
| Starts with `std/` | a stdlib module (core library or source) embedded in the compiler | `Fs from "std/fs"` |

```hexal
import
    Fs from "std/fs",
    Net from "std/net",
    Shapes from "./graphics/shapes"
end
```

Rules:

- A module path is either **relative** (starts with `./` or `../`, unchanged) or
  a **collection path** `"<collection>/<component>{/<component>}"`. A bare path
  with no `./` is never relative, so `"shapes"` cannot silently mean a user file.
- `std` is the only collection in v1. Every other collection name is a Module
  Error: `unknown module collection <name>; only std is available`. This keeps
  bare paths reserved for future packages without committing to their design.
- Collection paths take no `.hex` suffix. Components are Hexal identifiers.
- An alias is still required. No wildcard, unqualified, or selective import is
  added.
- A module inside `std` imports another stdlib module with a collection path or a
  relative path that stays inside `std`. A relative path cannot leave the
  collection it starts in: user modules cannot reach into `std` relatively, and
  stdlib modules can never import user modules.
- Duplicate-import, cycle, export-closure, and visibility rules apply unchanged.
- Resolution is compiler-owned: an unknown stdlib module is a Module Error naming
  the path and listing no host filesystem location.

### Using an imported module

An import binds only the alias. Every exported declaration is reached through it,
and the rule is the same for user modules and stdlib modules: **write the name
exactly as the defining module writes it, prefixed by `Alias.`**

| Declaration | Inside the defining module | From an importer |
| --- | --- | --- |
| Function | `origin()` | `Shapes.origin()` |
| Type in an annotation | `p: Point` | `p: Shapes.Point` |
| Struct construction | `Point(x = 1, y = 2)` | `Shapes.Point(x = 1, y = 2)` |
| ADT variant | `Shape.Circle(radius = 1.0)` | `Shapes.Shape.Circle(radius = 1.0)` |
| ADT variant pattern | `\| Shape.Circle then` | `\| Shapes.Shape.Circle then` |
| Method on a value | `p.width()` | `p.width()` (no alias; the method must be exported) |
| Fixed module value, read | `DefaultPort` | `Settings.DefaultPort` |
| `mut` module value, assign | `RequestCount = RequestCount + 1` | `Settings.RequestCount = Settings.RequestCount + 1` |
| Generic function, type, or method | `new_box<Int32>(1)`, `Box<Int32>`, `box.get()` | `Boxes.new_box<Int32>(1)`, `Boxes.Box<Int32>`, `box.get()` |

```hexal
import
    Fs from "std/fs",
    Settings from "./settings"
end

opened := Fs.open("data.txt", Fs.FileMode.Read())
Settings.RequestCount = Settings.RequestCount + 1
```

Consequences:

- Values carry no alias. Once a value exists, its methods, members, and equality
  are used exactly as for a local value; only names being *looked up* are
  qualified.
- Module values are the same storage for every importer. Assigning an exported
  `static mut` value through an alias writes the defining module's storage, and
  `Atomic` module values use their own operations (`Stats.hits.add(1)`).
- An alias is never a value: `Fs` alone, `x := Fs`, or passing `Fs` is rejected.
- Aliases do not chain: `Fs.Io.IO` is invalid even if `std/fs` imports `std/io`.
  Import `std/io` directly.

### ADT variants keep their owner

Today an importer writes a variant as `Alias.Variant(...)`, dropping the ADT
name, and the long form `Alias.Adt.Variant(...)` is rejected. Verified against
the current compiler:

```hexal
-- lib.hex
type Env is Inherit | Clear end
type Stream is Ignore | Inherit end
type Conduit is struct fd: Int32 end
type Link is Idle | Conduit end
export
    Env,
    Stream,
    Conduit,
    Link
end
```

```hexal
import
    L from "./lib"
end
s: L.Stream := L.Inherit()           -- Type Error: expected Stream initializer; got Env
p: L.Conduit := L.Conduit(fd = 1)    -- Type Error: L.Conduit takes no arguments
```

`Stream.Inherit` and the struct `Conduit` cannot be reached from an importer at
all. `std/process` has both shapes: `Environment.Inherit` next to
`ProcessStream.Inherit`, and the type `Pipe` next to the variant
`ProcessStream.Pipe`.

The only qualified variant form becomes `Alias.Adt.Variant(...)`, in
construction and in patterns:

```hexal
s: L.Stream := L.Stream.Inherit()
p: L.Conduit := L.Conduit(fd = 1)
stream := Proc.ProcessStream.Pipe()

code := match status is
| Proc.ExitStatus.Exited then status.code
| Proc.ExitStatus.Terminated then -1
end
```

`Alias.Variant(...)` is removed rather than kept as a shorthand, so there is one
spelling and no silent resolution to the wrong ADT. Variants of moved core types
follow the same rule: `Io.Seek.Start(position = 0)`, never an unqualified
`Start`.

### Migration diagnostic

Using a moved name without importing it reports one of these exact templates:

```text
File is declared in std/fs; add `Fs from "std/fs"` to the import block
File.open is now open in std/fs; add `Fs from "std/fs"` and call `Fs.open`
Dns is removed; resolve is a function in std/net
```

```text
<name> is declared in <module>; add `<alias> from "<module>"` to the import block
<owner>.<operation> is now <replacement> in <module>; add `<alias> from "<module>"` and call `<alias>.<replacement>`
<owner> is removed; <replacement> is a function in <module>
```

The diagnostic aliases come from the core-library table. They are suggested
spellings only: import aliases remain file-local user choices. The hints come
from one static table of moved names and operations; names are never
auto-imported.

A hint is emitted only for a name that ordinary resolution leaves unresolved.
Local, module, and imported declarations resolve first; a user declaration of a
freed name is never reported:

```hexal
type File is struct
    id: Int32,
end

value: File := File(id = 1)   -- valid; no std/fs hint
```

## Module identity and generated artifacts

- A stdlib module's canonical identity is `std/<path>` in the stdlib collection.
  It is distinct from a user module whose logical key happens to be
  `std/<path>.hex`; users may still name a directory `std`.
- Symbol owner encoding keeps its current form for user modules (`m...`) and uses
  prefix `s` for stdlib modules (`hex_f_s2_io4_path_join`), so the two can never
  collide.
- A source stdlib module emits `stdlib/<path>.c` and `stdlib/<path>.h` under the
  same rules as `modules/<path>.c/.h`.
- Its source key for `#line` directives, diagnostics, and `Error.file` is
  `stdlib/std/<path>.hex`, the module's path in this repository. It is never
  `std/<path>.hex`, which a user module may legitimately own. Canonical module
  identity stays `std/<path>`; only source provenance uses the distinct key, so
  a user `std/fs.hex` and the stdlib `std/fs` never share a `#line` file name.
- Every reachable module, user or stdlib, emits all of its functions and methods,
  exactly as today. See "Unused functions" below.
- A core library emits no module artifact. Its declarations keep their existing
  compiler-owned C spellings (`hex_file`, `hex_tcp_connection`, ...) and existing
  `hexal/` component artifact names, selected by the existing operation-driven
  demand rules. Importing a core library without calling it selects nothing.
- `hexal.h`, component, and root-module contracts are otherwise unchanged.

### Unused functions

A reachable module emits every function it declares. Verified: a module
exporting `used` and `unused_exported` and declaring private `unused_private`,
imported by a program that calls only `used`, emits all three definitions in
`modules/lib.c`.

This RFC keeps that rule for user and stdlib modules:

- A non-generic module's generated C depends only on its own source (plus the
  types it names), never on which of its functions importers call. The same
  non-generic stdlib module produces the same artifact in every program.
- A module exporting a generic template emits only the concrete specializations
  requested by the complete project. Its artifact is therefore project-
  dependent and deterministic for that source map and specialization-demand
  set. Future incremental compilation and RFC 0164 cache keys must include that
  concrete demand; module source alone is not a valid key.
- Release builds remove unused functions at link time: RFC 0187 compiles with
  `-ffunction-sections -fdata-sections` and links with `--gc-sections`. Debug
  executables keep them.
- Generic templates keep emitting only their requested specializations.
- Removing unused *private* functions per module (provable from the module's own
  source alone) is a possible later generator improvement, alongside RFC 0183's
  demand-driven helper emission; it is not part of this RFC.

## Where the code lives

| Content | Location |
| --- | --- |
| Source stdlib modules | `stdlib/std/<path>.hex` |
| Embedding shim | `stdlib/stdlib.go`: package `stdlib`, one `//go:embed` of `std`, one exported `Sources() map[string]string` that returns a fresh copy on every call, so no caller can mutate a later compilation's stdlib |
| Core library declarations | `compiler/corelib`, one file per module |
| Core library C runtime templates | `compiler/corelib/runtime/` |
| Language-core C runtime templates | `compiler/generator/packages/` |
| Third-party libraries | `libs/` stays reserved and uncontracted; out of scope |

### The `compiler/corelib` package

Today each capability is spread across four places: `compiler/types/<name>.go`,
`compiler/checker/<name>.go`, `compiler/generator/<name>.go` (plus
`<name>_render.go`), and `compiler/generator/packages/<name>.c/.h`. Core
libraries become one Go package, `corelib`, that owns everything about a module
that does not depend on a particular compiler stage and is not canonical type
metadata:

- **Module table** (`corelib/modules.go`): each module path (`std/fs`, ...) and
  its exported names.
- **Declarations** (`corelib/fs.go`, `corelib/net.go`, ...): exported module
  functions, instance-method signatures, the mapping from exported names to
  canonical types, and each operation's fixed Error messages.
- **Runtime templates** (`corelib/runtime/file.c`, `network.c`, `process.c`,
  `signal.c`, `terminal.c`, `time.c`, `handle.c`, and later `program.c` and
  `entropy.c`), embedded by `corelib` and handed to the generator by name.

Dependencies point one way, so no import cycle is possible:

```text
compiler/types  <-  compiler/corelib  <-  compiler/checker
                                      <-  compiler/generator
```

`corelib` imports only `compiler/types`. It does not import the checker or
generator.

What stays where it is:

- **Canonical type metadata stays in `compiler/types`.** The type arena,
  canonicality checks, and union/equality/placement rules consult each core
  type's descriptor and low-level identity predicate (`FileType`, `IsFile`,
  `IsFileMode`, `IsTcpConnection`, `IsBuiltinAdt`, ...). Moving them would force
  `compiler/types` to import `corelib` (a cycle) or add a descriptor
  registration API (a plugin layer). They remain in `compiler/types`, unchanged
  in identity. What changes is only how a source name reaches them: through a
  `corelib` module export instead of the global protected-name table.
- **Checking and lowering logic.** It reads and writes checker and generator
  state directly. Moving it into `corelib` would force `corelib` to import those
  packages (a cycle), or would need a registration interface that every stage
  calls back into, a plugin framework the pipeline architecture rejects. These
  files stay in `checker/` and `generator/` and are renamed `corelib_<module>.go`
  so the boundary is visible.
- **Runtime used by the language core.** `event.c` (the Task-to-native-work
  bridge), `io.c` (the sink `print` writes through), `runtime.c`, and every
  collection, text, heap, and scheduler template stay in `generator/packages/`.
  `std/io` declares the `IO` type, but the byte-stream runtime remains core
  because `print` depends on it.

A core-library feature is therefore one declaration file, one runtime template,
one checker file, one generator file with matching names, and its canonical
type descriptor in `compiler/types`.

The stdlib is embedded in the compiler binary at Go build time, so:

- `Compile(sources, entrypoint, project)` keeps its signature; the source map
  holds only user sources.
- The compiler reads no files; embedding is not host filesystem inspection at
  compile time.
- The stdlib version is the compiler version. There is no separate stdlib
  version, lockfile, or override in v1.
- Determinism holds: identical user sources plus an identical compiler produce
  identical artifacts.
- The driver and workbench need no stdlib path configuration.

`go:embed` cannot reference parent directories, which is why the shim lives in
`stdlib/` and the compiler imports `hexal/stdlib` rather than embedding from
`compiler/`.

## What goes into the source stdlib

Nothing moves into Hexal source merely to populate the directory. A source module
is added only when there is a concrete consumer and it needs no C runtime. v1
ships exactly one small source module to prove the pipeline end to end:

- `std/ascii`: pure `Byte` classification (`is_digit`, `is_alpha`, `is_space`,
  `to_lower`, `to_upper`), with no allocation and no dependencies.

Its complete v1 surface is:

```text
is_digit(value: Byte) -> Bool
is_alpha(value: Byte) -> Bool
is_space(value: Byte) -> Bool
to_lower(value: Byte) -> Byte
to_upper(value: Byte) -> Byte
```

- `is_digit` recognizes ASCII `0` through `9`.
- `is_alpha` recognizes ASCII `A` through `Z` and `a` through `z`.
- `is_space` recognizes space, horizontal tab, line feed, vertical tab, form
  feed, and carriage return.
- `to_lower` maps ASCII uppercase letters to lowercase and returns every other
  byte unchanged; `to_upper` performs the inverse mapping.
- These functions classify bytes only. They do not decode UTF-8 or apply
  locale-sensitive or Unicode rules.

Later candidates, each requiring its own justification: path manipulation over
`String`, sorting and searching over `Slice<mut T>`, hashing, formatting helpers,
and a deterministic PRNG (kept distinct from `std/entropy`).

Until RFC 0039 lands, source modules cannot call C. When it does, a core library
may be rewritten as Hexal source over foreign declarations without
changing its import path or exported names.

## Naming inside modules

Every moved type keeps its current name, and every static operation takes the
module-function name in the table above. Removing now-redundant type prefixes
(`Net.TcpConnection` could become `Net.Connection`) is out of scope. New stdlib
declarations do not repeat their module name, and a module function whose
module exports several creatable types names what it creates
(`parse_address`, `bytes_over`).

## Policy for future capabilities

- A new operating-system or library capability gets a stdlib module, not a
  protected name.
- A focused RFC proposing a new protected name must show which core admission
  criterion it meets.
- In-flight RFCs 0178 and 0182 target `std/program` and `std/entropy`.

## Required sweep

- remove moved names from the protected-name table and from the reference's
  protected list;
- delete any checker or generator branch that exists only to reject user
  redeclaration of a moved name;
- parser and lexer path-literal validation for collection paths;
- module resolution, identity, owner encoding, artifact paths, and `#line` keys;
- every compiler-owned static operation and the `Signals(...)` constructor:
  replace with the module functions in the table, and delete the namespace-only
  types `Dns`, `Tcp`, and `Terminal` with their checker and generator branches;
- alias-qualified ADT variant construction and patterns: remove
  `Alias.Variant(...)`, add `Alias.Adt.Variant(...)` to the parser (patterns)
  and checker (construction), and update the reference's dotted-pattern rule;
- every `Seek.Start(...)`, `Seek.Current(...)`, and `Seek.End(...)` construction
  and pattern, which becomes `Io.Seek.Start(...)` and so on. (`Start`,
  `Current`, and `End` are already unprotected user names; nothing about that
  changes.)
- the dotted match-pattern grammar, which gains the alias-qualified form
  `dotted-match-pattern = identifier , "." , identifier , [ "." , identifier ]`
  for `| Alias.Adt.Variant then`;
- move capability module/API declarations into `compiler/corelib` (canonical
  type descriptors and identity predicates stay in `compiler/types`), move
  capability templates out of `compiler/generator/packages`, and rename the
  remaining checker and generator capability files to `corelib_<module>.go`;
- migration diagnostic table;
- every snippet, integration test, and c23validation fixture that uses a moved
  name, static operation, or `Alias.Variant(...)`; and
- every repository consumer of compiler name resolution; no LSP exists in the
  current tree, so this RFC adds no hypothetical LSP work.

## Implementation plan

### Phase 0: freeze the migration inventory

1. Record every protected name, compiler-owned static operation,
   `Signals(...)` construction, alias-qualified ADT construction/pattern, and
   current source use in snippets, integration tests, tagged fixtures, and the
   workbench catalog.
2. Record generated artifacts and the snippet manifest before changing module
   resolution or capability ownership.
3. Treat additions after this inventory as explicit scope changes rather than
   silently omitting them from migration.

### Phase 1: collection paths and identity

1. Accept `"std/..."` import paths; reject other collections with the exact
   diagnostic above.
2. Add collection-qualified canonical identity, `s` owner encoding, and
   `stdlib/<path>` artifact keys.
3. Add `stdlib/stdlib.go` and `std/ascii`; resolve source stdlib modules from the
   embedded map.

### Phase 2: core libraries

1. Create `compiler/corelib` with the module table, per-module declarations, and
   the embedded runtime templates; the checker and generator consume it.
2. Make core-library declarations resolvable only through an import alias.
3. Remove moved names from the protected set; add the migration diagnostic.
4. Replace static operations and `Signals(...)` with module functions; delete
   the namespace-only types.
5. Switch alias-qualified ADT variants to `Alias.Adt.Variant(...)` in
   construction and patterns; remove `Alias.Variant(...)`.

### Phase 3: migration

1. Rewrite snippets, tests, and fixtures to import the modules they use.
2. Regenerate the snippet manifest. For snippets changed only by imports,
   operation spelling, and variant qualification, review the generated diff and
   permit only corresponding `#line` renumbering plus intentional stdlib
   artifact ownership; runtime statements and helper bodies remain unchanged.
3. Add a two-program generic-module probe: different requested concrete
   specializations produce the corresponding deterministic defining-module
   artifacts, while repeated identical demand produces byte-identical output.

### Phase 4: documentation

1. Update `docs/reference.md` Modules, Programs/names (protected list), grammar
   (`module-path-literal`), Generated artifact split, and each moved capability
   section header, only with explicit user approval.

## Validation

This list is exhaustive:

- `std/...` import resolves; other collections, `.hex` suffix on a collection
  path, and non-identifier components are rejected with exact diagnostics;
- relative paths cannot cross into or out of `std`; stdlib cannot import user
  modules;
- a user logical key `std/fs.hex` and stdlib `std/fs` coexist with distinct
  identities, symbols, and artifacts;
- `std/ascii` compiles, emits `stdlib/ascii.c/.h` with `#line` mapping to
  `std/ascii.hex`, and runs under the tagged C23 gate;
- every `std/ascii` function has the exact Byte/Bool signature above; digit,
  letter, six whitespace-byte, case-conversion, unchanged-byte, non-ASCII-byte,
  and UTF-8-non-decoding boundaries are covered;
- every moved name and every former static operation is rejected with its exact
  migration diagnostic, and each freed name (including `Dns`, `Tcp`, `Terminal`,
  `Program`, and `Entropy`) is declarable by user code;
- every module function in the static-operation table works through its alias
  with unchanged parameters, results, Errors, and runtime behavior; instance
  methods, ADT variants, equality, and printing of moved types work unchanged;
- every row of the "Using an imported module" table works for a user module and
  for a stdlib module; `Alias.Variant(...)`, a bare alias used as a value, and a
  chained alias are rejected with exact diagnostics;
- an importer constructs and matches both same-named variants of two exported
  ADTs, and constructs a struct whose name equals a variant of another exported
  ADT;
- `compiler/corelib` imports no compiler package other than `compiler/types`;
- importing a core library without calling it emits no component or dependency;
- a non-generic module's generated artifacts are byte-identical when compiled by
  two programs that call different subsets of its exported functions;
- an exported generic template produces specializations matching the complete
  project's demand, repeated identical demand is byte-identical, and differing
  concrete demand is permitted to change only the defining module's requested
  specializations;
- `Io.Seek` works for `IO`, `Bytes`, and
  `File`; `Byte` remains a protected core name;
- a user declaration of a freed name (`File`, `Signal`) resolves without a
  migration hint; an unresolved moved name receives the hint; a program using a
  user `File | Error` union and `Fs.File | Error` together generates distinct,
  deterministic C union spellings;
- a source stdlib module's `#line` directives, diagnostics, and `Error.file`
  use `stdlib/std/<path>.hex`, distinct from a user `std/<path>.hex` in the same
  program;
- mutating the map returned by `stdlib.Sources()` does not change a later
  compilation;
- a stdlib source module constructing `ErrorKind.Other(header = ...)` behaves
  exactly as the same code in a user module;
- migrated snippet manifest movement is limited to reviewed `#line`
  renumbering and intentional stdlib artifact ownership; generated runtime
  statements and helper bodies remain unchanged; and
- `Compile` signature, determinism, and the no-filesystem boundary are unchanged.

## Settled decisions

- **Sequencing.** This RFC lands before RFCs 0178 and 0182. `Program` and
  `Entropy` never become protected names; those RFCs target `std/program` and
  `std/entropy` directly.
- **Sleep.** `Task.sleep` moves to `std/time` as `Time.sleep(duration)`. No core
  API names a stdlib type.
- **Names.** Every moved type keeps its current name. Shortening type names is
  out of scope.
- **Static operations are module functions.** No compiler-only static operation
  or fallible constructor survives the move; see the table. Adding static methods
  to the language was rejected as new surface and a second way to expose a
  factory.
- **Variants are `Alias.Adt.Variant(...)`.** The short importer form is removed
  because it resolves same-named variants to the wrong ADT and hides structs.
- **Whole-module imports only.** Selective imports
  (`import { File } from "std/fs"`) are not added. They give no artifact, link,
  or compile-time saving: the compiler already selects generated code from
  checked uses, not from the import list, and an imported module is checked in
  full either way. They would add a second way to name every declaration and
  let imported names collide with locals, which the separate alias namespace
  prevents today. A module needing a short type name uses an ordinary
  transparent alias (`type File is Fs.File`).
- **Byte streams.** `IO`, `Bytes`, and `Seek` move to `std/io`; `Byte` stays core.
  `print` writes through the core runtime sink and needs no source-visible `IO`.
  Seeking a File imports both `std/fs` and `std/io`.
- **Error kinds.** Stdlib modules follow exactly the user rules: existing
  `ErrorKind` variants and `Other(header = ...)`. A new variant needs an RFC
  showing a portable operating-system or runtime condition; library-domain
  conditions never qualify. A library needing precise classification returns
  its own ADT in a structural union beside `Error`
  (`Value | JsonProblem | Error`).
- **Collection name.** `"std/<path>"`; future packages use `"<package>/<path>"`
  with `std` reserved.
- **Unused functions.** Reachable modules emit whole; release builds drop unused
  functions at link time (RFC 0187). Program-wide function reachability was
  rejected because it makes a module's artifact depend on its importers, which
  breaks per-module incremental compilation and caching.
- **Generic artifacts.** Exported generics are permitted uniformly in user and
  source stdlib modules. Their requested concrete specializations remain owned
  by the defining module, so that module's artifact is project-dependent. A
  future incremental compiler or object cache includes the deterministic
  specialization-demand set in its identity.

## Open questions

None.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation updates
the grammar, module resolution, protected names, artifact split, and each moved
capability's section only after behavior stabilizes and with explicit user
approval. The protected-name list update also adds `ErrorKind`, which the
reference already declares protected but omits from that list.
