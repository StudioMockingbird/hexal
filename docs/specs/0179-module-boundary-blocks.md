# RFC 0179: Module Boundary Blocks and Module Values

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Created: 2026-09-13
- Updated: 2026-09-13
- Scope: replace declaration-prefixed imports and exports with one leading
  import block and one trailing export block; add statically initialized module
  values
- Does not add: runtime module initializers, mutable import aliases, wildcard
  imports, re-exports, automatic cleanup, or foreign ABI exports

## Summary

Replace:

```hexal
module Maths = import "./maths.hex"

export type Number is Int32

export fun add(left: Number, right: Number): Number do
    return left + right
end
```

with:

```hexal
import
    Maths from "./maths.hex"
end

type Number is Int32

fun add(left: Number, right: Number): Number do
    return left + right
end

export
    Number,
    add
end
```

- A file has at most one import block. It is its first top-level construct.
- A file has at most one export block. It is its final top-level construct.
- Both blocks are top-level only and require at least one entry.
- Export lists describe module visibility separately from declaration syntax.
- Exported module values may be fixed or `mut`; mutability is not an export
  restriction.
- A fixed direct `Atomic<T>` module value is the synchronization primitive for
  concurrently shared counters and flags. Its atomic payload remains mutable
  through Atomic operations while the binding itself cannot be replaced.
- Module values have program lifetime and require static initialization. This
  RFC adds no module-init functions or hidden runtime work.

## Grammar

Conceptual replacement grammar:

```ebnf
program = lexical-separation , [ import-block ] , { top-level-item }
          , [ export-block ] ;

import-block = "import" , import-entry
               , { "," , import-entry } , [ "," ] , "end" ;
import-entry = identifier , "from" , module-path-literal ;

export-block = "export" , export-entry
               , { "," , export-entry } , [ "," ] , "end" ;
export-entry = identifier , [ "." , identifier ] ;

top-level-item = declaration-item | statement ;
```

Semantic restrictions narrow this shape:

- `declaration` at module scope is a module value and must satisfy Static
  initialization below.
- Imported modules still reject executable top-level statements.
- The entrypoint may retain executable top-level statements.
- `import`, `export`, `from`, and their matching `end` are block syntax, never
  expressions or local statements.
- `from` becomes reserved. `module` ceases to be reserved because this RFC
  removes its only language use.
- A comma separates entries; a final trailing comma is optional. Line breaks
  carry no additional grammar meaning.

## Import contract

```hexal
import
    Maths from "./maths.hex",
    Shapes from "../graphics/shapes"
end
```

- The import block, when present, is the first top-level construct after
  discarded whitespace and comments.
- All imports in one file appear in that block. A second import block is an
  error even when adjacent to the first.
- The alias is local to the importing module and retains the existing distinct
  import-alias namespace.
- Path spelling, optional `.hex`, lexical source-map resolution, duplicate
  canonical-module rejection, cycle rejection, and case sensitivity retain
  their current contracts.
- Import order is source order. It affects deterministic diagnostics and graph
  traversal but creates no runtime initialization order in this RFC.
- An import entry does not re-export its target. An export entry naming an
  import alias or imported declaration is an error.
- The removed `module Alias = import "..."` form is a Syntax Error.

## Export contract

```hexal
export
    Point,
    distance,
    Point.translate,
    DefaultPort,
    RequestCount
end
```

- The export block, when present, is the last top-level construct. No
  declaration, statement, import, or second export block may follow it.
- An unqualified entry names one declaration or module value owned by the
  current module.
- A qualified `Type.method` entry names one method declared by the current
  module for that nominal type. It is not an imported-name path.
- Types, named functions, methods, fixed module values, and `mut` module values
  are exportable.
- Local functions, anonymous-function bindings, import aliases, imported
  declarations, and executable statements are not exportable.
- Each exported declaration is listed exactly once. Unknown, ambiguous, and
  duplicate entries are errors.
- Exporting a type does not export its methods. Each method is listed
  explicitly.
- An exported declaration or value type retains the existing interface-closure
  rule: every private nominal type reachable from its public type is an error.
- Successfully resolved exports remain visible to importers independent of the
  declaration's textual position before the final block.
- `export` no longer prefixes declarations. `export fun`, `export type`, and
  `export method` are Syntax Errors with guidance to use the final block.
- Module export visibility is unrelated to the future foreign C ABI. A later
  C-interoperability RFC owns its own explicit export spelling and stable C
  symbol contract.

## Module values

Both fixed and mutable values may be shared:

```hexal
DefaultPort: Int32 := 8080
mut RequestCount: UInt64 := 0

export
    DefaultPort,
    RequestCount
end
```

An importer may read both and assign only the mutable value:

```hexal
import
    Settings from "./settings.hex"
end

port := Settings.DefaultPort
Settings.RequestCount = Settings.RequestCount + 1
```

- A module value has one program-wide storage instance and program lifetime.
- Fixed module values cannot be assigned after initialization through either
  their defining module or an importer.
- `mut` module values may be assigned through either module. Copies of their
  current value follow ordinary Hexal copy rules; qualification denotes the
  storage place itself.
- `@Settings.RequestCount` is valid and yields `Ptr<mut UInt64>`;
  `@Settings.DefaultPort` yields `Ptr<Int32>`. The storage outlives every Task.
- Concurrent unsynchronized reads and writes to one mutable module value have
  the same programmer-owned race discipline as equivalent shared pointer
  access. This RFC adds no implicit lock or atomic conversion.
- A direct Atomic module value is declared fixed and shared through its
  qualified storage place:

```hexal
RequestCount: Atomic<UInt64> := Atomic<UInt64>(0)

export
    RequestCount
end
```

Importers use `Settings.RequestCount.load()`, `store`, `exchange`, `fetch_add`,
`fetch_sub`, or `compare_exchange`. They cannot copy, replace, assign, or take
`@` of the Atomic. This preserves Atomic's existing non-copyable and
non-addressable contracts; the compiler internally passes the address of the
one program-lifetime storage instance to the generated atomic operation.
- A `mut Atomic<T>` module binding is rejected: `mut` would imply replacing the
  non-copyable Atomic value. Mutation belongs to Atomic's operations.
- Functions and methods in the defining module may read fixed module values and
  read or write mutable ones. This is access to program-lifetime module
  storage, not a closure or capture of a lexical local.
- Module values are visible throughout their defining module independent of
  textual position. Type names inside their declarations retain the existing
  source-order type-visibility rule; initializers cannot refer to another
  module value, so no initialization-order graph is introduced.
- Imported access is always qualified. Wildcard imports and implicit unqualified
  injection do not exist.

## Static initialization

Module values introduce storage but no executable initializer. Their
initializer must be a finite static value that the compiler can lower directly
to C static storage without evaluation at program startup.

Initially accepted initializer trees contain only:

- Bool, integer, Size, Byte, Float, Rune, and Strand literals;
- Array literals whose elements are accepted static initializer trees;
- struct construction whose fields are accepted static initializer trees;
- ADT variant construction whose payload fields are accepted static
  initializer trees;
- structural-union injection from one accepted static initializer tree; and
- transparent aliases around an accepted type and value.

One explicit constructed-value exception is accepted:

- a direct fixed `Atomic<T>` module value initialized by
  `Atomic<T>(literal)`, where the literal directly determines an allowed T
  under Atomic's current contract. The initializer is emitted directly as C
  atomic static initialization; it performs no runtime call or allocation.

Initially rejected anywhere in a module-value initializer:

- function or method calls, including otherwise pure calls;
- operations, conversions, indexing, member reads, and references to another
  module value;
- String, List, Dict, Task, Channel, Mutex, Stash, Pool, IO, File, pointers,
  Slice, Fun, Error, an aggregate transitively containing one, or an aggregate
  containing Atomic; direct Atomic is governed by the exception above;
- `try`, `spawn`, allocation, address-taking, dereference, or interpolation;
- Nil without an accepted contextual union member; and
- any expression requiring runtime cleanup, failure propagation, or ordering.

These restrictions are representation-driven rather than mutability-driven.
Both of these are valid:

```hexal
Origin: Point := Point(x = 0, y = 0)
mut CurrentDirection: Direction := Direction.North()
```

Neither of these is valid:

```hexal
Connection := try Database.connect("localhost")
Names := List<String>(Heap())
```

Runtime resources remain explicitly created by the entrypoint and passed as
handles or pointers. A future module-initialization RFC may expand the
initializer set only after defining dependency order, fallible initialization,
cleanup, detached-Task interaction, and repeated embedding.

## Generated C

- Every module value has one definition in its owning module C file.
- An exported value has one `extern` declaration in the owning module header.
- A fixed value's declaration and definition carry C `const`; a mutable value's
  do not. A direct fixed Atomic is the interior-mutability exception: its C
  storage uses the existing `_Atomic` spelling without `const`, while Hexal
  still forbids rebinding it.
- A private value has internal linkage. An exported value has generated-program
  external linkage using the existing injective module/declaration naming.
- Static initializer lowering preserves Hexal evaluation-independent value
  representation and emits no module initializer function.
- Generated C consumers include the module header and access the same storage;
  the compiler does not synthesize getter or setter wrappers.
- Exported generated symbols are private implementation details of one
  generated program, not a promised foreign ABI.
- Module headers remain self-contained and include every complete type needed
  by an exported value declaration.
- Definition and declaration ordering remains deterministic.

## Diagnostics

Use the earliest phase that can prove each error:

- parser: misplaced, repeated, empty, or unterminated import/export block; old
  declaration-prefix syntax; malformed entries;
- module resolution: bad path, missing source, duplicate canonical import,
  cycle, or alias collision;
- checker: unknown/duplicate export, re-export attempt, invalid method owner,
  private interface dependency, invalid module-value initializer, or assignment
  to a fixed imported value; and
- generator: no new user diagnostic. Valid checked input must lower or report
  an internal compiler error.

Required guidance:

```text
import block must be the first top-level construct
export block must be the final top-level construct
declaration export modifiers were removed; list the declaration in the final export block
module value initializer requires a static value
```

## Required sweep

- Remove `module Alias = import "..."` parsing, checked-node assumptions,
  snippets, tests, and documentation.
- Remove the per-declaration `export` modifier from grammar, parser nodes,
  checker dispatch, generator dispatch, snippets, and tests.
- Preserve one authoritative module graph and export table; the blocks change
  syntax and collection timing, not identity or resolution architecture.
- Replace closed-spec syntax only in active source, tests, snippets, and
  canonical documentation. Archived specs remain immutable historical records.
- Reconcile deferred RFCs that show old syntax only when they become active;
  do not edit them as part of this RFC.
- Do not introduce a general constant evaluator, runtime module initialization,
  getter wrappers, or C ABI annotations while performing the migration.

## Implementation plan

### Phase 1: parser and checked representation

1. Add `from`; remove `module` if repository search confirms it has no other
   current grammar role.
2. Replace import declarations and export modifiers with explicit block nodes.
3. Enforce one leading import block and one trailing export block during
   parsing, with exact structural diagnostics.
4. Preserve source tokens for every entry so module and export diagnostics keep
   correct logical file, line, and UTF-8 byte column.

### Phase 2: module graph and export resolution

1. Adapt the current graph builder to consume import entries without changing
   canonical identities or lexical resolution.
2. Collect declarations first, then resolve the final export list against the
   owning module's declaration tables.
3. Add qualified method-entry resolution and reject re-exports, duplicates,
   ambiguity, and private-interface leakage.
4. Keep graph traversal and diagnostics deterministic in source order.

### Phase 3: module-value checking

1. Admit module-scope fixed and `mut` declarations in every reachable module.
2. Implement one recursive allowlist predicate for the exact static initializer
   tree and exact permitted types above; do not use a rejection blocklist.
3. Register module storage separately from lexical local bindings so function
   access is global access rather than capture.
4. Extend qualified place checking with fixed/mutable capability and ordinary
   address-taking.
5. Apply exported-interface closure to the complete stored type.
6. Add direct fixed Atomic as one dedicated module-value position. Reuse its
   existing T allowlist and operations; reject `mut Atomic`, copying,
   assignment, address-taking, and Atomic-containing aggregates.

### Phase 4: generation

1. Add one program-wide deterministic inventory of module-value definitions and
   exported declarations.
2. Emit private/internal or exported/external C storage with matching `const`
   qualification.
3. Lower accepted initializer trees directly as C static initializers.
4. Extend qualified reads, assignments, and address-taking to the generated
   symbol; add no accessor or initialization function.
5. Keep module headers self-contained and source mappings on executable code,
   not on compiler-generated storage scaffolding.

### Phase 5: migration and conformance

1. Mechanically migrate all active Hexal sources, integration fixtures, C23
   fixtures, and workbench snippets to the two block forms.
2. Add focused parser/checker tests and module-graph integration tests.
3. Add tagged generated-C fixtures covering private/exported, fixed/mutable,
   aggregate, qualified assignment, address-taking, and cross-module access.
4. Assert no module-init function or accessor wrapper is emitted.
5. Regenerate snippet hashes only for expected syntax/source-map or generated-
   artifact changes and review the complete artifact breakdown.
6. With explicit user approval, update `docs/reference.md` once after behavior
   stabilizes; otherwise leave the implementation unclosed.

## Validation

This section is exhaustive.

### Syntax and placement

- Zero blocks, one non-empty leading import block, one non-empty trailing export
  block, or both in their required positions parse.
- Empty, repeated, nested, local, misplaced, and unterminated blocks reject.
- A declaration or statement before import rejects; anything after export
  rejects.
- Multiple entries require commas; one optional trailing comma is accepted.
- The old module-import and declaration-prefixed export forms reject with the
  required migration guidance.
- `from` is reserved and `module` is available as an ordinary identifier after
  its old use is removed.

### Import and export resolution

- Existing lexical path, optional-extension, source-map, identity, duplicate,
  cycle, alias-namespace, reachability, and determinism contracts remain.
- Types, functions, and methods export and import through the new lists.
- A method requires `Type.method`; exporting its type alone does not export it.
- Unknown, ambiguous, repeated, local, anonymous-function, import-alias, and
  re-export entries reject.
- Exported interfaces reject every reachable private nominal type and accept
  builtins plus exported nominal types.

### Module values

- Fixed and `mut` module values may be private or exported.
- Imported fixed and mutable values are readable; only mutable values are
  assignable.
- Defining-module functions can access their module values without enabling
  lexical capture or nested functions.
- Address-taking yields read-only or writable Ptr according to storage
  mutability and remains valid for program lifetime.
- All admitted scalar, Array, object, ADT, union, and transparent-alias static
  initializer shapes compile and preserve their values.
- Every rejected expression/type category above produces `module value
  initializer requires a static value` at its earliest offending node.
- Concurrent access to an ordinary mutable module value gains no implicit lock,
  atomic operation, or race diagnostic.
- A fixed exported `Atomic<UInt64>` supports concurrent qualified `fetch_add`
  and `load` against one storage instance; the generated result is exact.
- Direct Atomic accepts every currently permitted Atomic element type and
  operation. `mut Atomic`, Atomic assignment/copy/address-taking, and a module
  value whose aggregate contains Atomic reject.

### Generated artifacts and regression

- One owning C definition exists per module value; exported values have one
  matching header declaration and private values have none.
- Fixed declarations and definitions agree on `const`; mutable ones omit it.
  Direct Atomic declarations and definitions agree on the existing `_Atomic`
  spelling, omit `const`, and still reject source-level rebinding.
- One exported Atomic definition and every qualified Atomic operation use the
  same C storage object; no per-import copy or wrapper state is emitted.
- Imported reads, mutable assignments, and pointer access use the same generated
  storage symbol without accessor wrappers.
- No module initializer function, runtime allocation, cleanup registration, or
  generated foreign-ABI promise is introduced.
- Headers are self-contained, symbol names remain injective, output is
  deterministic, and unrelated runtime components are not selected.
- Ordinary Go tests require no external toolchain. Tagged C23 tests compile and
  run all accepted cross-module cases under every qualified toolchain.
- Every active snippet demonstrates the new syntax where relevant; the catalog
  contains focused multi-module imports, exports, methods, fixed values, and
  mutable values.
- Existing manifest entries change only where source syntax/source mapping or
  the new module-value artifacts require it; every unexpected artifact change
  is investigated rather than absorbed.

## Reference synchronization

Do not edit `docs/reference.md` from this draft without explicit user approval.
An approved implementation must update the grammar first, then replace the
module import/export rules and add the exact module-value/static-initializer/C23
contracts above before the RFC closes.
