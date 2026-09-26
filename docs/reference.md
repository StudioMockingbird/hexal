# Hexal Language Reference

This file is the sole normative syntax and semantic reference. `status.md` tracks implementation.
Compiler behavior that disagrees with this file is a conformance bug.

## Grammar

The grammar defines source shape only. Semantic rules in the remainder of this file may reject a
grammatically valid form.

Nine lexical/parser rules are not expressible in EBNF:

- Tokens use maximal munch. Inside nested type-argument lists only, one `>>` token may close two
  levels; in expression position it is always one shift token.
- `|` is a union separator in type position and bitwise-or in expression position; parser context
  selects the grammar. On `is`'s right side specifically, an unparenthesized `|` stays inside that
  type expression as a union separator and is never read as the following `binary-tail`.
- Within one independently delimited expression, every `binary-tail` must repeat the same token
  kind (`is` counts as its own kind, distinct from every other). A grouping `( expression )`, each
  call argument, index expression, array element, and match scrutinee or arm
  result independently delimits a fresh expression with its own requirement. Hexal has no
  binary-operator precedence: this is the entire rule for how a mixed chain must be written.
- A `<` immediately after a postfix expression opens a type-argument list only when a balanced
  `>` (or a `>>` closing two nested lists at once) is immediately followed by `(` (an ordinary
  generic call) or by `.` then an identifier then `(` (a qualified generic ADT-variant
  constructor, e.g. `Result<Int32, String>.Ok(value)`); otherwise `<` is the relational operator.
- The lexer recognizes `{{ expression }}` interpolation inside every interpreted string, not only
  inside `String.interpolate`'s template argument, so it can tokenize the embedded expression (and
  any string or raw string nested inside it) correctly in one forward pass; a string containing
  `{{` outside that exact position is a grammatically valid `interpolation-template` that the
  semantic rules in the Text section reject. `{{`/`}}` nest through further interpreted strings
  written inside the embedded expression, tracked by the lexer, not by expression-level bracket
  matching.
- A quoted literal is a C header literal only after the contextual `c` that itself follows `from`
  (`Alias from c "x.h"`), or after `from` in a leading `extern c from "x.h"` block. An
  angle-bracketed literal is a C header literal only in those two positions. Everywhere else a
  quoted literal is an ordinary string, and a quoted literal directly after `from` is the module
  path.
- `extern`, `from`, `c`, `opaque`, `constant`, and `global` are contextual: each is the shown
  terminal only in the C-interop positions, and an ordinary identifier everywhere else. `opaque` in
  particular is not a reserved word, so `type X is opaque` is ambiguous between the opaque form and
  an alias to a type named `opaque`; the parser resolves it in favor of the opaque form.
- In a `method-declaration`, the receiver is the written `type-expression` and the method name is
  the final `.` component. A dotted receiver such as `Geometry.Point.rotate` is one qualified
  chain, so the parser peels the final `. identifier` back out of the receiver as the method name;
  `Ptr<Point>.length` peels the same way.
- A trailing comma is accepted in a value or member list -- struct members, ADT payload fields, call
  and method arguments, and array elements -- and rejected in a declaration or type list -- imports,
  exports, parameters, generic parameters, and type arguments. The grammar's `[ "," ]` alternatives
  encode exactly this policy.
- Maximal munch resolves comments: `--[` begins a multiline comment, and any other `--` begins a
  line comment. The two share the `--` opener, which this notation cannot express as a predicate.

The normative grammar is maintained in [`GRAMMAR.ebnf`](../GRAMMAR.ebnf), using the `golang.org/x/exp/ebnf` format.


## Language boundary

- Hexal is a statically typed systems language that lowers to readable C23 with `#line` mappings.
- Invalid or unsupported source fails closed under the diagnostic contract below.
- Values follow C-style shallow copying. Allocation and cleanup are explicit; there are no moves,
  borrow states, retain counts, implicit destructors, or compiler-enforced exactly-once cleanup.
  That names the mechanisms the language lacks, not a limit on what it diagnoses: see Allocation
  and lifetime for which cleanup misuses are rejected.
- Native modules are implemented; each `.hex` source is one module.
- Importing C declarations is implemented; see C interoperability for the supported surface.

## Programs, names, and bindings

- A source file contains ordered type, function, method, and executable declarations/statements.
  Executable statements occur only in the root program and lower to automatic locals in `main`.
- Hexal has no native mutable globals or global constants, and no `global` storage of its own. A
  fixed top-level `let` in an imported module is a module constant (see Modules): immutable
  program-lifetime storage private to its module; the `global` spelling appears only in an
  `extern c` declaration for a C object. State is otherwise local, allocated, or passed explicitly.
- Functions and methods are file-scope declarations. Nested functions and closures do not exist. A
  named function or method in the selected entry module captures an earlier top-level entry binding
  by reference, after parameters, `self`, and locals resolve; only bindings declared textually
  before it are visible for capture, and a captured binding is read or written through the entry
  environment. Imported-module functions, anonymous literals, and local function literals never
  capture.
- `return` is valid only inside a function or method body. The root program has no declared result.
- Type declarations become visible in source order: a type may name itself or an earlier type, not
  a later one. A module-level function or method signature is visible throughout its module
  regardless of declaration order: a function or method may call itself, an earlier declaration, a
  later declaration, or take part in mutual recursion, and its signature and body may name a module
  type declared later. Root executable statements execute in written source order and may call a
  function declared later, but a root declaration's own type annotation keeps the same source-order
  restriction as a type declaration: it cannot name a type not yet declared.
- Type and value names share one namespace. Protected names cannot be redeclared or shadowed.
  Protected types are every scalar plus `Size`, `Byte`, `String`, `Nil`, `EoS`,
   `Unknown`, `Heap`, `Error`, `ErrorKind`, `Mutex`,
   and constructors `Ptr`,
   `Slice`, `Fun`, `Array`, `List`, `Dict`, `Task`, `Channel`, `Atomic`, `Stash`, `Pool`.
   The retired `MutPtr` and `View` names stay reserved but name no type; the never-implemented
   `Ref`, `MutRef`, `Box`, and `MutSlice` names are free for user declaration.
  Protected operations are `print`, `size_of`, and `align_of`.
- Every operating-system capability type (`IO`, `Bytes`, `Seek`, `File`, `FileMode`, `Duration`,
  `Instant`, `WallTime`, `Address`, `TcpConnection`, `TcpListener`, `Process`, `Pipe`,
  `ProcessOptions`, `StartedProcess`, `Environment`, `EnvironmentVariable`, `ProcessStream`,
  `ExitStatus`, `Signal`, `Signals`, `TerminalSize`) and the former namespace types `Dns`, `Tcp`,
  `Terminal`, `Program`, and `Entropy` reserve no name. They are reachable only through a std
  module alias, and a user declaration of any of those names resolves as an ordinary declaration.
  An unresolved use of one reports the exact migration hint (see Standard library modules).
- Every value-binding declaration is introduced by `let` and states its type exactly once, on one
  side or the other. `let name: T = initializer` states it on the left; `let name = initializer`
  says the initializer states it, and is rejected when the initializer is contextual — an integer,
  float, or string literal, `nil`, an array literal, or a `match` whose every arm is contextual.
  Stating it on neither side is an error. Written parameters, members, ADT payloads, and results
  always require an explicit type. Compiler-typed `self` and `for` binders are the remaining
  exceptions; a `for` binder may also be written with a type, which must equal the type it would
  take (see `for ... in`).
- `=` assigns to an existing writable place. It does not introduce a value binding. Module aliases
  and a labeled constructor argument (`name = value` inside a struct or ADT-variant call) retain
  their grammar-defined uses of `=`.
- Bindings and object members are fixed by default. `mut` permits replacement and appears only on
  their declarations. Parameters, `self`, and `for` binders are fixed and cannot be shadowed in
  their own scopes.
- Member assignment requires a writable root and `mut` at every object-member step. Dereference
  writability comes from the pointer type.

## Modules

- `Compile` receives a source map and one entrypoint logical key. Source-map keys use `/`, are
  case-sensitive, and do not denote or inspect host filesystem paths. The entrypoint must exactly
  name one supplied source.
- A legal logical key is relative, uses `/` as its only separator, ends in exactly one `.hex`
  extension, and has one or more path components before it, each a Hexal identifier. Its first
  component may not be `std`: that prefix is reserved for the compiler-owned standard library, so a
  user key can never claim a stdlib canonical identity. Only the entrypoint and each key a resolved
  import reaches are validated, immediately before that source is lexed; an unreachable invalid key
  is ignored like any other unreachable entry. A violation is a Module Error naming the offending key
  and the complete rule.
- A module's canonical identity is its logical key without the trailing `.hex`. The logical key,
  not an absolute host path or import alias, determines nominal type, function, method, generic,
  specialization, generated-symbol, and artifact identity. Same-named declarations in distinct
  canonical modules are distinct.
- A file has at most one import block and at most one export block. The import block, when
  present, is the file's first top-level construct; the export block, when present, is its last.
  Either or both may be absent.
- An import entry is `Alias` `from` `Reference`. `from` is a contextual keyword, recognized only
  immediately after an import alias; it is never reserved elsewhere. `Alias` binds only in the
  importing module; import aliases occupy their own namespace and cannot shadow or be shadowed.
- A module reference is either a quoted relative source path or an unquoted dotted
  standard-library reference. The two forms are lexically distinct; the resolver, never string
  inspection, decides which tables to consult.
- A relative source path is quoted, starts with `./` or one or more `../`, uses `/`, and contains
  identifier path components with an optional terminal `.hex`. Resolution is lexical relative to
  the importing module's directory, strips the optional `.hex`, and cannot walk above the logical
  source-map root. Resolution consults only the supplied source map and requires exactly one
  case-sensitive logical key with the resulting canonical identity. A quoted payload that does not
  start with `./` or `../` is a Syntax Error.
- A standard-library reference is `std` `.` `identifier` { `.` `identifier` }; `std` is contextual
  and recognized only in that position. It must begin on the same line as `from`, and every
  component is an ordinary Identifier token (never a reserved word). It canonicalizes by joining
  the components after `std` with `/`: `std.program` and `std/crypto/hash` are the canonical
  identities `std/program` and `std/crypto/hash`. A std module is either a core library
  (compiler-owned declarations and C runtime templates, no Hexal source) or a source module
  embedded in the compiler; the distinction is an implementation detail, and importers use one
  alias form and one access syntax for both. An unknown module is a Module Error naming the dotted
  spelling. `std` remains legal as an ordinary declaration name, member name, and import alias
  outside reference position.
- A relative path may not reach the reserved `std/` canonical prefix: a supplied logical key whose
  first component is `std` is rejected, so a user source module never shares a stdlib canonical
  identity. Core-library module names are also not added to the protected-name table: an import
  alias is the only way to reach them, and a user declaration of the same name remains legal.
- Only the entrypoint and its transitive dependencies are compiled. Unreachable source-map entries
  produce no diagnostics, artifacts, or statistics. Each reachable canonical module is processed
  once. Duplicate imports of one canonical module and every dependency cycle are Module Errors.
- For identical source strings and entrypoint, traversal, diagnostics, statistics, and generated
  file contents are deterministic. `Files` map iteration order has no meaning.
- The entrypoint module may contain executable statements and root value bindings. Every imported
  module is declarations-only: it has no executable statements, root value bindings, initializer,
  runtime Heap, import-time effects, or final-expression result. A fixed top-level `let`
  declaration in an imported module is a module constant, not an executable statement.
- A fixed top-level `let name [: type] = expr` in an imported module declares a module constant:
  immutable program-lifetime storage private to its defining module unless named in that module's
  export block. Its initializer must be built entirely from the closed static-initializer set:
  literals, `Array` literals, struct construction, ADT variant construction, and structural-union
  injection, applied recursively. Every binding reference, including a reference to another module
  constant, is excluded, so no dependency graph or cycle rule exists. A module constant's type may
  not contain `Atomic` directly or through an alias, array, aggregate, ADT, union, or specialized
  generic value field; pointer, view, and function indirection stop the containment walk. A
  top-level `let mut` in an imported module is rejected: mutable library state is passed explicitly.
  A module constant is visible to every declaration and statement in its own module regardless of
  source position, exactly like a module-level function, and has one stable immutable object whose
  address may be taken.
- Declarations, including module constants, are private by default. An export block lists the bare
  names of module-level types, functions, and module constants, and `Type.method` for an exported
  method, that the defining module makes available; each name or `Type.method` pair may appear at
  most once, and only a module-level type, function, method, or module constant may be named. An
  importer accesses exported declarations only through its local alias; wildcard and unqualified
  imports do not exist. A module-scope anonymous function literal declares no source name and can
  never appear in an export block; a function declared at local (non-module) scope is not itself a
  module-level declaration and is equally ineligible.
- An exported declaration's complete interface closes over builtins and exported types only,
  including types reached through aliases, aggregates, generic arguments, parameters, results,
  receivers, members, and ADT payloads. Private types may remain inside an exported function or
  generic body when absent from its interface.
- Qualified types, functions, ADT variants, exported methods, and exported module constants retain the
  defining module's identity; renaming an import alias changes no identity. Within each module,
  type declarations retain source-order visibility; function, method, and module-constant visibility
  is order-independent (see Programs, names, and bindings). Successfully checked exports are
  available to importers regardless of the export's textual position in the defining module.
- Only a nominal type's defining module may declare methods for it. Imported types and
  transparent aliases of imported types may call exported methods but cannot receive new methods.
- Generated module artifacts, symbol linkage, header ownership, and source mapping are specified
  exclusively under Generated artifact split.

### Standard library modules

- The dotted standard-library reference resolves only to compiler-embedded modules; the compiler reads no host files. A
  core-library module emits no module artifact: its declarations keep compiler-owned C spellings and
  existing `hexal/` components, selected by the existing operation-driven demand rules. A source
  stdlib module emits `stdlib/<path>.c` and `stdlib/<path>.h`, maps `#line`, diagnostics, and
  `Error.file` to `stdlib/std/<path>.hex`, and prefixes its generated symbols with `s` (for example
  `hex_f_s5_ascii_is_digit`); user modules keep the `m` prefix and `modules/` artifacts, so the two
  can never collide.
- An imported module is used by writing the defining module's own names through the alias:
  `Alias.function(...)`, `Alias.Type(...)`, `Alias.Adt.Variant(...)`, `Alias.Type` in an annotation,
  and the ordinary unqualified method, member, and equality syntax on values.
- `std/program` exports these functions; `Prog` below is an ordinary file-local alias, not a
  protected name:

| Signature | Contract |
| --- | --- |
| `arguments() -> Slice<String> \| Error` | The host invocation in order, including element zero when supplied. Over one immutable process-lifetime snapshot shared by every call; the Slice and String bytes are read-only. The returned String handles are non-owning and `free` traps. Zero arguments produce an empty Slice. |
| `current_directory(heap) -> String \| Error` | Caller-Heap-owned current working directory. |
| `home_directory(heap) -> String \| Error` | Caller-Heap-owned home directory observation; not proof the path exists or is writable. |
| `temporary_directory(heap) -> String \| Error` | Caller-Heap-owned temporary directory observation. |
| `executable_path(heap) -> String \| Error` | Caller-Heap-owned executable path. |
| `available_parallelism() -> Size` | A non-zero estimate, matching libuv's contract; it does not expose scheduler worker count or mutate scheduler policy. |

  Path results are UTF-8-validated on every target; invalid bytes return `Error`, never a lossy
  replacement. No path query performs normalization, canonicalization, symlink resolution,
  separator rewriting, or case folding.
- `std/entropy` exports `fill(into: Slice<mut Byte>) -> Nil | Error`. It fills the entire
  destination or returns Error; short success is impossible. An empty Slice succeeds immediately,
  touches no memory, and submits no request. On failure the destination contents are unspecified.
- `std/ascii` exports exactly five functions:

| Signature | Contract |
| --- | --- |
| `is_digit(value: Byte) -> Bool` | ASCII `0` through `9`. |
| `is_alpha(value: Byte) -> Bool` | ASCII `A`–`Z` and `a`–`z`. |
| `is_space(value: Byte) -> Bool` | Space, horizontal tab, line feed, vertical tab, form feed, carriage return. |
| `to_lower(value: Byte) -> Byte` | ASCII uppercase to lowercase; every other byte unchanged. |
| `to_upper(value: Byte) -> Byte` | ASCII lowercase to uppercase; every other byte unchanged. |

  These functions classify bytes only: they do not decode UTF-8 and apply no locale-sensitive or
  Unicode rules.

- The capability core libraries export their former protected types and static operations. An
  exported type keeps its name; a static operation becomes a module function with the parameters,
  result, errors, and runtime behavior unchanged. Instance methods (`file.read`, `duration.as_seconds`,
  `signals.next`, ...) are unchanged and call for no alias.

| Module | Exported types | Module functions |
| --- | --- | --- |
| `std/io` | `IO`, `Bytes`, `Seek` | `stdin()`, `stdout()`, `stderr()`, `bytes_over(buffer)` |
| `std/fs` | `File`, `FileMode` | `open(path, mode)` |
| `std/time` | `Duration`, `Instant`, `WallTime` | `nanoseconds`, `microseconds`, `milliseconds`, `seconds`, `now()`, `wall_time()`, `sleep(duration)` |
| `std/net` | `Address`, `TcpConnection`, `TcpListener` | `parse_address(text, port)`, `resolve(heap, host, service)`, `connect(address)`, `listen(address, backlog)` |
| `std/process` | `Process`, `Pipe`, `ProcessOptions`, `StartedProcess`, `Environment`, `EnvironmentVariable`, `ProcessStream`, `ExitStatus` | `start(options)` |
| `std/signal` | `Signal`, `Signals` | `subscribe(subscriptions)` |
| `std/terminal` | `TerminalSize` | `is_attached(stream)`, `size(stream)` |

- An ADT variant of a type declared in another module (user or std) is written
  `Alias.Adt.Variant(...)` in construction and `| Alias.Adt.Variant then` in a match pattern. The
  former `Alias.Variant(...)` short form is removed, so a same-named variant of another exported ADT
  can never resolve silently. Variants of a local type keep the unqualified `Adt.Variant(...)` form.
  Seeking a file therefore imports both modules: `Fs.open(...)` with `Io.Seek.Start(position = 16)`.

## C interoperability

- A compilation containing a C import or a handwritten foreign declaration requires a nonempty
  qualified target profile; host-default ABI inference is rejected.
- `Alias from c <header>` imports a prepared binding module resolved from the reserved logical key
  `hexalc/h<sha256(target NUL header-form NUL header-payload)>.hex`. The leading `h` keeps the digest
  a legal identifier. System and quoted forms are distinct identities, as are different targets.
- An automatic import also exposes every object-like macro the selected Clang proves is a value
  expression of a supported foreign-constant type (a scalar, a data pointer, or a complete foreign
  record) as a foreign constant named after the macro; the generated C names the macro, so Clang
  performs the expansion and constant evaluation. Function-like macros, macros with no value
  expression, and macros whose type is not a supported foreign-constant type are omitted with the
  binding or wrapper guidance.
- The compiler verifies that the prepared module declares the requested header; an absent or
  mismatched entry reports `prepared C binding missing for <header>`. The `hexalc` prefix is reserved
  and rejected for user source.
- `DiscoverCImports(sources, entrypoint)` returns reachable header requests in deterministic module
  order and performs no filesystem or process operation.
- A foreign block is `extern c from <header> do ... end`, top-level only, after the import block and
  before every ordinary top-level item. Multiple blocks are permitted. `extern` is contextual but
  reserved at statement start.
- Declarations are private unless the module's final export block names them. `export` never changes
  C linkage.

Foreign declaration model:

| Form | Meaning |
| --- | --- |
| `type X is <alias-target>` | Transparent alias; carries no C spelling and adds no foreign type family. |
| `type X [as "C name"] is opaque` | Incomplete C type. May appear only behind a pointer. |
| `type X [as "C name"] is struct ... end` | Complete foreign record. Each field is `mut` unless C-qualifies it const. |
| `fun name [as "C symbol"](params) [: Type [as "C type"]]` | Foreign function; no body, generics, or methods. |
| `constant name [as "C symbol"]: Type` | Typed, non-addressable scalar, data pointer, or complete foreign record whose C spelling is an enumerator or object-like macro. |
| `global [mut] name [as "C symbol"]: Type` | Foreign object; `mut` permits writes. |

ABI type set on a qualified target:

| C family | Checked type |
| --- | --- |
| `void` result | no result, never `Nil` |
| `bool` / `_Bool` | `Bool` |
| exact-width signed/unsigned integers | matching Hexal integer |
| `float`, `double` | `Float32`, `Float64` |
| `size_t` | `Size` |
| `char *`, `const char *` | mutable/read-only `Ptr<Byte> | Nil` |
| `void *`, `const void *` | `Ptr<mut Unknown> | Nil`, `Ptr<Unknown> | Nil` |
| `char`, `short`, `int`, `long`, `long long`, pointer-width integers | target-resolved fixed Hexal integer |
| C enum | transparent alias to its resolved integer type |
| complete record | nominal foreign record; the named header owns its layout |

- `long` is `Int32` on the LLP64 Windows profile and `Int64` on an LP64 target. An `as` clause
  retains the exact boundary spelling.
- A parameter or result `as "C type"` accepts only compiler-known fundamental, exact-width,
  tag/record, and recursively qualified pointer spellings. An arbitrary library typedef is rejected
  with `C spelling <spelling> cannot be proven in a handwritten binding; use an automatic C import or
  expose a C wrapper`. This is not a general C declarator parser.
- C pointer syntax never proves non-null. A written bare pointer asserts non-null as part of its
  unsafe foreign contract; a nullable result must be narrowed before dereference.
- A foreign record has one program-wide identity keyed by target and canonical C identity (tag
  namespace plus tag spelling, or the canonical typedef spelling). An opaque declaration and a
  compatible complete definition coalesce; conflicting complete definitions report
  `conflicting foreign declarations for C symbol <symbol>`.
- A complete foreign record constructs, copies, accesses fields, passes and returns by value, and
  uses C-owned `sizeof`/`alignof`. Generated C never emits its `struct`, `enum`, or typedef
  definition. Foreign records reject equality, ordering, printing, and Dict-key use.
- An opaque type behind a pointer is valid; by value it reports `foreign type <type> is incomplete in
  <position>`.
- A foreign constant is readable without `unsafe`. Every direct foreign function call and every
  foreign global read or write requires `unsafe do ... end`; `unsafe` never suppresses an ordinary
  diagnostic.
- `String.c_pointer() -> Ptr<Byte>` and `Slice<T>.pointer() -> Ptr<T> | Nil` /
  `Slice<mut T>.pointer() -> Ptr<mut T> | Nil` each require `unsafe`, expose the address already
  present, and allocate and copy nothing. A mutable C output parameter never receives a String
  pointer.
- Calls and accesses lower to the exact recorded C identifier; a boundary cast is emitted only where
  the recorded C spelling differs from the checked representation. No forwarding wrapper is
  generated.
- A module that uses a foreign declaration emits each required C include once, deterministically,
  after `hexal.h` and the component headers and before declarations that name a foreign type.
- Diagnostics: `Syntax Error: extern blocks must precede ordinary top-level items`; `Syntax Error:
  foreign declaration requires a C header`; `Syntax Error: invalid C header name <name>`; `Syntax
  Error: invalid C spelling <spelling>`; `Configuration Error: C interoperability requires a
  qualified target`; `Type Error: unsupported foreign declaration <declaration>`; `Type Error: C
  spelling <spelling> cannot be proven in a handwritten binding; use an automatic C import or expose
  a C wrapper`; `Type Error: foreign type <type> is incomplete in <position>`; `Type Error: foreign
  call <name> requires an unsafe do ... end block`; `Type Error: foreign global <name> requires an
  unsafe do ... end block`; `Type Error: conflicting foreign declarations for C symbol <symbol>`;
  `Type Error: <type> has no supported C ABI mapping for target <target>`; `Name Error: C import
  <header> has no automatically imported declaration <name>; check the C name, use a handwritten
  binding, or expose a C wrapper`.

## Values, copying, and evaluation

- **Representation follows ownership, not shape.** A type that owns an allocation is a
  pointer-sized handle: it is passed as a pointer and copies alias one allocation. These are
  exactly the types exposing `free` — `String`, List, Dict, Channel, Mutex, Pool — plus Task, whose
  storage the scheduler reclaims through join or detach, and Stash, which exposes `reset`/`destroy`
  in place of `free`. A type that is inline or borrows
   storage is a value: it is passed by value and a copy copies its region. These are scalars,
   `String<N>`, Array, objects, ADTs, and Slice. A new type derives its representation from this
   rule rather than from resemblance to an existing one. `String<N>` is the case the rule exists
   for: it is text like `String`, but it owns no allocation and exposes no `free`, so it is a
   value, while the unparameterized `String` owns its bytes and is a handle.
- The rule is ownership because the C struct shape does not decide it: `String` and `Slice<T>`
  are both a pointer and a length, and differ only in that `String` owns its bytes while a Slice
  borrows them. `String` is therefore a handle and a Slice is a descriptor value.
- Every value is stored inline. Every copy copies the C representation. Scalars and
  inline aggregates (`String<N>`, Array, objects, ADTs) copy all inline bytes. Pointers and
  `String`, List, Dict, Task, Channel, Mutex, Stash, Pool copy their handle representation. Slice
  copies its pointer-length descriptor. Copies of a writable Slice alias the same elements;
  correct exclusive use is the programmer's responsibility. Heap copies a stateless token that selects the one default
  allocator.
- Assignment, arguments, returns, object/ADT construction, collection insertion, union injection,
  and Task capture are shallow copies. Copying does not invalidate the source.
- Values referring to external state include String, List, Dict, Task, Channel, Mutex, Stash, Pool,
  Slice, and aggregates containing them. Copies alias the same state. Freeing one
  alias leaves others dangling; losing the last handle can leak. A Slice additionally never
  owns its backing Array, List, String, allocation, or foreign region: keeping that storage
  alive and unreallocated for every Slice use is the programmer's responsibility, and misuse
  may produce undefined behavior in generated C. This is an explicit narrowing of the
  no-undefined-behavior goal, chosen to avoid a language-wide lifetime system.
- Every value is copyable except `Atomic<T>` and inline aggregates transitively containing one.
  Atomic containment traversal stops at every pointer and handle indirection.
- Full statements execute in source order, and evaluation within a statement is fully ordered:
  a binary expression evaluates its left operand before its right, keeping its written tree
  structure and a repeated operator's left associativity; a unary expression evaluates its
  operand before the operator applies; a
  receiver evaluates before a call's or method's arguments, which then evaluate left to right in
  written order; array elements and object/ADT initializers evaluate left to right in written
  order regardless of storage layout order; a union initializer evaluates its source once before
  selecting or writing the active member; an assignment evaluates its target place, including any
  receiver and index, before its source value. `and`/`or` keep left-to-right short-circuit
  evaluation and do not evaluate a skipped operand. Constant folding and reordering are valid only
  when they cannot change a value, a trap, or an observable effect (a defined trap, `print`
  output, an allocation or free, `spawn` task creation, or a write to a `mut` place or binding).
  Generated C uses a temporary or a separate statement wherever C's own operand or argument order
  would otherwise be unspecified.

## Position eligibility

These are the positions that hold a value. Other sections name them directly when narrowing what
they accept.

```text
Binding          ObjectMember     ADTPayload       UnionMember
ArrayElement     SliceElement     ListElement      DictValue
FunctionParam    FunctionResult   TaskArgument     TaskResult
ChannelElement   Pointee
HeapAllocation
```

- Storability and copyability are separate. Eligibility is checked after generic substitution, then
  completeness and finite size, then copyability when the operation copies, then feature-specific
  exclusions.
- A complete, finitely sized value is valid in every position unless a rule explicitly excludes it.
- Pointee additionally admits incomplete Unknown and applies the Pointers and Functions exclusions.
- HeapAllocation requires a complete, finite, copyable initializer valid under the Atomic rules.
- Assignment, argument passing, return, insertion, union injection, and spawn capture require a
  copyable value. Direct in-place initialization exists only for bindings and object members;
  non-copyable Atomic state is restricted to those positions.
- `Atomic<T>` is restricted as described under Atomic; `Fun<...>` has its own placement rules;
  `Unknown` exists only as an incomplete pointee.

## Core types

| Hexal | Meaning | C23 |
| --- | --- | --- |
| `Bool` | `false` or `true` | `bool` |
| `UInt8`, `UInt16`, `UInt32`, `UInt64` | exact-width unsigned | `uint*_t` |
| `Int8`, `Int16`, `Int32`, `Int64` | exact-width signed | `int*_t` |
| `Float32`, `Float64` | IEC 60559 binary32/64 | `float`, `double` |
| `Size` | target-sized unsigned length/index | `size_t` |
| `Byte` | transparent alias of `UInt8` | `uint8_t` |
| `Rune` | Unicode scalar value: `UInt32` excluding surrogates | `uint32_t` |
| `Nil` | zero-state `nil`; valid only as a union member | no stable foreign ABI |
| `EoS` | zero-state completion `eos`; valid standalone | no stable foreign ABI |

- `Byte` is the canonical spelling wherever the value is raw storage rather than a number:
  `Slice<Byte>`, `Array<Byte, N>`, `List<Byte>`, and byte-oriented parameters and results. `UInt8`
  is canonical wherever the value is an 8-bit integer participating in arithmetic, comparison, or
  conversion. Both remain the same canonical type; this rule governs spelling, not semantics.

- `Size` always lowers directly to the selected C compiler's `size_t`; that target decides width,
  range, alignment, and representation. Hexal has no Size width, no width assertion, and never
  rejects a conforming target for its `sizeof(size_t)`. Size remains canonically distinct from
  fixed-width integers.
- `Int`, `UInt`, `Float`, `Double`, `Char`, `Long`, `ISize`, `Void`, and `Strand` are not
  built-ins. `Strand` was removed with rune-based text; naming it reports a migration hint
  (`Strand` at `String<N>`), and the name is not reserved.
- Nil is valid only in a union containing at least one non-Nil member. Standalone Nil is invalid in
  aliases, bindings, parameters, results, members, payloads, collection positions, and generic
  arguments. The `nil` literal requires a contextual union containing Nil, except as a `print`
  argument, which is the sole position admitting standalone Nil.
- A function with no result omits `: Type` and uses bare `return` when explicit return is required.

### Contextual literals

- Integer literals remain exact until context selects an integer type; without context they default
  to Int32 and must fit. Floats use an expected Float32/64 or default to Float64.
- A direct negative literal is negated before range checking, allowing signed minima. `-0.0` is
  negative zero. Any negative literal in unsigned context, including `-0`, is invalid.
- Expected types reach untyped literals transitively through arithmetic and never retype a typed
  value. Comparisons and logical contexts provide no arithmetic expected type; untyped operands use
  the Int32/Float64 defaults.

### Aliases and objects

- `type Alias is T` is transparent: identical canonical type, representation, and operations, with
  no C typedef. Targets resolve in source order; recursive aliases are invalid. `T` is a single
  primary type expression: a trailing `|` is rejected with a pointer to `union ... end`.
- Objects are nominal, ordered inline values declared `type Name is struct member-list end`, where
  `member-list` is zero or more `[mut] name: Type` entries separated by commas (trailing comma
  allowed). Identical layouts remain distinct. Construction (`Name(member = value, ...)`) names
  every non-empty struct's member exactly once in any order; trailing comma is allowed.
  Initializers evaluate left to right in written order; the member's declared position governs
  storage layout only, never evaluation order.
- An empty struct (`type Marker is struct end`) constructs as `Marker()`; omitting `()` is rejected
  because a type name is not a value, and passing an argument is rejected. Every value of one empty
  struct type compares equal; distinct empty struct types remain nominally distinct and are never
  assignable or comparable to one another. It prints as `Name {}` and supports methods like any
  other object. C23 has no portable zero-sized object, so its generated struct carries one private
  `unsigned char` field; `size_of`/`align_of` both report 1.
- Identity is canonical and recursive, never derived from display names: same-named nominal types
  in distinct modules are distinct, identical layouts included, and constructed builtin generic
   types (pointer, nullable, function, Array, Slice, List, Dict, Task, Channel, Atomic, Stash, Pool,
   union) intern once per compilation and are shared by every module. `List<Int32>` written in two
  modules is one type, while `List<m.Point>` and `List<s.Point>` over same-named `Point` types are
  two.
- Direct and mutual by-value recursive layouts are invalid; pointer-indirect recursion is valid.
- Pointer member access auto-dereferences one object-pointer layer. `^pointer` explicitly
  accesses the whole pointee and is required for non-object pointees; a member actually named
  `value` resolves like any other member after that same auto-dereference.

### Pointers and nullability

- `Ptr<T>` is non-null, non-owning, and read-only through the pointer. `Ptr<mut T>` is non-null,
  non-owning, and writable through the pointer. Neither records whether its pointee is on the
  stack, heap, in an allocator, or in foreign storage; neither owns, retains, or automatically
  releases it.
- `@place` is the only address-taking form: writable places yield `Ptr<mut T>`, fixed places
  `Ptr<T>`. `@` accepts an optional `^` prefix chain (`@^pointer`) addressing the dereferenced
  place with its access mode.
- A directly returned `@` of a local binding of the returning function is rejected, including
  when the result widens to `Ptr | Nil`; parameter-reached, `self`-reached, and
  Heap/Stash/Pool-allocated pointers remain returnable. A
  local-rooted Ptr nested inside a returned object, ADT, union, or Array is not yet tracked;
  this covers the direct case only.
- `Ptr<mut T>` weakens implicitly to `Ptr<T>` at the outermost layer only. No upgrade or nested weakening.
- `^pointer` dereferences to a place, writable exactly for `Ptr<mut T>`. `^^pp` dereferences
  two layers by ordinary unary nesting. Nullability is explicit `P | Nil`; nullable data pointers must be narrowed
  with `== nil`, `!= nil`, or match before dereference. The null niche adds no tag or allocation.
- `Unknown` is incomplete and valid only behind `Ptr` in either mode. One pointer layer may erase to or recover
  from Unknown; Unknown cannot be stored or dereferenced by value.
- The unparameterized `String`, List, Dict, and Slice cannot be `Ptr` pointees. Each already carries
  its own aliasing and invalidation rules over borrowed or allocated storage, and a pointer to one
  would add a second aliasing layer with no defined semantics. `String<N>` is an inline value with
  no such rules and is a valid pointee. This is not a general handle exclusion:
  `Task<R>`, `Channel<T>`, `Mutex`, `Stash<T>`, and `Pool<T>` are shared by handle copy and are
  valid pointees.
- `Atomic<T>` cannot be a direct `Ptr` pointee. `Ptr<Atomic<T>>` and
  `Ptr<mut Atomic<T>>` are invalid type expressions.
- Pointers name one object. Ordering, subtraction, integer conversion, `bit_cast`, one-past values,
  increment/decrement, and compound assignment are unavailable. Inside `unsafe do ... end`,
  `Ptr<T>.offset(count: Size)` and `Ptr<mut T>.offset(count: Size)` return a new forward pointer
  advanced by `count`; pointer indexing `pointer[index]` is a place equivalent to
  `^pointer.offset(index)`; and `Ptr<T>.cast<U>()` / `Ptr<mut T>.cast<U>()` change only the pointee
  type while preserving the outer access mode. `offset` and indexing require a complete pointee and
  reject an Array, Slice, or List pointee; `cast` permits an erased or incomplete one. A nullable
  pointer must be narrowed before any of the three, and no implicit pointer cast is introduced.

### Functions and methods

- `fun` declares a function, not mutable storage. `Fun<(P1, P2) : R>` is a function-pointer type;
  omit `: R` for no result.
- A signature has at most one rest parameter, written `name: T...`, and it must be final. Inside the
  body the name is a fixed read-only `Slice<T>`; a call supplies at least the fixed parameters and
  checks every trailing argument in `T` context. Zero trailing arguments pass the canonical empty
  Slice. `T` must be a complete, shallow-copyable type valid in both Slice-element and
  function-parameter positions; a type containing `Atomic` is invalid. The invocation owns one
  compiler-created backing region, reclaimed automatically with no allocator and no element cleanup;
  elements are shallow copies and element ownership is not transferred.
- The rest-backed Slice is a non-owning descriptor over that region. It may be read (`length`,
  indexing, slicing, iteration), bound to a fixed local alias, or have an element copied out;
  returning or storing the descriptor or a derived Slice, binding it to a mutable alias, assigning
  it, placing it in an object/ADT/union/Array/List/Dict/Channel/Task/module storage position,
  passing it to a function, method, function value, or foreign declaration, taking an address inside
  the region, or capturing it in `defer`, `errdefer`, or `spawn` is rejected with `rest-backed Slice
  cannot escape its function invocation` (a mutable alias reports `rest-backed Slice requires a
  fixed local alias`).
- Rest mode is part of `Fun` identity: `Fun<(T...)>` and `Fun<(Slice<T>)>` are distinct and not
  assignable. `Fun<(T...)>` is called with zero or more explicit `T` values; the C ABI passes the
  same final pointer-and-length Slice for both. A declaration or `Fun` type with a parameter after
  `T...` reports `rest parameter must be final`; `...` after a call argument reports `spread
  arguments are not supported; pass explicit values`. Rest is not permitted on foreign declarations,
  constructors, compiler-owned operations, or `print`.
- Fun is valid as a binding, function parameter, parameter inside another Fun, function result,
  object member, ADT payload, Array/Slice/List element, Dict value, Task argument or result,
  Channel element, or union member. It is invalid as a `Ptr` pointee, a `@` target, a
  Dict key (function values have no equality or hash contract), and a direct heap-allocation
  type. Function declarations are not addressable. Every accepted position stores or copies one
  ordinary C function pointer; no position adds an environment, ownership operation, or
  allocation. An object may store a Fun value as an explicit dispatch table; the field holds one
  ordinary C function pointer and is called as `table.operation(args)` with no hidden receiver or environment.
- Calls require assignable arguments and either exact arity or, for a rest signature, at least the
  fixed parameter count. No-result calls are statements only. Results
  must match their declarations; result-producing bodies cannot fall through.
- Infallible commands with no payload return no value. Fallible commands with no success payload
  return `Nil | Error`.
- `method T.name(...)` declares a method on exactly one nominal struct type `T` (`type T is struct
  ... end`, or a transparent alias naming it, which creates no second method owner). No other
  receiver form exists: `Ptr<T>`, `Ptr<mut T>`, a nullable type, a union, a primitive, a builtin
  generic type, or a non-struct nominal type is rejected with `method receiver must be a struct
  type; got <type>`. `T` must also be shallow-copyable under the same classification an ordinary
  value copy uses; a struct that directly or transitively contains `Atomic<T>` (or another
  non-copyable value) is rejected with `method receiver must be shallow-copyable; got <type>`, even
  when the method is reached only through `Ptr<T>` or `Ptr<mut T>`. An explicit function taking
  `Ptr<mut T>` remains the non-copying way to operate on a non-copyable struct.
- `self` is an implicit fixed binding of type `T`, copied when the method is entered. Assigning to
  `self` or writing `self.field` is rejected; a method that needs to mutate copies `self` into a
  `mut` local first and returns the modified copy as an ordinary `T` value. A method call on a `T`
  value uses it directly. A call on `Ptr<T>` or `Ptr<mut T>` autoderefs exactly one pointer layer,
  copies the pointee, and invokes the same value-receiver method on that copy; neither pointer mode
  grants mutation of the original, and a nullable pointer must be narrowed first. More than one
  pointer layer is never implicitly dereferenced for method dispatch. In-place mutation through a
  pointer is expressed with an explicit function taking `Ptr<mut T>`, not with a method.
- One method name exists at most once per struct. It cannot equal a member name or be extracted as
  a function value. Only the struct's defining module may declare its methods; an imported struct
  may call exported methods but cannot receive local ones.
- `receiver.name(arguments)` resolves to a method first. Only when no method named `name` exists,
  and the receiver's type has a member `name` of an exact or nullable `Fun<...>` type, does the
  call resolve to an indirect call through that member instead, with the member's signature
  governing arity and argument types; a nullable member must be narrowed before the call. This
  needs no precedence rule beyond the existing one: a member and a method already share one
  namespace, so a type cannot declare both under the same name. A member matching the name but not
  `Fun<...>` is rejected with `member <name> is not callable; its type is <type>`, distinct from
  `<type> has no method named <name>` when the name is neither a method nor a member.
- There is no overloading, default/named/variadic argument syntax, static method, or closure.

#### Anonymous function literals and local literal bindings

- `fun (p1: T1, p2: T2): R do ... end` is a non-capturing function value with type
  `Fun<(T1, T2) : R>`; omitting `: R` gives `Fun<(T1, T2)>` and forbids a value-returning `return`.
  A generic literal declares type parameters between `fun` and its signature: `fun<T>(value: T): T do ... end`.
  The literal is a postfix base: a same-line call suffix invokes it directly, valid only where an
  expression is expected, never as a call statement. It declares no source name and can never be
  named in an export block.
- `fun name(...) do ... end` at statement position (inside a function or method body, or nested
  inside a branch, loop, or bare block) is rejected: Syntax Error, named function declarations are
  only valid at module scope. A function declared at local scope is not a module-level declaration
  and can never be named in an export block either.
- An inferred fixed declaration (`let name = ...`) whose initializer is directly a function literal,
  after stripping only grouping-only parentheses, behaves differently by scope. At module scope it
  is declaration sugar over the same function form as a named declaration: it emits the helper
  function and no function-pointer storage, is fixed and self-recursive, participates in forward
  calls and mutual recursion with every other module-level function and method exactly like a named
  declaration, and is accepted in a declaration-only imported module. Its bound name is an ordinary
  module-level function name and may be named in the export block exactly like a named declaration.
  At local scope the same syntax is
  ordinary source-ordered runtime data: fixed by default, mutable with `mut`, receives no
  self-recursion name, and cannot be called before its own declaration is reached in its enclosing
  block. A written type, a call or other suffix on the initializer, or a binding initialized from an
  existing function value are ordinary runtime data with no self-recursion name at either scope.
- Module-level named functions, module-level direct inferred fixed literal declarations, and
  anonymous literals share one signature, body, return-flow, defer, and generic-specialization
  implementation; they differ only in how their own name and result are bound. Every module-level
  function and method signature is collected before any body is checked, which is what makes
  forward calls and mutual recursion between module-level declarations resolve regardless of
  source position.
- An anonymous literal, and a local direct inferred fixed literal binding, are checked in a fresh
  closed scope: each may use its own parameters, bindings it declares, every module-level function
  and method regardless of source position, and Fun values it receives or declares. It cannot read
  an enclosing local, parameter, `self`, or root Fun/data binding; no environment, capture, or heap
  allocation is generated for this. A mutable receiving binding at module scope cannot supply a
  stable self-recursion identity, so referring to it from the literal's own body remains an invalid
  capture.
- A local direct inferred fixed literal binding is visible from its declaration onward in its
  containing lexical block, including from later local declarations in that same block, and is
  hidden outside the block, exactly like any other local value binding. Two same-named local
  declarations in disjoint blocks are distinct.
- An anonymous literal or a module-level direct inferred fixed literal declaration may itself
  declare generic type parameters. Its own parameter names must be distinct from every generic
  parameter active in an enclosing generic function or method; redeclaring an enclosing name is a
  duplicate-parameter error, while an unshadowed enclosing parameter remains usable in the inner
  signature and body. Every open generic function, module-level or anonymous, has a compiler-owned
  template identity distinct from its source name. Contextual specialization (an exact expected
  `Fun<...>` type, or a call's own arguments) applies to a generic literal exactly as it does to a
  named generic function; an inferred local declaration whose initializer is a generic literal has
  no open-template mechanism at local scope and is rejected.

### Generics

- User parameters are types only. Compiler-owned `Array<T, N>` uses a positive integer literal N.
- Specializations are invariant and keyed by declaration identity plus canonical arguments; repeated
  requests reuse one. Only reachable concrete specializations emit C; there is no erasure or runtime
  generic representation.
- Explicit type arguments must be complete. Otherwise inference uses typed arguments, expected
  result, and initializer fields; conflicts or unresolved parameters are errors.
- A balanced `<...>` is generic syntax only when immediately followed by call arguments (`List<Int32>(heap)`)
  or by a qualified member call (`Result<Int32, String>.Ok(value)`). Otherwise `<`, `>`, and `>>` are
  operators.
- A generic function value needs an exact expected Fun type. Generic methods inherit receiver
  arguments and infer or explicitly receive their own.
- An open generic body is checked once at declaration, after the defining module's complete
  signature collection, with each type parameter bound to its existing placeholder, and is fully
  rechecked after substitution. Each diagnostic condition is classified independently: a condition
  runs at declaration when substitution cannot change whether it applies, and is deferred to
  specialization only when some substitution could make it succeed. Name resolution, independent
  typing, known-callee arity, control flow, mutability of places, generic-parameter use, and local
  cleanup facts are checked at declaration; operators, member and method access on a dependent
  receiver, assignment between a dependent and a different type, equality/ordering/printing/hashing
  and placement of a dependent type, conversions involving a dependent type, nested specialization
  with dependent arguments, and the callability and arity of a dependent callee are deferred. A
  dependent type defers only the conditions it participates in: a deferred operation still checks
  its independent subexpressions, such as an unknown argument name. A template that fails
  declaration checking is unavailable for specialization, so its diagnostic is not repeated per
  specialization. Checking is diagnostic-only: an unused generic changes no generated artifact and
  consumes no binding or helper ordinal. Same-argument recursive specialization is allowed;
  argument-changing recursive cycles are rejected.
- A qualified generic type is `Alias.Name<Arguments>`, valid in an annotation, as a struct
  construction (`Alias.Name<Arguments>(...)`, or `Alias.Name(...)` when the arguments are inferred),
  and as the receiver of an exported generic method. It resolves exported types only, with the same
  arity and visibility diagnostics a non-generic qualified type already has. A generic exported
  template specializes in its defining module's retained context, so its signature, body, and
  provenance are resolved there regardless of which module requests the specialization; a private
  defining-module name inside the body therefore resolves normally. Its concrete specializations are
  owned by the defining module: generated linkage is external (never `static`) whenever an importer
  can reach the specialization, and the defining module's artifact carries the requested
  specializations, so that artifact is deterministic for a given source map and specialization-demand
  set rather than for its source alone.

### Structural unions

- A union holds exactly one active member; injection is implicit and allocation-free. Unions are
  flattened, duplicate-free, structural, and order-independent. Written order only chooses among
  contextual initializer candidates.
- A union contains at least two distinct canonical members. A written union must name each canonical
  member exactly once: a member repeated after alias resolution and generic substitution is an error
  naming the later member, so `Int32 | Int32` and `A | Int32` where `type A is Int32` are both
  invalid, and a written union is never an alias for a surviving member. Distinct members are then
  flattened, canonically ordered, and interned as one structural identity. Nil is valid only as one
  member of a union satisfying this rule.
- Widening is allowed only when every source member fits the destination; implicit narrowing and
  declaration-time union inference do not exist.
- `is` tests an exact active member. Narrowing applies to direct local reads; assignment or writable
  address escape invalidates it.
- `is Nil` is invalid, and `T | Nil` also rejects `is T`; use `== nil`/`!= nil`. Larger nullable
  unions may test non-Nil members, and match type patterns may name Nil.
- A pointer type is exactly Ptr or Fun after transparent-alias resolution. A union of
  exactly Nil and one pointer type uses the null-pointer niche without a tag. All other unions,
  including handle-plus-Nil unions, use the general tag-plus-inline-payload representation. Member
  operations require narrowing.
- Union equality requires identical canonical union types and equality-capable members; ordering is
  unavailable. Members may be any storable value. Atomic and Unknown cannot be members.

### Algebraic data types and match

- An ADT is a nominal closed sum declared `type Name is union | Variant [as member-list end] ... end`,
  with at least one variant; `Identifier | Identifier { | Identifier } end` is shorthand for an
  all-unit ADT of the same variant names and carries no separate identity, validation, or generation
  rules from the long form. A record variant's payload (`as member-list end`) requires at least one
  field, which is fixed (never `mut`).
- Every variant, unit or record, constructs as a call: `Owner.Variant()` for a unit variant,
  `Owner.Variant(field = value, ...)` for a record variant, naming every payload field exactly once
  in any order. Omitting `()` on a unit variant is rejected because a type name is not a value. An
  imported ADT's variant is `Alias.Adt.Variant(...)`; the short `Alias.Variant(...)` form is not a
  spelling.
- Direct by-value recursion is invalid; pointer-indirect recursion and generic
  specialization are valid.
- `match` is an expression and evaluates its scrutinee once. Value mode matches `true`/`false` and
  scalar literals: an integer or byte literal with an optional leading minus over an
  integer-like scrutinee (`Int8`..`Int64`, `UInt8`..`UInt64`/`Byte`, `Size`), plus `eos`
  over an `EoS` scrutinee. A scalar literal is typed contextually to the scrutinee type through the
  ordinary literal path; an out-of-range literal is rejected at the pattern, and a repeated constant
  after contextual typing is a duplicate. An integer-like domain is open, so a final `else` is
  required. `EoS` is a closed singleton, so `| eos` is exhaustive and a following `else` is
  unreachable. Float and text scrutinees are not value-mode domains.
  Type mode (`match value is`) matches exact complete types, individual union members, Nil, or ADT
  variants; a union type itself is not one pattern. A dotted pattern is neutral syntax resolved by
  scrutinee domain: against an ADT scrutinee it denotes that ADT's variant, named through a local
  owner (`Adt.Variant`), an import alias (`Alias.Adt.Variant` for an imported ADT), or the same
  alias for a locally visible ADT; against any other scrutinee it denotes the import-qualified type.
  Coverage is canonical type identity, never a short name: each canonical union member, each ADT
  variant, or the one exact non-union type.
- Arms are `| pattern then expression`; optional final `else` is catch-all. Match is exhaustive;
  duplicates and patterns unable to match remaining values are errors, and a final `else` is an
  error when no value remains. Arms run in source order.
- Arm result types agree unless an expected result accepts every arm. A named scrutinee narrows only
  inside its arm; ADT arms expose only that variant's payload.
- Unparenthesized `|` starts another arm. Bitwise-or scrutinees/results require parentheses. An `is`
  following the scrutinee marks type mode; a scrutinee containing `is` requires parentheses.

## Numeric conversions and operators

### Lossless widening

Typed numeric values widen implicitly only when every source value is exactly representable. Size
has no widening edges: no fixed-width integer or float implicitly converts to Size, and Size does
not implicitly convert to any fixed-width integer or float, because no conversion is lossless on
every conforming target. Identity `Size -> Size` remains implicit. The table lists fixed-width
destinations only. `none` means no fixed-width destination.

| Source | Fixed-width destinations excluding identity |
| --- | --- |
| `Int8` | `Int16 Int32 Int64 Float32 Float64` |
| `Int16` | `Int32 Int64 Float32 Float64` |
| `Int32` | `Int64 Float64` |
| `Int64` | none |
| `UInt8`/`Byte` | `UInt16 UInt32 UInt64 Int16 Int32 Int64 Float32 Float64` |
| `UInt16` | `UInt32 UInt64 Int32 Int64 Float32 Float64` |
| `UInt32` | `UInt64 Int64 Float64` |
| `UInt64` | none |
| `Float32` | `Float64` |
| `Float64` | none |

- Widening applies to initialization, assignment, arguments, returns, fields, collection insertion,
  and binary common-type selection.
- `Size` with any distinct numeric type has no implicit binary common type; only a Size/Size binary
  operation is implicit. An untyped non-negative integer literal may be contextually typed as Size
  (literal typing, not a conversion); a literal whose fit depends on the C target emits a C
  `static_assert(value <= SIZE_MAX, ...)`. Negative literals remain invalid in unsigned context.
- Explicit `value.to<Size>()` and `size.to<T>()` are the portable conversion routes and preserve
  the checked-conversion contract: target-independent failures are diagnosed by the checker;
  target-dependent constants are guarded by a generated C `static_assert`; dynamic out-of-range
  values trap before casting. Canonical identities remain distinct.
- Binary numeric operations choose the unique least type losslessly reachable from both operands.
  Surrounding result context does not change that choice.

### Explicit conversion

- `value.to<T>()` is the only explicit scalar conversion; T is mandatory and the call has no value
  arguments. Identity conversions are no-ops and Byte canonicalizes to UInt8.
- Constants outside the destination domain fail compilation. Dynamic invalid values trap before an
  unsafe C conversion.
- Integer conversion preserves the mathematical value. Integer/float and float/float round nearest,
  ties-to-even; finite overflow traps. Float/integer truncates toward zero then checks range; NaN and
  infinities are invalid.
- Bool/numeric and pointer conversions are invalid. Wrapping, saturating, unchecked,
  destination-named, and mode-selecting conversions do not exist.
- `bit_cast<T>()` reinterprets same-width bits; it is not a value conversion.

### Operators

- Integer `+`, `-`, `*`, unary `-`, and left shift wrap modulo width with defined two's-complement
  results. Constant folding uses the same rule. Unary `-` rejects typed unsigned values.
- Integer division truncates toward zero; remainder follows the dividend sign. Evaluated known zero
  divisors are compile errors; dynamic zero traps. A signed type's `MIN / -1` yields MIN and
  `MIN % -1` yields zero.
- Floating arithmetic follows IEC 60559; `%` is integer-only and NaN comparisons follow IEC rules.
- Bitwise operations accept fixed integers, excluding Size (whose width follows the target),
  Bool, pointers, aggregates, and managed values. Shift counts must be `0..width-1`; bad constants
  fail and bad dynamic counts trap. Signed right shift is arithmetic, unsigned zero-filling.
- `bit_cast<T>()` supports equal-width fixed integers and Float32/64, excluding pointers, Size,
  and aggregates. Fixed integers provide `to_le_bytes()`/`to_be_bytes()` and
  `T.from_le_bytes(array)`/`T.from_be_bytes(array)` through exact `Array<Byte, N>`.

### Equality, ordering, and truthiness

- Numeric comparison uses the lossless common type. Other comparisons require identical canonical
  types, except that any two text operands (`String` and `String<N>` of any capacity) compare
  with each other. Bool and EoS compare by value; pointers by identity; text by its bytes, so
  equal bytes are equal regardless of form or capacity and canonically equivalent but
  byte-different text is unequal; objects by members; ADTs by tag/payload; unions by member; Array/Slice/List by length then elements.
- `== nil` and `!= nil` test whether a union's active member is Nil. They require a union containing
  Nil, are the only Nil comparison, and read no payload. Nil has no standalone value to compare.
- Functions, allocators, and Dicts have no equality. An aggregate is comparable only when all
  recursively compared components are.
- Ordering exists only for numeric scalars and text, and any two text forms order against each
  other. Duration, Instant, and WallTime
  additionally compare and order by value against the same type, as defined under Time; they have
  no aggregate equality. Text uses unsigned-byte
  lexicographic order with shorter prefix first.
- Only `false` and `nil` are falsey. Truthiness applies to conditions and `!`, `and`, `or`; it is not
  Bool conversion or union narrowing. Logical operators return Bool and short-circuit left-to-right,
  while both operands must still be valid expressions.

## Control flow and cleanup

- Every structured body opens with an explicit delimiter: function, method, `while`, and
  `for` bodies open with `do`; `if` and `elseif` bodies open with `then`; `else` is itself the opener;
  match arms open with `then`. All forms end with `end`. `break` and
  `continue` target the nearest loop.
- Branches and loop iterations are scopes. Locals may shadow outer names; assignments may reach
  accessible outer mutable bindings.
- `is`/nil facts follow control flow. A branch-established fact survives afterward only on the sole
  continuing path when every alternative terminates with context-valid `return`, `break`, or
  `continue`. Assignment or writable address escape invalidates `is` narrowing as defined under
  Structural unions.
- Every continuing path in a result-producing function must return. A loop is always treated as able
  to fall through, including `while true`; break/continue never satisfy a return requirement.
- `return` at entry-module scope exits the complete entry module after active root defers and
  records one `UInt8` process status. It is invalid at top level in an imported module. `return
  expression` requires exact `UInt8` with no implicit conversion; `return` without an expression and
  entry-module fallthrough both record zero. The status is evaluated once before any defer runs.
- A root `return` under `if`, `while`, or `for` exits the program body and is a context-valid
  terminating statement for `is`/nil flow facts, exactly like a function `return`. A `return` inside
  a function or method retains that declaration's ordinary result contract. `try` and `errdefer`
  remain invalid at entry-module root because the root has no Error result.
- The generated entry adapter is emitted when `std/program.arguments()` or
  `std/program.executable_path(heap)` is reachable. Executable-path demand runs the native
  bootstrap and then `uv_setup_args` exactly once before the first query; argument demand alone
  selects no libuv. Windows keeps `int main(void)`; POSIX widens to `int main(int argc, char **argv)`,
  and host-neutral output spells both under `#if defined(_WIN32)`.
- `defer expression` registers cleanup in the current scope. Actions run in reverse registration
  order on fallthrough, return, break, or continue. A direct call captures callee, receiver, and
  arguments at registration; other expressions evaluate on exit.
- `errdefer` uses the same rules but runs only while the function exits with active Error. It shares
  reverse order with defer on Error exit and is discarded otherwise.
- Cleanup result values are discarded. Process traps need not run cleanup. Errors defines `try` and
  `errdefer` validity.

### `for ... in`

- Sources and binder forms are exact:

| Source | Binders | Binder types and order |
| --- | ---: | --- |
| Array, Slice, List, String, `String<N>` | 1 | value |
| Array, Slice, List, String, `String<N>` | 2 | `index: Size`, value |
| Dict | 2 | key, value |
| Dict | 3 | `index: Size`, key, value |

Every other source/arity combination is invalid.

- Text iterates as `Byte` (storage units), `Rune` (decoded scalars), or `Grapheme` (extended
  grapheme clusters); Dict order is unspecified.
- A binder may carry a written type, `for name: Type in source`, on any binder. The written type
  must equal the type the binder would take (`Byte` and `UInt8` are the same type), and a
  disagreement is rejected naming both types. The value binder over text is the one binder that
  must be annotated, because text has no default element type: `for b: Byte in text`,
  `for r: Rune in text`, or `for g: Grapheme in text`. The index binder is `Size`. A
  `List<X | Y>` element binder is the whole
  union, so annotating it with one member is rejected.
- Finite-source traversal boundaries are captured once. Array places iterate in place; temporary
  Arrays materialize once and an inline text source is read from a copy taken before the loop, so
  reassigning the text inside the body changes neither the bytes read nor their count; handles
  copy shallowly.
- Binders are fresh immutable copies each iteration and names in one header are distinct. Nullable or
  union sources must first narrow to one iterable type.
- Array and Slice traversal has a fixed boundary. Element replacement is valid; there is no structural resize operation.
- List traversal captures the source's structural version. `push`, `pop`, `clear`, `free`, and any operation that changes storage or length invalidate the traversal. A `push` that would extend the traversal traps with `collection modified during iteration` rather than extending or terminating.
- Dict traversal captures the source's structural version. `insert`, replacement, `remove`, `free`, and any bucket/topology change invalidate the traversal.
- Mutation through any alias observes and updates the same version because copied handles refer to the same collection state.
- A traversal checks its version immediately before each iteration body with `if (version != captured) hex_runtime_trap("[Runtime Error] collection modified during iteration\\n")`. No check is required at the loop increment; the next body's check covers the transition. When the checker proves that no operation in the traversing scope or any reachable call can mutate the source (proven-safe elision), the check may be omitted.
- The version is a monotonic `Size` (`size_t`) counter incremented on every structural change; it wraps modulo `2^N` and a wrapped version that coincides with a live traversal's captured token is an accepted false negative.
- Freeing the traversed List or Dict, or an alias that refers to it, is always rejected while the traversal is active. Passing the traversed collection or an alias to an unproven call is rejected; the checker must not rely on a post-call version check after a possible free.
- A mutation after `break` or after the traversal's scope exits is valid when no separate lifetime rule rejects it. Nested traversals capture independent versions. Array/List element replacement remains valid.

## Errors

```text
type ErrorKind is union
    | NotFound
    | PermissionDenied
    | AlreadyExists
    | InvalidInput
    | InvalidPath
    | NotADirectory
    | IsADirectory
    | DirectoryNotEmpty
    | ReadOnly
    | Busy
    | Interrupted
    | Cancelled
    | TimedOut
    | Unsupported
    | ResourceExhausted
    | Closed
    | AddressInUse
    | AddressUnavailable
    | ConnectionRefused
    | ConnectionReset
    | ConnectionAborted
    | HostUnreachable
    | NetworkUnreachable
    | BrokenPipe
    | NotConnected
    | Other as header: String<128> end
end

Error(kind: ErrorKind, message: String<256>) -> Error
Error.header()                         -> String<128>
ErrorKind.header()                     -> String<128>
```

`Error` is entirely inline: its message is a `String<256>` and an `ErrorKind.Other` header is a
`String<128>`, so an `Error` owns nothing, needs no cleanup, and cannot allocate when built. It is
about 432 bytes on `x86_64-linux-gnu` (`size_of<Error>()`), and `T | Error` results move that much.
`Error` is one concrete type, not generic over these capacities.

- The two bounded positions, the `message` argument of `Error(kind, message)` and the `header`
  argument of `ErrorKind.Other`, are the only places text of any form converts to a fixed capacity
  implicitly. Each accepts a `String`, a `String<M>` of any `M`, or a literal. A literal longer
  than the bound is a compile error (`Error message literal exceeds 256 UTF-8 bytes`,
  `ErrorKind.Other header literal exceeds 128 UTF-8 bytes`); a computed text that does not fit
  traps (`[Runtime Error] Error message exceeds 256 bytes`, `[Runtime Error] ErrorKind.Other header
  exceeds 128 bytes`) and is never truncated. Every other bounded position keeps the exact-type
  rule.

- `ErrorKind` is a protected compiler-owned nominal type; it cannot be redeclared or shadowed. Every
  unit variant is constructed with the ordinary call shape (`ErrorKind.NotFound()`); `Other` is the
  only payload variant and stores one caller-supplied `header: String<128>`. `ErrorKind` is complete,
  finite, copyable, equality-comparable, non-orderable, and no more Dict-key eligible than an
  equivalent ADT. `==` and `!=` compare variant identity; two `Other` values are equal only when
  their header bytes are equal.
- `ErrorKind.header() -> String<128>` and `Error.header() -> String<128>` derive the same display header
  without allocation: the fixed text below for a unit variant, or the stored header for `Other`.
  Header text is presentation only; classification and equality never compare it.
- A type-mode `match` whose scrutinee is exactly `ErrorKind` requires a final `else`, even when the
  written arms already name every variant the compiler currently knows: `match on ErrorKind requires
  a final else arm`. The variant set may grow with later native capabilities; this is
  source-compatible because of that required `else`. Matching narrows only the selected variant;
  `Other`'s payload is read only through `header()`, not through a narrowed match binding.
- Fixed unit headers: `NotFound` "not found", `PermissionDenied` "permission denied",
  `AlreadyExists` "already exists", `InvalidInput` "invalid input", `InvalidPath` "invalid path",
  `NotADirectory` "not a directory", `IsADirectory` "is a directory",
  `DirectoryNotEmpty` "directory not empty", `ReadOnly` "read only", `Busy` "busy",
  `Interrupted` "interrupted", `Cancelled` "cancelled", `TimedOut` "timed out",
  `Unsupported` "unsupported", `ResourceExhausted` "resource exhausted", `Closed` "closed",
  `AddressInUse` "address in use", `AddressUnavailable` "address unavailable",
  `ConnectionRefused` "connection refused", `ConnectionReset` "connection reset",
  `ConnectionAborted` "connection aborted", `HostUnreachable` "host unreachable",
  `NetworkUnreachable` "network unreachable", `BrokenPipe` "broken pipe",
  `NotConnected` "not connected".
- Protected nominal `Error` has fixed immutable fields `file: String`, `line: Size`, `column: Size`,
  `kind: ErrorKind`, `message: String<256>`. `file` is a compiler-injected static literal handle
  that is never freed. There is no stored `header` field.
- `Error(kind, message)` is the only constructor and injects the current module's logical
  source key plus one-based line and UTF-8 byte column. Propagation preserves the location. A
  non-ErrorKind first argument is rejected: `Error requires ErrorKind as
  its first argument; use Error(ErrorKind.Other(header = ...), message)`.
- Classification compares `error.kind`, never `error.header()` display text. Two Errors differing
  only in kind compare unequal even when their headers are byte-equal.
- Runtime-constructed Errors use one portable `ErrorKind` classification: libuv-backed capabilities
  map their native status through one common mapper (RFC 0180); IO's native POSIX/Windows codes map
  through their own small front-end that agrees with it on shared conditions; every native and
  non-native producer's message remains a fixed, allocation-free operation string that never embeds
  a native number.
- Fallible functions return structural unions containing Error; there are no exceptions or hidden
  result channels. An Error is a self-contained value: copying it copies its message and header, and
  nothing must outlive it.
- A try expression or try statement requires exactly one Error member and at least one success
  member. It evaluates once and returns Error unchanged. A try expression yields the normalized
  success value/union; a try statement discards it. Neither catches traps.
- `try` and `errdefer` are valid only inside a function whose declared result accepts Error; both are
  invalid at root scope. `try` is additionally invalid inside any cleanup action.

## Allocation and lifetime

```text
Heap() -> Heap
Heap.allocate<T>(initial: T) -> Ptr<mut T>
Heap.allocate_aligned<T>(initial: T, alignment: Size) -> Ptr<mut T>
Heap.free<T>(pointer: Ptr<T>) -> no value
Heap.free<T>(pointer: Ptr<mut T>) -> no value
```

- `Heap()` selects the default allocator without runtime allocation; Heap operations are
  thread-safe. There is exactly one default allocator: Heap is a value token with no runtime
  state, and no Heap value selects different storage from any other.
- `h.allocate<T>(initial)` allocates and initializes one complete finite T, returning non-owning
  `Ptr<mut T>`. T must be valid in HeapAllocation; direct Atomic allocation is invalid. Failure or
  unrepresentable size traps.
- `h.allocate_aligned<T>(initial, alignment)` has `allocate`'s T eligibility, initializer typing,
  result, cleanup, and freed-state behavior, and additionally requires `alignment` to be Size,
  non-zero, and a power of two. `alignment` is a requested minimum: the effective alignment is
  `max(alignment, align_of<T>())`, so requesting less than T's natural alignment is valid and
  still returns storage correctly aligned for T. Requesting exactly the natural alignment differs
  from `allocate<T>` in no observable way. Receiver, `initial`, then `alignment` are each
  evaluated exactly once in that order; `initial` is fully evaluated before any alignment trap,
  and allocation occurs only after validation succeeds.
- A compile-time-constant `alignment` of zero or a non-power-of-two is rejected: `Type Error:
  alignment must be a non-zero power of two; got <value>`. A non-Size alignment is rejected with
  `Type Error: allocate_aligned requires Size; got <type>`; wrong arity with `Type Error:
  allocate_aligned expects 2 arguments (initial, alignment)`; a missing or repeated type argument
  with `Type Error: allocate_aligned requires exactly one type argument`. A dynamic invalid
  alignment traps with `[Runtime Error] invalid allocation alignment` before the allocator is
  reached. Allocation failure traps with `[Runtime Error] heap allocation failed`, as `allocate`
  does.
- `h.free(ptr)` accepts `Ptr` in either mode, and releases an aligned allocation identically to an
  ordinary one; `free` takes no alignment argument. Every Heap value selects the same allocator, so
  no Heap can be the wrong Heap and none is compared. That is a statement about Heap identity only:
  a pointer the checker proves was allocated by a Stash or a Pool is rejected by `h.free`, and a
  Heap- or Stash-allocated pointer is rejected by `Pool.free`.
- Heap-backed library values — `String`, `List`, `Dict`, `Channel`, `Mutex` — receive their Heap
  explicitly; their allocation and cleanup never choose a hidden allocator.
- Allocator-owning types are the exception, and are explicit about it: `Stash` and `Pool` construct
  and destroy against the one default allocator with no Heap argument, because a stateless default
  Heap makes passing one inert ceremony rather than a choice. The rule above governs values
  allocated *from* an allocator, not the allocators themselves.
- Freeing a container releases only its own header/backing region. It never frees allocations its
  elements or nested handles refer to. Referenced owned allocations require cleanup before loss of
  reachability, exactly once per distinct allocation rather than per alias or slot.
- The shallow rule applies at every depth. Replacing/dropping the last handle may leak; freeing one
  alias dangles all others. No runtime metadata records allocation state, so a repeated or invalid
  release has no guaranteed diagnostic; only the compile-time rules below reject cleanup misuse.
- Cleanup misuse is rejected at compile time wherever a local analysis decides it. Four are
  rejected: freeing a pointer traceable to `@`, freeing a local binding already freed on every
  path to that point, reading through one, and releasing a pointer through an allocator that
  provably did not produce it (`Heap.free` of a Stash or Pool allocation; `Pool.free` of a Heap
  or Stash allocation, or of a pointer from a different Pool). Misuse requiring interprocedural
  analysis, or a pointer whose allocator or identity is unknown, is never rejected — a pointer
  arriving as a parameter, read from a member or collection, or copied from a foreign source is
  not classified, and leaks are not diagnosed. An undecided case is always accepted.

### `Stash<T>` and `Pool<T>`

```text
Stash<T>() -> Stash<T>
Stash<T>.allocate(initial: T) -> Ptr<mut T>
Stash<T>.reset() -> no value
Stash<T>.destroy() -> no value

Pool<T>(capacity: Size) -> Pool<T>
Pool<T>.allocate(initial: T) -> Ptr<mut T>
Pool<T>.free(pointer: Ptr<T> | Ptr<mut T>) -> no value
Pool<T>.destroy() -> no value
```

- Stash and Pool are independent allocator roots, not Heap-backed library values: both
  constructors take no Heap argument and always build on Heap's own allocation primitives
  internally. The no-hidden-allocator rule above applies only to Heap-backed library values
  (String, List, Dict, Channel, Mutex); those keep their exact Heap signatures and reject a Stash
  or Pool argument.
- `Stash<T>` grows as needed and allocates only one canonical T; `allocate` takes no explicit type
  arguments and its result is always `Ptr<mut T>`. A union-typed Stash accepts each union member
  through ordinary contextual injection but still returns a pointer to the union, not to the
  injected member. T must be complete, finite, and valid for HeapAllocation, exactly like Heap's
  own eligibility rule; direct Atomic and function elements are invalid.
- A Stash allocation cannot be individually released: `stash.free(pointer)` is rejected with
  "Stash allocations are released by reset or destroy". `reset()` invalidates every allocation made
  since construction or the previous reset, retaining and reusing the underlying storage without
  zeroing it; `destroy()` invalidates every remaining allocation and releases all storage. The
  checker rejects a reset/destroy followed by a use through the same locally tracked allocation;
  aliased, escaped, parameter-reached, member-reached, and collection-reached pointers follow
  the undecided-case policy above and receive no diagnostic.
- `Pool<T>` owns a fixed number of reusable slots for one concrete T, sized by a runtime `Size`
  capacity. A constant zero capacity is rejected statically ("Pool capacity must be positive"); a
  dynamic zero capacity traps, as does exhaustion. `allocate` accepts exactly T; `free` accepts
  `Ptr` of exactly T in either mode and validates that the address names an aligned, currently live slot in
  that exact Pool, trapping otherwise. A pointer the checker proves came from a Heap or Stash is
  rejected with the real source allocator's message; a pointer directly traceable to another Pool
  is rejected before generation; unknown provenance reaches the runtime address check.
- `destroy()` requires every Pool slot to have been freed first: a directly tracked live slot is
  rejected at compile time ("Pool cannot be destroyed while a locally tracked slot is live");
  otherwise a non-empty destroy traps at runtime. Pool release runs no cleanup for stored T; the
  programmer cleans any resources T holds before releasing a slot.
- Stash and Pool handles are pointer-sized, shallow-copyable aliases, exactly like List, Dict, and
  Mutex; `mut` controls only binding reassignment, not allocator behavior, and neither type is
  equality-, ordering-, hashing-, or print-eligible. Stash and Pool are not thread-safe: shallow
  copying a handle adds no synchronization, and a cross-task conflict the checker cannot prove
  locally receives no diagnostic.

## Collections

### Common rules

- Signature metavariable Integer means any Hexal integer type. `place<T>` and
  `read-only-place<T>` describe writable and read-only expression results; they are not source types.
- Lengths, capacities, indices, and normalized bounds use Size. Index arguments may be any integer
  and are normalized with compile-time rejection or dynamic traps.
- Ranges are zero-based and end-exclusive. `length`, indexing, and `slice` use the
  same bounds where available. No type has `at` or `is_empty`: `receiver[index]` and
  `receiver.length() == 0` are their identical replacements.
- Array/Slice/List equality compares length then elements. No collection ordering; no Dict equality.

### `Array<T, N>`

```text
Array<T,N>.length() -> Size
Array<T,N>[index: Integer] -> place<T>
Array<T,N>.slice(start: Integer, end: Integer) -> Slice<T>
Array<T,N>.mut_slice(start: Integer, end: Integer) -> Slice<mut T>
```

- Fixed inline sequence; N is a positive integer literal. A contextual `[a, ...]` must contain
  exactly N elements, evaluated left-to-right.
- Assignment, arguments, and returns copy the inline region. Element writes require a writable Array
  place. Indexing is checked; `slice` returns a read-only Slice, `mut_slice` returns a writable
  Slice and requires a writable Array place.
- T follows general storability, including nested Arrays. Arrays free nothing; external-state
  elements copy only their references.

### `Slice<T>` and `Slice<mut T>`

```text
Slice<T>.from_pointer(pointer: Ptr<T> | Ptr<mut T>, length: Size) -> Slice<T>
Slice<mut T>.from_pointer(pointer: Ptr<mut T>, length: Size) -> Slice<mut T>
Slice<T>.empty() -> Slice<T>
Slice<mut T>.empty() -> Slice<mut T>
Slice<T>.length() -> Size
Slice<mut T>.length() -> Size
Slice<T>[index: Integer] -> read-only-place<T>
Slice<mut T>[index: Integer] -> writable-place<T>
Slice<T>.slice(start: Integer, end: Integer) -> Slice<T>
Slice<mut T>.slice(start: Integer, end: Integer) -> Slice<mut T>
```

- Non-owning copyable contiguous pointer-length descriptor. Copying a Slice copies only its
  pointer and length; all copies address the same elements. T follows general storability, and
  nested Slice descriptors are valid.
- `Slice<T>` permits element reads only. `Slice<mut T>` permits element reads and writes.
  Multiple aliases, including multiple writable aliases, are permitted; their correct use is the
  programmer's responsibility.
- `Slice<mut T>` weakens implicitly to `Slice<T>` at the outermost layer only. There is no
  upgrade and no nested weakening.
- `from_pointer` accepts only the exact non-null pointer types listed and does not validate the
  allocation's length, alignment, initialization, lifetime, provenance, or future validity.
  `empty()` and an empty slice of an empty List use null data plus zero length; no valid
  operation dereferences it.
- Slicing a temporary Array is rejected because no source place exists. Every other backing
  store — Array, List, String, allocation, or foreign region — is the programmer's
  responsibility: it must remain alive and unreallocated for every Slice use. Growing or
  freeing a List, freeing a String, or resetting/destroying an allocator invalidates Slices
  into its old storage. Such misuse is outside the Slice contract and may produce undefined
  behavior in generated C.
- Indexing traps unless `0 <= index < length` after Size normalization. Re-slicing traps
  unless both bounds form a valid half-open subrange; it cannot widen the represented range
  and preserves the receiver's access mode. Bounds checks validate only the descriptor
  length, never liveness of the backing storage.
- Slice has no storage-position restriction beyond the general rules: bindings, results,
  members, payloads, union members, Array/Slice/List elements, Dict values, function
  parameters/results, Task arguments/results, and Channel elements. It is invalid as a `Ptr`
  pointee. Equality compares length then elements; printing, truthiness, and iteration follow
  the collection contracts.

### `List<T>`

```text
List<T>(heap: Heap) -> List<T>
List<T>.length() -> Size
List<T>[index: Integer] -> place<T>
List<T>.slice(start: Integer, end: Integer) -> Slice<T>
List<T>.mut_slice(start: Integer, end: Integer) -> Slice<mut T>
List<T>.push(value: T) -> no value
List<T>.pop() -> T
List<T>.clear() -> no value
List<T>.free(heap: Heap) -> no value
```

- Growable allocated sequence. A fixed handle can mutate its List; `mut` only reassigns the handle.
  `slice` returns a read-only Slice; `mut_slice` returns a writable Slice with no `mut`-binding
  requirement. `pop` traps when empty; indexed access is bounds-checked.
- T follows general storability. Every operation copies/discards T shallowly, including String.
  Index assignment, `clear`, and `free` drop slots without freeing referents; free releases only List storage.
- Values read or popped are aliases. Each distinct referenced owned allocation requires exactly one
  cleanup before loss of reachability. Repeated aliases must not be freed per slot. Reverse defer
  order runs later-registered element cleanup before earlier-registered container cleanup.

### `Dict<K, V>`

```text
Dict<K,V>(heap: Heap) -> Dict<K,V>
Dict<K,V>.insert(key: K, value: V) -> no value
Dict<K,V>.get(key: K) -> V
Dict<K,V>.find(key: K) -> V | Nil
Dict<K,V>.contains(key: K) -> Bool
Dict<K,V>.remove(key: K) -> V
Dict<K,V>.length() -> Size
Dict<K,V>.free(heap: Heap) -> no value
```

- Open-addressing allocated dictionary. K is exactly Int32 or `String<N>` for any capacity; V
  follows List eligibility. The heap `String` is not a key, because a Dict stores its keys and
  `String` does not own its bytes (`dictionary key type String is not allowed: a Dict stores its
  keys, and String does not own its bytes; use String<N>`); any other key type reports
  `dictionary key type must be Int32 or String<N>`. Dicts keyed at different capacities are
  different types, and a key of another capacity is converted by hand
  (`dictionary key requires String<128>; got String<16>; use widen<128>()`). A literal key is
  measured against the key capacity at compile time. Missing get/remove trap; find returns Nil for
  a missing key; insert replaces.
- Keys and values copy shallowly. Reads/removal return aliases; replacement/free drop entries without
  freeing referents. Free releases only buckets/header. Overwriting the final reachable handle leaks
  its referent.
- Hashing is internal and infallible for supported keys. Equal values hash equally; a text key
  hashes and compares only its logical bytes (embedded NUL included, storage past `byte_length`
  excluded), so equal bytes are one key at any capacity. Algorithm, seed, and iteration order are unstable
  and unspecified; no source hash operation exists.

## Text

```text
String        heap allocated, variable size, owns its bytes
String<N>     inline value storage holding up to N bytes
```

`N` is a positive decimal integer literal from 1 to 4096 (`String<1_024>` and `String<1024>` are
one type). Any other spelling is rejected: `String capacity must be a positive integer literal`,
`String capacity 4097 exceeds the maximum of 4096`, `String takes at most one capacity argument`.
No name, expression, or generic parameter can stand for a capacity.

```text
String.length() -> Size                                       O(1)
String.rune_length() -> Size                                  O(n), decodes scalars
String.grapheme_length() -> Size                              O(n), segments clusters
String.grapheme_cursor() -> GraphemeCursor
String.casefold(heap: Heap) -> String | Error
String.normalize(heap: Heap, form: NormalizationForm) -> String | Error
String.bytes() -> Slice<Byte>                                 O(1)
String.byte_cursor() -> ByteCursor
String.rune_cursor() -> RuneCursor
String.slice(start: Integer, end: Integer) -> Slice<Byte>     O(1), byte bounds
String.copy(heap: Heap) -> String
String.concat(heap: Heap, other: Slice<Byte>) -> String | Error
String.free(heap: Heap) -> no value
String.from_bytes(heap: Heap, bytes: Slice<Byte>) -> String | Error
String.from_runes(heap: Heap, runes: Slice<Rune>) -> String | Error
String.interpolate(heap: Heap, template: InterpolationTemplate) -> String
String.c_pointer() -> Ptr<Byte>                               unsafe

String<N>.length() -> Size
String<N>.rune_length() -> Size                               O(n), decodes scalars
String<N>.grapheme_length() -> Size                           O(n), segments clusters
String<N>.grapheme_cursor() -> GraphemeCursor
String<N>.casefold(heap: Heap) -> String | Error
String<N>.normalize(heap: Heap, form: NormalizationForm) -> String | Error
String<N>.bytes() -> Slice<Byte>
String<N>.byte_cursor() -> ByteCursor
String<N>.rune_cursor() -> RuneCursor
String<N>.slice(start: Integer, end: Integer) -> Slice<Byte>
String<N>.copy(heap: Heap) -> String
String<N>.widen<M>() -> String<M>                             M >= N; infallible
String<N>.from_bytes(bytes: Slice<Byte>) -> String<N> | Error
String<N>.concat(left: Slice<Byte>, right: Slice<Byte>) -> String<N> | Error
String<N>.interpolate(template: InterpolationTemplate) -> String<N> | Error
```

`String<N>` has no `free` and no `c_pointer`. `to_string` does not exist, and `from_runes` is
heap-only: an inline destination converts through `String<N>.from_bytes`.

- `ByteCursor`, `RuneCursor`, and `GraphemeCursor` are copyable positions over one text:
  `has_next() -> Bool`, `next()` and `peek()` yielding the cursor's element (`Byte`, `Rune`, or
  `Grapheme`), and `offset() -> Size`. `offset()` is always a **byte** offset, on every cursor, and
  is the one unit `slice` takes. `next` and `peek` trap when exhausted, with `has_next` as the guard.
  `next` advances its binding, so the receiver must be a mutable binding; a `GraphemeCursor`'s
  `peek` caches its lookahead so a following `next` shares the single break-state advance, so it
  takes a mutable binding too. A copied cursor holds an independent position over the same bytes; a
  cursor is a view and dangles if the text it points at is freed, reassigned, or leaves scope. Only
  the Rune and Grapheme cursors select the private utf8proc adapter; byte stepping is index
  arithmetic.
- `Grapheme` is a byte range borrowed from the text it came from, spanning exactly one extended
  grapheme cluster. `Grapheme.bytes() -> Slice<Byte>` views its bytes and `Grapheme.rune_length() ->
  Size` counts its scalars. Like every borrowed range it dangles if the text it views is freed,
  reassigned, or leaves scope, so transient use inside the traversal that produced it is the
  intended shape. `Grapheme` and `GraphemeCursor` are not Dict-key eligible: a Dict stores its keys
  in the table, and a key that points at bytes the Dict does not own is a dangling key.
- `String.casefold(heap) -> String | Error` applies full Unicode case folding, so a multi-scalar
  expansion such as `ß` to `ss` is performed; it is not the same operation as `Rune.to_lower()`,
  which maps one scalar to one scalar. The transform copies the utf8proc result into one Hexal Heap
  allocation and releases the utf8proc `malloc` buffer on both the success and every failure path;
  that buffer is never reachable as a Hexal `String` and never reaches `Heap.free`. A failed
  transform is `InvalidInput` with message `Unicode transform failed`.
- `NormalizationForm` is the closed four-variant built-in enum `NFC | NFD | NFKC | NFKD`.
  `String.normalize(heap, form) -> String | Error` applies it under the same allocation contract as
  `casefold`. Unit variants are constructed call-shaped (`NormalizationForm.NFC()`) and matched
  without parens. Normalization is never implicit: `"é" == "e\u{301}"` is false, and the two remain
  bytewise unequal, hash differently, and are distinct Dict keys until a caller normalizes
  explicitly.

- Byte is UInt8. A byte literal contains exactly one printable ASCII byte or one of
  `\\ \' \n \r \t \0 \xHH`.
- A bare-quote literal is exactly one Unicode scalar value: the `Rune` type. Its escape set is the
  string set (`\\ \' \" \n \r \t \0 \u{HEX}`) and excludes `\xHH`; an empty body, more than one
  scalar, a surrogate, or a value above U+10FFFF is rejected. `Rune.value() -> UInt32` yields the
  underlying scalar and `Rune.utf8_length() -> Size` its encoded byte length (1..4); `Rune` is
  equality-comparable, ordered by scalar value, and valid as a match scrutinee, in `print`, and in
  interpolation. `Rune.from(value: UInt32) -> Rune | Error` is the type-level constructor that
  rejects surrogates and values above U+10FFFF at runtime with `InvalidInput`.
- The Tier 2 Rune surface reads utf8proc's tables: `is_lower()`, `is_upper()`, `is_alphabetic()`,
  `is_numeric()`, and `is_whitespace() -> Bool`; `to_lower()`, `to_upper()`, and `to_title() ->
  Rune` (simple one-scalar case mappings, not case folding); `display_width() -> Int32`; and
  `combining_class() -> UInt8`. The alphabetic, numeric, and whitespace predicates read the
  general category (Letter, Number, Separator).
- `UnicodeCategory` is the closed 30-variant built-in enum of Unicode general categories, named by
  the standard abbreviations (`Cn Lu Ll Lt Lm Lo Mn Mc Me Nd Nl No Pc Pd Ps Pe Pi Pf Po Sm Sc Sk So
  Zs Zl Zp Cc Cf Cs Co`), in `utf8proc_category_t` order. `Rune.category() -> UnicodeCategory`
  yields it. Unlike `ErrorKind`, the set never grows, so a type-mode `match` over `UnicodeCategory`
  is exhaustive without a final `else`. Unit variants are constructed call-shaped
  (`UnicodeCategory.Lu()`) and matched without parens (`| UnicodeCategory.Lu then`).
- Text is validated UTF-8 bytes: `length()` and `slice` count and bound by bytes on every form, so
  `"héllo".length()` is 6, `slice` is O(1) and legal on a byte that splits a sequence, and text
  storage never carries a character count. `rune_length()` and `grapheme_length()` decode: each is
  O(n) and counts Unicode scalars or extended grapheme clusters, so `"héllo".rune_length()` is 5 and
  a base letter plus a combining mark is one grapheme. Segmentation uses
  `utf8proc_grapheme_break_stateful`, which must see every adjacent scalar pair in order. Text is not
  indexable; `bytes`
  gives indexed byte access, `for b: Byte in text` iterates bytes, and `for r: Rune in text` decodes
  scalars. A slice is bytes, not text:
  feeding one back through `from_bytes` or `concat` validates it again.
- `String` is immutable UTF-8 behind a non-null pointer-sized handle holding the data pointer, the
  byte length, and the storage kind. Runtime values use one allocation with one trailing NUL that
  the length does not count, so `c_pointer()` yields a NUL-terminated string; literals use static
  storage. `String<N>` is a value of `byte_length` plus `N` bytes of storage (`size_of<String<31>>()`
  is 40 on `x86_64-linux-gnu`); it owns nothing, copies with its bytes, and needs no cleanup.
- `from_bytes` and `concat` are the validating boundaries: bytes become text only there, and
  validation covers the joined result, so a multi-byte sequence split across the two operands of
  `String<N>.concat` is accepted. Both forms return `| Error`. Capacity is checked before content,
  so input that is both too long and malformed reports the capacity failure. Malformed input is
  `InvalidInput` with message `invalid UTF-8 in string`; an inline overflow is `ResourceExhausted`
  with message `string exceeds capacity`. No failure allocates or yields a partial value. Heap
  `concat` appends to text that is already valid and validates only what it appends. Heap
  `interpolate` and `copy` cannot fail and return a plain `String`; heap allocation failure still
  traps.
- A string literal takes the form its context names: a `String<N>` context measures it in bytes
  and rejects it above N at compile time (`String<4> literal exceeds 4 UTF-8 bytes`); a `String`
  context or an untyped position gives the static-backed heap form. A literal converts to a union
  member by the first written member whose capacity holds it, so `String<16> | String<32>` holds a
  `String<16>` and `String<32> | String<16>` a `String<32>`; when none fits, `no member of ...
  accepts this expression`. A non-literal `String<M>` injects only into the member that is exactly
  `String<M>`.
- No implicit conversion exists between text forms in any position (binding, argument, return,
  member, payload, element). A mismatch names the explicit route: `; use widen<M>()` when the
  source fits, `; use String<N>.from_bytes(...) for a checked conversion` from `String`, and
  `; use copy(heap)` to `String`. The one exception is the bounded Error text described under
  Errors.
- `bytes()` and `slice()` on a `String<N>` need an addressable place (a binding, a member, a `mut`
  binding), since the slice would point into the value itself: on a temporary they report `a
  Slice cannot be rooted in a temporary String<N>`. The slice reads the current bytes, so keeping
  the place unassigned for the slice's use is the programmer's responsibility. No `Slice<mut Byte>`
  over text exists.
- Runtime String allocations require one matching free; all aliases then dangle. Literals must never
  be freed: a free the checker proves literal-backed is rejected, and any other literal-backed free
  traps at runtime. Runtime String storage records its ownership. Collection reads produce aliases
  without ownership transfer or lifetime protection.
- `String<N>` is a valid `Ptr` pointee (`Ptr<String<N>>`, `@place`, `^p = other`); the
  unparameterized `String` is not. Neither form has a C ABI mapping as a foreign parameter, result,
  global, or record field (`String<N> has no supported C ABI mapping for target <target>`), but a
  pointer to `String<N>` crosses as an address.
- A raw string literal (`r"..."`, `r#"..."#`, `r##"..."##`, ...) copies its content byte-for-byte
  with no escape or interpolation processing; any number of `#` delimiters is accepted, and the
  literal closes at the first `"` followed by at least that many `#` characters. It is static-backed
  like an interpreted literal (never freed) and identical in byte length, equality, ordering,
  slicing, and print behavior to an interpreted literal with the same UTF-8 bytes.
- Every interpreted string is scanned for unescaped `{{`, whether or not it appears inside
  `String.interpolate`; `\{` and `\}` are escapes producing literal braces, and a single unescaped
  `{`, `}`, or `}}` outside an active `{{ ... }}` region is literal text. A `{{ expression }}` found
  anywhere except exactly `String.interpolate`'s second argument is a Type Error rather than an
  implicit allocation.
- `String.interpolate(heap, template)` builds one heap-owned String: the Heap evaluates first and
  exactly once, then each embedded expression evaluates exactly once, left to right, formatted with
  the same spelling `print` uses (no quotes, separators, or trailing line break) and concatenated
  with the template's literal text in source order. `String<N>.interpolate(template)` does the same
  into inline storage with no Heap and returns `String<N> | Error`, failing with the capacity
  failure when the result does not fit. The template must contain at least one
  interpolation; a plain literal with no braces or a raw literal is rejected, since raw text never
  interpolates. Interpolation supports exactly Bool, every fixed-width signed and unsigned
  integer, Size, Byte, Float32, Float64, and text of any form; Nil, pointers, unions, structs, ADTs,
  arrays, slices, lists, dictionaries, allocators, concurrency values, Error, and Fun are rejected.
  The heap result follows the ordinary String allocation/free contract; borrowed text operands
  contribute only their bytes and gain no new lifetime relation to the result.

## Output

### `print`

```text
print(first: Printable, rest: Printable...) -> no value
```

- `print(arg, ...)` is protected, requires at least one argument, inserts no separator/newline, and
  returns no value. Arguments evaluate once left-to-right; output starts only after all evaluation.
- Directly printable: Bool, fixed-width integers, Size, Byte, Float32, Float64, text of any form
  (`String` and `String<N>`), Nil, and Error. Objects, ADTs, Array, Slice, List, and Dict are printable exactly when every
  recursively visited component is printable. Every other canonical type is non-printable; unions
  must narrow to a printable member first. Failure identifies the first non-printable member path in
  declaration order.
- A print argument is the one position that admits standalone Nil, so a union narrowed to Nil and the
  bare `nil` literal are both printable. Nil prints `nil` directly and nested.
- Direct text is raw; nested text is quoted/escaped; Byte is numeric. Structural forms are
  fixed, one line, and exactly:

```text
object:             <Type> { <member> = <value>, ... }
unit ADT variant:   <ADT>.<Variant>
record ADT variant: <ADT>.<Variant> { <field> = <value>, ... }
Array/Slice/List:    [<value>, ...]
Dict:               {<key>: <value>, ...}
```

Object members use declaration order and ` = `. Record variants print only the active payload.
Array/Slice/List use `[]` when empty. Dict uses `:`, `{}` when empty, and unspecified entry order.

- Float32/64 use `%g` precision 9/17; signed zero and `inf`, `-inf`, `nan` are preserved. A direct
  Error prints `file:line:column: header: message` with no trailing newline, where `header` is
  `error.kind`'s derived header text; nested, it uses the object form with declaration-ordered
  fields (`kind` prints its derived header text; there is no separate `header` member) and quoted
  text. A direct or nested `ErrorKind` prints its derived header text.
- A whole call is atomic relative to print and standard text writes. It does not flush per call.
  Root defers finish before process exit; shutdown then flushes stdout/applicable stderr.
  Detected output failure is unrecoverable.

## Tasks and synchronization

### Tasks

```text
spawn function(args) -> Task<R> | Error
Task<R>.join() -> R
Task<R>.detach() -> no value
Task.yield() -> no value
```

Sleep is not a Task method; it is the `std/time` module function `sleep(duration: Duration)`.

- Spawn evaluates arguments once left-to-right and shallow-copies them; failure starts no task. R
  must be valid in FunctionResult and TaskResult, complete, finite, and copyable. Spawn Error is
  separate from returned R. Task creation failure (allocation) returns Error with kind
  `ErrorKind.ResourceExhausted()`.
- `join()` waits, copies the exact result, and reclaims storage. `detach()` discards result and
  arranges reclamation. Exactly one successful join or detach is allowed across aliases.
- Scheduler-owned stacks/control/queues need no allocator. `Task.yield()` is the explicit scheduling
  point in one cooperative M:N scheduler over libuv worker threads. A native operation that parks its
  caller on the runtime event bridge (see IO) does not satisfy this explicit-yield rule; a `while true` loop
  containing only such an operation still requires its own `Task.yield()`.
- The qualified native target is Linux x86-64 built by installed Clang 18 or newer, using the
  qualified libuv thread, mutex, condition, detach, and available-parallelism facilities. Windows
  x86-64 remains a core C-generation target with no native driver in this release; its checked-in
  runtime pack is not delivered and its generated C is checked by pure-Go tests only. Root is
  pinned to worker zero; root return does not join tasks. Stacks
  reserve 1 MiB by default with an 8 KiB initial commit, both `Project` build-time settings; the
  initial commit is a Windows-only knob, and the usable region is the reserve less one guard page.
  Exceeding the reserve traps with `[Runtime Error] task stack overflow` rather than corrupting
  memory.
- `std/time.sleep(d)` parks only the current Task on the runtime event bridge; no scheduler worker
  blocks. Using it selects the scheduler, so root may sleep. Zero returns immediately and is not a
  scheduling point; like every parking operation it never satisfies the explicit-yield rule. A
  duration above `Int64` maximum nanoseconds traps with `[Runtime Error] sleep duration too large`
  before any timer starts. Sleep completes no earlier than the requested duration and promises no
  upper lateness bound. Timer failure traps with `[Runtime Error] task sleep failed`. Root
  completion does not wait for a detached sleeping Task.
- Every repeating path through task-reachable literal `while true` visibly executes `Task.yield()` or
  compilation fails.
- Spawn, join, Mutex, Channel, and sequentially consistent Atomic operations provide their specified
  C23 synchronization edges. Unsynchronized conflicting access is a data race with no guarantee.

### `Channel<T>`

```text
Channel<T>(heap: Heap, capacity: Size) -> Channel<T> | Error
Channel<T>.send(value: T) -> Nil | Error
Channel<T>.receive() -> T | EoS
Channel<T>.close() -> no value
Channel<T>.free(heap: Heap) -> no value
Channel<T>.length() -> Size
Channel<T>.capacity() -> Size
Channel<T>.is_closed() -> Bool
```

- Bounded MPMC FIFO; capacity zero fails at compile time when known, otherwise with Error. Full send
  and empty receive park Task, not worker. Construction failure (allocation) returns Error with kind
  `ErrorKind.ResourceExhausted()`.
- T must be valid in ChannelElement, complete, finite, and copyable, which excludes top-level EoS and
  any value transitively containing Atomic. Elements copy shallowly. Error is a valid T.
- Send after close returns Error with kind `ErrorKind.Closed()`. Close is idempotent, preserves
  queued values, and wakes waiters; closed/drained receive returns eos. Receive adds no Error result
  member.
- Free requires closed, empty, unused state and releases only Channel storage.

### `Mutex`

```text
Mutex(heap: Heap) -> Mutex | Error
Mutex.lock() -> no value
Mutex.unlock() -> no value
Mutex.free(heap: Heap) -> no value
```

- Allocated scheduler-aware non-recursive lock owned by Task identity. Waiting parks Task. Recursive
  lock, wrong-owner/double unlock, or freeing locked/waited Mutex is programmer error. Invalid states
  detectable from a live control block trap, including recursive lock and wrong-owner unlock. Freed
  control blocks need not be retained to diagnose stale aliases; use after free is not guaranteed to
  trap. Construction failure (allocation) returns Error with kind `ErrorKind.ResourceExhausted()`.

### `Atomic<T>`

```text
Atomic<T>(initial: T) -> Atomic<T>
Atomic<T>.load() -> T
Atomic<T>.store(value: T) -> no value
Atomic<T>.exchange(value: T) -> T
Atomic<T>.fetch_add(value: T) -> T
Atomic<T>.fetch_sub(value: T) -> T
Atomic<T>.compare_exchange(expected: T, desired: T) -> Bool
```

- T is Bool, Int32, UInt32, Int64, UInt64, or Size. Operations are inline, allocator-free, and
  sequentially consistent; lock-freedom is not guaranteed. `fetch_add`/`fetch_sub` reject Bool.
  Compare-exchange is strong and non-spurious: equality stores desired and returns true; inequality
  preserves the value and returns false. Expected is input-only.
- Atomic and inline aggregates containing one are non-copyable. Their direct in-place construction is
  valid only in Binding and ObjectMember positions. Copy-requiring parameters/results, ADT payloads,
  unions, collections, Tasks, Channels, and HeapAllocation are invalid.
- Atomic itself is invalid in Pointee; an enclosing object containing Atomic remains valid in Pointee.
- `Atomic<T>(value)` directly initializes fresh binding or object-member storage; these are its
  only placements. Nested object construction initializes each member in place. The resulting object
  is non-copyable but may be shared through `Ptr`. `@` of Atomic or an Atomic member is
  independently invalid. Pointers to enclosing Atomic-containing objects remain valid.

## Time

```text
-- std/time module functions
nanoseconds(value: UInt64)  -> Duration
microseconds(value: UInt64) -> Duration
milliseconds(value: UInt64) -> Duration
seconds(value: UInt64)      -> Duration
now()                       -> Instant
wall_time()                 -> WallTime | Error
sleep(duration: Duration)   -> no value
-- instance methods
Duration.as_nanoseconds()            -> UInt64
Duration.as_microseconds()           -> UInt64
Duration.as_milliseconds()           -> UInt64
Duration.as_seconds()                -> UInt64
Instant.elapsed()                    -> Duration
Instant.duration_since(earlier: Instant) -> Duration
WallTime.seconds()                   -> Int64
WallTime.nanosecond()                -> UInt32
```

- `Duration`, `Instant`, and `WallTime` are value types exported by `std/time` with no scalar kind: numeric
  operators, `to<T>()`, `print`, integer mixing, and construction other than the listed operations
  are rejected. Arguments are exact `UInt64`/`Instant` values with no implicit conversion.
- Duration is an unsigned nanosecond magnitude lowering to `uint64_t`. Unit constructors scale with
  checked multiplication and trap with `[Runtime Error] duration overflow`; unit accessors
  truncate toward zero. `Duration + Duration` traps on overflow with the same message and
  `Duration - Duration` traps with `[Runtime Error] duration underflow` when the right operand is
  larger. Duration has no `*` or `/`.
- Instant is a monotonic timestamp from libuv's `uv_hrtime()` whose origin and representation are
  not observable. `Instant - Instant` and `later.duration_since(earlier)` yield Duration and trap
  with `[Runtime Error] invalid instant subtraction` when the left operand precedes the right;
  `elapsed()` is `std/time.now()` minus the receiver. No other arithmetic accepts Instant.
- WallTime is a UTC observation from C23 `timespec_get(TIME_UTC)`: signed Unix seconds and a
  normalized `0..999_999_999` nanosecond fraction. It has no arithmetic and never converts to or
  from Instant. Acquisition failure returns an Error with kind `ErrorKind.Unsupported()` and message
  `wall clock acquisition failed`, allocating nothing.
- All three types support `==`, `!=`, `<`, `<=`, `>`, and `>=` against the same type; WallTime
  orders by seconds, then fraction. Operands of different time types are rejected.
- Selection: any time type or operation emits `hexal/time.h`/`hexal/time.c`. `std/time.now` and
  `elapsed` additionally select libuv and its native bootstrap but not the scheduler; `WallTime`
  selects no libuv; `std/time.sleep` selects the scheduler and the event bridge.

## Byte streams

```text
-- std/io module functions
stdin()  -> IO | Error
stdout() -> IO | Error
stderr() -> IO | Error
bytes_over(buffer: List<Byte>) -> Bytes
-- instance methods
IO.read(into: List<Byte>, max: Size)            -> Size | EoS | Error
IO.write(from: Slice<Byte>)                      -> Size | Error
IO.seek(to: Seek)                                -> Size | Error
IO.close()                                       -> Nil | Error
Ptr<mut Bytes>.read(into: List<Byte>, max: Size) -> Size | EoS | Error
Ptr<mut Bytes>.write(from: Slice<Byte>)           -> Size | Error
Ptr<mut Bytes>.seek(to: Seek)                     -> Size | Error

type Seek is union | Start as position: Size end | Current as offset: Int64 end | End as offset: Int64 end end
```

- `IO`, `Bytes`, and `Seek` are exported type names of `std/io`. They reserve no name, so a user may
  declare them; each is reached through the module alias (`Io.IO`, `Io.Bytes`, `Io.Seek`). `Start`,
  `Current`, and `End` remain available as unqualified names. Seek variants construct as
  `Seek.Start(position = ...)`, `Seek.Current(offset = ...)`, and `Seek.End(offset = ...)`, reached
  through the alias (`Io.Seek.Start`).
- IO lowers to `{ intptr_t desc, uint8_t access, bool owned }`; Bytes lowers to a borrowed
  `List<Byte>` header pointer plus an inline cursor. Copies of IO alias one external resource;
  copying Bytes copies the cursor, so copies advance independently.
- Constructors are fallible because the process may lack the requested standard handle. They return
  borrowed handles with `owned = false`; stdin carries readable access, stdout and stderr writable.
- Capability checking has two tiers: constructor/flow facts proving absence reject at the call;
  otherwise the operation checks the access mask and returns Error before any platform call or
  allocation. Facts seed from constructors, copy on assignment from a tracked binding, intersect on
  branch merge, and drop to unknown on escape through parameters, results, members, unions, or other
  untracked aliases.
- A positive count is ordinary success including short transfers. `eos` is returned only when no
  byte was transferred and the source is drained. Each read/write issues at most one platform call,
   clamped per target (`SSIZE_MAX` POSIX, `UINT32_MAX` Windows); `max == 0` and an empty Slice return
   `Size(0)` touching nothing. POSIX `EINTR` before transfer returns Error and is never retried by
   the primitive.
- Read appends at most `max` bytes to the destination list, preserving prior contents; destination
   capacity grows once through the internal List reserve helper before one platform call.
- Bytes write overwrites at the cursor and extends the list past its end; Bytes seek resolves within
   `[0, buffer.length]` only — sparse holes do not exist. Self-read (destination identity equal to
   the backing list) and writes from a Slice overlapping the backing allocation return Error before
  any mutation, with messages `memory stream cannot read into its backing list` and `memory stream
  cannot write from its backing list`.
- Close on an owned handle invalidates every copy even when it reports failure; POSIX close is never
  retried after `EINTR`. Closing a borrowed standard or foreign handle traps. Locally proved
  use-after-close and repeated close are rejected; escaped aliases follow the external-state
  envelope. Only `IO.close()` may appear in defer/errdefer.
- Bytes borrows its source List: a locally proved free of that list rejects later construction and
  operations; deferred frees and escaped aliases take the undecidable envelope.
- Placement bootstrap: both types are valid in bindings, parameters, results, direct union members,
  and pointer pointees; IO additionally in Task arguments and results; Bytes is excluded there.
  Both are rejected in object members, ADT payloads, collections, Channels, and heap allocation,
  recursively through aggregates.
- Concurrent use of one stream requires external synchronization; no cross-task ordering or
  compound-write atomicity is promised. A synchronous native transfer invoked by a running Task
  parks that Task and runs through the runtime event bridge instead of blocking a scheduler worker;
  the request executes through libuv's shared worker pool, whose size follows libuv's process-level
  configuration. Hexal does not create, grow, retire, or maintain a second blocking pool. A queued
  operation is a parked Task, not a blocked one. A call made outside any Task runs directly, with
  no pool involved. Bytes never uses the pool, since its transfers are pure memory operations.
  Source-visible semantics and failures for IO and print are unchanged either way.
- Failures carry a portable `ErrorKind` classified from the native POSIX errno or Windows code (a
  small front-end owned by `hexal/io.c`, agreeing with the common libuv mapper on shared conditions)
  plus a static message such as `read failed` or `stream is not writable`; an unmapped native code
  uses `ErrorKind.Other(header = "IO error")`. A capability mismatch (`not readable`/`not writable`)
  uses `PermissionDenied`; a Bytes self-read or overlapping write uses `InvalidInput`. No Heap is
  required on a failure path, and no header or message contains a native number.
- `print` shares the descriptor write-all backend of stdout: one buffering domain, short-write and
  EINTR retries inside print's private sink, trap only when a complete print cannot finish.
- Generated C confines all platform branches to `hexal/io.c`; no signature contains `#ifdef`,
  `FILE *`, or a platform type. Selecting IO, Bytes, or print selects the pair plus the
  `List<UInt8>` specialization once; programs using none emit no IO artifact.

### `File`

```text
-- std/fs module function
open(path: String, mode: FileMode) -> File | Error
-- instance methods
File.read(into: List<Byte>, max: Size)   -> Size | EoS | Error
File.write(from: Slice<Byte>)            -> Size | Error
File.seek(to: Seek)                      -> Size | Error
File.flush()                             -> Nil | Error
File.close()                             -> Nil | Error

type FileMode is Read | Write | Append | ReadWrite | CreateNew end
```

- `File` and `FileMode` are exported type names of `std/fs` and reserve no name; each is reached
  through the module alias (`Fs.File`, `Fs.FileMode`). FileMode variants construct call-shaped, for
  example `FileMode.Read()`, reached through the alias (`Fs.FileMode.Read()`). File lowers to a generation-checked handle plus an access mask, over one
  owned native descriptor in a heap-allocated (mimalloc-backed, not source-level `Heap`) control
  block; it is distinct from IO and exposes no descriptor, libuv, generation, or platform name.
- A File value is an ordinary copyable handle: every copy names the same descriptor and observes
  one shared lifecycle. Because closing is generation-checked rather than relying on a shallow-copy
  alias lifetime, File occupies every ordinary complete-value position -- bindings, parameters,
  results, struct and ADT payload members, union members, Array/Slice/List elements, Dict values,
  pointer pointees, Heap/Stash/Pool allocations, Task arguments/results, and Channel elements --
  unlike IO, which is restricted to the ephemeral positions its own shallow-copy model allows. File
  has no equality, ordering, hash, or print contract and is invalid as a Dict key.
- Modes: `Read` opens an existing file read-only; `Write` opens write-only, creating or truncating;
  `Append` opens write-only, creating, and every write lands at the then-current end; `ReadWrite`
  opens an existing file for both without truncation; `CreateNew` creates a write-only file and
  fails when the path exists. New POSIX files request mode `0666` subject to the umask.
- Paths are UTF-8 `String` values passed without normalization, canonicalization, case folding, or
  absolute conversion. An embedded NUL fails before any request with kind `ErrorKind.InvalidPath()`
  and message `file open failed`.
- Read is permitted by Read and ReadWrite; write and flush by Write, Append, ReadWrite, and
  CreateNew; seek and close by every mode. Capability checking follows IO's two tiers and precedes
  the zero-length path: a statically known mismatch rejects at the call, otherwise the operation
  returns an Error with kind `ErrorKind.PermissionDenied()` and message `file is not readable` or
  `file is not writable`.
- Read, write, EoS, zero-length, partial-transfer, per-call clamp (`UINT32_MAX` bytes), destination
  reservation, copied-cursor, and placement rules match IO. Read and write use and advance the
  shared descriptor position. `flush` completes after `uv_fs_fsync` succeeds. `seek` runs directly,
  never through the worker pool. Only `File.close()` may appear in defer/errdefer.
- Closing resolves the handle, transitions its slot from live to closing (which rejects every new
  operation immediately, from any copy), then runs the native close; the slot recycles once every
  operation that resolved before the transition has finished, whichever finishes last. A statically
  provable double-close on one binding is a check error ("this stream was closed on every path");
  an escaped or aliased use that the checker cannot prove -- a second copy, a copy stored in a
  struct or collection, a copy crossing a Task boundary -- instead returns the owning operation's
  Error with kind `ErrorKind.Closed()` at runtime. Neither path ever dereferences released control
  storage: a closed or stale handle's copy is detected before any native call runs.
- Outside a Task each operation uses libuv's synchronous filesystem request; inside a Task it parks
  only that Task on the event bridge. Filesystem requests share libuv's worker pool. Root completion
  does not wait for a detached Task doing File work.
- Failures carry a static operation message (`file open failed`, `file read failed`,
  `file write failed`, `file seek failed`, `file flush failed`, `file close failed`) and one
  portable `ErrorKind`, identical on every target and never containing a path or native code.
  File-specific categories and contextual overrides: `InvalidPath` (ENAMETOOLONG, ELOOP, and EINVAL
  from open only), `NotADirectory`, `IsADirectory`, `DirectoryNotEmpty`, `ReadOnly`, `Busy`,
  `PermissionDenied` for a statically-known-mismatch capability failure, and `Closed` for an
  escaped or stale handle. Every other libuv condition classifies through the shared handle
  component's common mapper (below); an unmapped result uses `Other(header = "filesystem error")`.
  A control-block allocation failure after a successful native open returns `ResourceExhausted`
  without publishing a handle, and closes the just-opened native descriptor first. No failure path
  performs an additional allocation to report itself.
- Selection: reachable File use emits `hexal/file.h`/`hexal/file.c` plus the shared
  `hexal/handle.h`/`hexal/handle.c` pair, and selects libuv, mimalloc, and the native bootstrap;
  File without the scheduler selects no event bridge. IO and print without Task keep their direct
  path and select no libuv.

### The shared handle registry

Every long-lived libuv-backed capability -- File, TcpConnection and TcpListener, Process and Pipe,
and Signals -- resolves through one program-wide, generation-checked handle registry rather than
each defining its own liveness state and libuv error switch.

- A handle is `{ slot, generation }`: an opaque slot identity plus the generation it was published
  under. Slot storage lives in fixed-size chunks allocated once and never moved or freed before
  process exit, so a published slot's address, and therefore every outstanding copy of a handle
  naming it, stays valid for the rest of the process even while the registry grows. Only chunk
  growth and free-slot selection take the one program-wide registry lock; every resolve, release,
  and close synchronizes through the resolved slot's own private lock, never that shared one.
- A slot carries a lifecycle state (`free -> opening -> live -> closing -> free-or-retired`), the
  capability kind that reserved it, a private control-block pointer, and an in-flight operation
  count. Resolving locks the slot, accepts only a `live` state with a matching generation and
  capability kind, increments the count, and returns the pinned control-block pointer before
  unlocking; releasing decrements the count under the same lock. Closing locks the slot and
  linearizes `live -> closing` immediately, which is what makes every other copy observe closed
  from that instant; recycling -- clearing the control block, incrementing the generation, and
  returning the slot to the free list -- waits until every operation that resolved before the
  closing transition has released, whichever operation (the closer or a still-running one)
  finishes last. A generation that would wrap on reuse retires the slot permanently instead.
- Registry, slot, and control-block storage use the runtime's private mimalloc-backed allocator,
  never source-level `Heap`; a materialized result collection a capability's own operation returns
  still uses the caller's `Heap`. Allocation failure returns Error and publishes no partial handle.
- One shared mapper (`hexal/handle.c`) classifies the libuv conditions common to every capability
  into a stable, target-independent `ErrorKind`: `NotFound`, `PermissionDenied`, `AlreadyExists`,
  `InvalidInput`, `ResourceExhausted` (`ENOMEM`, `ENOBUFS`, `EMFILE`, `ENFILE`), `Unsupported`,
  `Cancelled`, `Interrupted`, `TimedOut`, `AddressInUse`, `AddressUnavailable`, `ConnectionRefused`,
  `ConnectionReset`, `ConnectionAborted`, `HostUnreachable`, `NetworkUnreachable`, `BrokenPipe`, and
  `NotConnected`. An unmapped or capability-specific condition falls through to that capability's
  own `Other` fallback; no capability duplicates this switch.
- Selection: reachable use of any handle-backed capability emits exactly one
  `hexal/handle.h`/`hexal/handle.c` pair; a program using none emits neither file. The generated
  public surface exposes no libuv type, pointer, request, callback, or numeric error code.

### Networking

```text
type Address is union
    | IPv4 as bytes: Array<Byte, 4>, port: UInt16 end
    | IPv6 as bytes: Array<Byte, 16>, port: UInt16, scope: UInt32 end
end

-- std/net module functions
parse_address(text: String, port: UInt16) -> Address | Error
resolve(heap: Heap, host: String, service: String)
                                             -> List<Address> | Error
connect(address: Address)                 -> TcpConnection | Error
listen(address: Address, backlog: Size)   -> TcpListener | Error
-- instance methods
Address.format(heap: Heap)                -> String

TcpListener.accept()                          -> TcpConnection | Error
TcpListener.close()                           -> Nil | Error

TcpConnection.read(into: List<Byte>, max: Size)
                                             -> Size | EoS | Error
TcpConnection.write(from: Slice<Byte>)        -> Nil | Error
TcpConnection.shutdown()                      -> Nil | Error
TcpConnection.no_delay(enabled: Bool)         -> Nil | Error
TcpConnection.close()                         -> Nil | Error
```

- `Address`, `TcpConnection`, and `TcpListener` are exported type names of `std/net` and reserve no
  name; the former `Dns` and `Tcp` namespaces are removed. Each is reached through the module alias
  (`Net.Address`, `Net.parse_address`, ...). `Address` is an ordinary inline ADT: `IPv4` and `IPv6` construct and
  match through the general ADT rules. IPv4 stores four network-order bytes and a host-order
  port; IPv6 stores sixteen network-order bytes, a host-order port, and a numeric scope. No
  Address value allocates, and Address has no equality, ordering, hash, or print contract.
- `std/net.parse_address` accepts a numeric IPv4 or IPv6 literal only and performs no DNS lookup; an
  embedded NUL or malformed literal returns `InvalidInput`. A scoped IPv6 literal accepts only a
  decimal numeric scope after `%`; interface-name scopes are not supported. `Address.format`
  emits the numeric host address without a port, allocated from `heap`, appending
  `%<unsigned-decimal-scope>` itself for a nonzero IPv6 scope; every valid Address has a bounded
  representation, so formatting is infallible.
- `std/net.resolve` accepts a host plus a numeric or named service, rejects an embedded NUL before
  submission, and allocates its returned `List<Address>` from `heap`, preserving libuv's result
  order. DNS has no close or cancellation surface.
- `TcpConnection` and `TcpListener` use the shared generation-checked handle representation and
  occupy every ordinary complete-value position, exactly like File; `close` invalidates every
  copy and is the only operation valid in `defer`/`errdefer`.
- `backlog` must be positive and fit libuv's `int`; otherwise `std/net.listen` returns `InvalidInput`
  before native submission. Binding an IPv6 listener always passes the IPv6-only option; v1 never
  changes IPv4 acceptance according to a host's dual-stack default.
- `shutdown` closes only the write half; reads remain valid until EoS or close. `write` is
  write-all: success means every source byte was accepted, submitted as sequential chunks within
  libuv's representable length; failure after partial progress returns Error without exposing the
  completed prefix. A write borrows its source Slice, and a read appends into its destination
  List and reserves capacity before native submission, until the call returns; the programmer
  owns not mutating, growing, or freeing that storage from another Task meanwhile.
- DNS and TCP operations always run inside a Task; there is no synchronous fallback path for a
  parking operation, so root-level use still requires the scheduler bootstrap even without an
  explicit Task, Channel, Mutex, or spawn elsewhere in the program. A positive read count is
  ordinary success; `eos` appears only when the peer ended the stream and no byte was delivered by
  that call. POSIX runtime initialization ignores `SIGPIPE` before any socket or pipe write can
  run, so a closed peer becomes `BrokenPipe` instead of terminating the process.
- This implementation admits one active read, one active write, and one active accept at a time
  per connection or listener: a second concurrent call of the same kind returns Error with kind
  `Busy` immediately rather than joining a FIFO wait queue.
- Errors: networking first applies the shared handle registry's portable libuv ErrorKind mapper.
  Local contract failures additionally use `InvalidInput` (malformed numeric address, invalid
  backlog), `Busy` (a second concurrent read, write, or accept), `Closed` (a closed handle or a
  close-cancelled socket operation), and `Other` with a fixed operation-specific header
  (`"name resolution failed"` for DNS, `"network error"` for TCP/listener) for every other
  condition. Messages name the failed operation, never embed host text or addresses, and every
  Error carries the Hexal call site's source location.
- Selection: reachable networking use emits `hexal/network.h`/`hexal/network.c`. Address
  parse/format alone selects only the network component and libuv's address helpers, not the
  scheduler, event bridge, or handle component; DNS and TCP additionally select the handle
  component (for TCP's copied-handle registry and DNS and TCP's shared error mapper), the event
  bridge, and the scheduler bootstrap.
- UDP, multicast, keepalive, reusable-port controls, batched receive, interface discovery, and
  foreign socket adoption are not part of this implementation.

### Processes and IPC

```text
type Environment is union
    | Inherit
    | Replace as values: List<EnvironmentVariable> end
end

type EnvironmentVariable is struct
    name: String,
    value: String,
end

type ProcessStream is Ignore | Inherit | Pipe end

type ProcessOptions is struct
    program: String,
    arguments: List<String>,
    environment: Environment,
    working_directory: String | Nil,
    input: ProcessStream,
    output: ProcessStream,
    error: ProcessStream,
end

type ExitStatus is union
    | Exited as code: Int64 end
    | Terminated
end

type StartedProcess is struct
    process: Process,
    input: Pipe | Nil,
    output: Pipe | Nil,
    error: Pipe | Nil,
end

-- std/process module function
start(options: ProcessOptions)         -> StartedProcess | Error
-- instance methods
Process.wait()                         -> ExitStatus | Error
Process.terminate()                    -> Nil | Error
Process.close()                        -> Nil | Error

Pipe.read(into: List<Byte>, max: Size) -> Size | EoS | Error
Pipe.write(from: Slice<Byte>)          -> Nil | Error
Pipe.shutdown()                        -> Nil | Error
Pipe.close()                           -> Nil | Error
```

- `Process`, `Pipe`, `ProcessOptions`, `StartedProcess`, `Environment`, `EnvironmentVariable`,
  `ProcessStream`, and `ExitStatus` are exported type names of `std/process` and reserve no name;
  each is reached through the module alias (`Proc.Process`, `Proc.ExitStatus`, ...). `Process`
  and `Pipe` use RFC 0180's generation-checked copied-handle representation, exactly like File and
  TcpConnection, and occupy every ordinary complete-value position.
  `Environment`, `ProcessStream`, and `ExitStatus` are ordinary inline ADTs: their variants
  construct and match through the general ADT rules, for example
  `match status is | ExitStatus.Exited then status.code | ExitStatus.Terminated then -1 end`.
  `ProcessOptions`, `EnvironmentVariable`, and `StartedProcess` are ordinary inline structs.
- `program` names the executable; `arguments` holds only the arguments after argument zero, which
  the runtime supplies from `program`. No shell parses `program` or any argument, so argument
  boundaries, order, empty strings, and UTF-8 bytes are preserved exactly. An embedded NUL in
  `program`, an argument, `working_directory`, an environment name, or an environment value
  returns `InvalidInput` before native submission. A relative `program` uses the host's ordinary
  executable search (Windows may consider the current directory before PATH, unlike a POSIX
  `execvp` search); an absolute path avoids that ambiguity.
- `Environment.Inherit()` copies the parent environment at spawn. `Environment.Replace(values =
  ...)` supplies exactly the given `EnvironmentVariable` entries and does not merge them with the
  parent environment; there is no `NAME=value` parsing surface. A name must be non-empty and
  contain neither `=` nor NUL; a value must contain no NUL. A duplicate name (POSIX compares
  byte-for-byte, Windows case-insensitively) returns `InvalidInput` before submission. Entry order
  has no semantic effect.
- `working_directory = nil` inherits the parent's current directory. `ProcessStream.Ignore()`
  connects no parent-facing stream; `.Inherit()` uses the matching parent standard stream;
  `.Pipe()` creates one parent-facing `Pipe`. `StartedProcess.input`, `.output`, and `.error` are
  non-Nil exactly where the matching option requested `Pipe()`; `input` is writable, `output` and
  `error` are readable, and an operation contrary to that capability returns `PermissionDenied`.
- Exit code is libuv's signed 64-bit result; `ExitStatus.Terminated()` means the target reported
  signal termination, produced only when the platform reports it. `wait()` may be called
  concurrently and after exit; every successful call observes the same cached `ExitStatus`. An
  exit-completion-first race delivers that value to an already-parked wait; a close-first race
  delivers `Closed` to active waiters instead, and a later exit still reaps the child privately.
- `terminate()` is the one termination request: `uv_process_kill(process, SIGTERM)` on every
  target (POSIX delivers catchable `SIGTERM`; Windows implements it as forceful `TerminateProcess`
  through libuv). It has no separate forceful companion and no `kill()`.
- `close()` never kills a live process and never closes its native handle before the exit callback
  has reaped the child; it invalidates the public handle immediately, and private state releases
  once both the reap and the native close finish. Root completion never implicitly waits for or
  terminates a child: an unwaited, unterminated process becomes an ordinary external process. Only
  `Process.close()` and `Pipe.close()` may appear in `defer`/`errdefer`.
- `Pipe` uses the same pull-read, FIFO write-all, borrowed-buffer, and close-wakes-waiters
  contracts TcpConnection uses (see Networking above), including this implementation's one
  active read and one active write at a time per Pipe. A caller must concurrently drain a
  requested stdout/stderr `Pipe` while the child may write to it; waiting for exit before reading
  can deadlock once the OS pipe buffer fills, since Hexal adds no unbounded capture buffer.
- Named local IPC endpoints, IPC handle passing, raw process IDs, a wait deadline, Task
  cancellation, and a second termination operation are not part of this implementation.
- Errors: process and Pipe operations first apply the shared handle registry's portable libuv
  ErrorKind mapper. Local contract failures additionally use `InvalidInput` (an invalid option or
  embedded NUL), `PermissionDenied` (a Pipe direction mismatch), `Closed` (a closed Process or
  Pipe, including a close-cancelled Pipe operation), and `Other` with a fixed header
  (`"process error"` for Process operations, `"pipe error"` for Pipe operations) for every other
  condition. Messages name the failed operation, never embed command text, arguments, or paths,
  and every Error carries the Hexal call site's source location.
- Selection: constructing or inspecting `ProcessOptions`, `Environment`, `ProcessStream`,
  `ExitStatus`, `EnvironmentVariable`, or `StartedProcess` alone emits only `hexal/process.h`'s
  type definitions (which embed the shared `hex_handle` representation, a disclosed simplification
  with no scheduler or libuv cost of its own). A reachable Process or Pipe operation additionally
  emits `hexal/process.c` and selects the handle component, the event bridge, the scheduler
  bootstrap, libuv, and the native bootstrap.

### Signals

```text
type Signal is Interrupt | Hangup | Terminate end

-- std/signal module function
subscribe(subscriptions: Slice<Signal>) -> Signals | Error
-- instance methods
Signals.next()                        -> Signal | EoS | Error
Signals.close()                       -> Nil | Error
```

- `Signal` and `Signals` are exported type names of `std/signal` and reserve no name; each is
  reached through the module alias (`Sig.Signal`, `Sig.subscribe`). `Signal` is an
  ordinary inline ADT: `Signal.Interrupt()`, `Signal.Hangup()`, and `Signal.Terminate()` construct
  and match through the general ADT rules. `Signals` uses the shared generation-checked handle
  representation and occupies every ordinary complete-value position, exactly like File and
  Process; construction takes no source-level Heap.
- Construction copies the subscription values out of `subscriptions` before returning and retains
  no pointer to it; the Slice may be freed or go out of scope immediately after the call. An empty
  or duplicate-containing Slice returns `InvalidInput` before native registration.
- Linux and macOS support all three variants. Windows supports Interrupt and Hangup only;
  requesting Terminate on Windows returns Unsupported before installing any watcher. Windows
  Hangup delivery is best-effort: a console/window close may force process termination after a
  short platform-controlled cleanup interval. A variant unsupported by the selected target is
  rejected by construction, never silently turned into an inert watcher.
- Each subscription stores one pending bit per subscribed Signal. Repeated occurrences of an
  already-pending Signal coalesce; distinct Signals remain independently pending. `next()` clears
  and returns one pending Signal in stable Signal declaration order (Interrupt, Hangup, Terminate);
  occurrence counts are not observable, and arrival order between different Signal variants is not
  preserved. Every active Signals resource subscribed to a Signal receives it -- there is no
  process-global winner.
- One `next()` may be active per Signals resource; a concurrent second call returns Busy without
  consuming a pending event. Event selection and `close()` linearize under the same slot
  synchronization: whichever wins first determines the outcome for the active waiter. A `next()`
  already parked when close wins receives EoS; a new `next()` through an already closed handle
  returns Closed instead.
- No native signal handler executes Hexal work: the libuv callback that sets a pending bit and
  wakes a parked Task runs on the loop thread, performs no allocation, and invokes no user code.
  Signal sending, raw signal numbers, and target-specific signal names are not part of this
  surface; Process control or unsafe C interoperability own sending. Root completion performs no
  implicit subscription close -- close explicitly, normally through `defer`.
- `Signals.close()` is a valid `defer`/`errdefer` cleanup call; it is the only operation valid
  there.
- Errors: Signal operations first apply the shared handle registry's portable libuv ErrorKind
  mapper. Local contract failures additionally use `InvalidInput` (an empty or duplicate
  subscription set), `Unsupported` (a requested variant the target cannot deliver), `Busy` (a
  second concurrent `next()`), `Closed` (a closed subscription operation other than an already
  parked `next()`), and `Other` with a fixed `"signal error"` header for every other condition.
  Messages name the failed operation, contain no native signal number, and every Error carries the
  Hexal call site's source location.
- Selection: constructing or matching a `Signal` variant alone emits only `hexal/signal.h`'s type
  definitions (which embed the shared `hex_handle` representation, a disclosed simplification with
  no scheduler or libuv cost of its own). A reachable `std/signal.subscribe` call, `next`, or `close`
  additionally emits `hexal/signal.c` and selects the handle component, the event bridge, the
  scheduler bootstrap, libuv, and the native bootstrap. A collection specialized over `Signal`
  (`List<Signal>`, `Array<Signal, N>`, `Slice<Signal>`, `Dict<K, Signal>`, `Pool<Signal>`) is
  rendered in each consuming module's own header rather than the shared collection component, a
  header-ordering accommodation with no effect on program behavior; equality (`==`/`!=`) over such
  a collection is not part of this implementation, though bare `Signal == Signal` comparison is.
- Sending signals, exposing signal numbers, synchronous fault handling (stack overflow,
  segmentation faults), Task cancellation, deadlines, and preserving a foreign handler installed
  before a Hexal subscription are not part of this implementation.

### Terminal

```text
-- std/terminal module functions
is_attached(stream: IO) -> Bool | Error
size(stream: IO)        -> TerminalSize | Error

type TerminalSize is struct
    columns: Size,
    rows: Size,
end
```

- The former `Terminal` namespace type is removed. `TerminalSize` is an exported type name of
  `std/terminal` that reserves no name; each is reached through the module alias
  (`Term.is_attached`, `Term.TerminalSize`). `TerminalSize` is an ordinary immutable struct: it
  constructs, compares, and prints through the general struct rules, for example
  `TerminalSize(columns = 80, rows = 24)`.
- `is_attached` returns `false` for a valid non-terminal stream (redirected output, a file, a
  pipe); this is success, not Error. It performs no allocation and changes no terminal state or
  stream ownership.
- `size` succeeds only for an attached terminal, returning its visible column and row counts (the
  visible window, never a larger scrollback buffer). A valid non-terminal stream returns
  `ErrorKind.InvalidInput()` with message `stream is not a terminal`; a native zero or negative
  dimension returns `ErrorKind.InvalidInput()` with message `terminal dimensions are invalid`.
- Both operations are stateless queries: neither initializes a loop, retains a terminal handle,
  changes terminal mode, or takes ownership of `stream`. A closed, stale, or unavailable stream
  returns the existing operation's Error classification; Terminal does not create a second
  liveness model.
- Windows classifies the current stdout handle at each logical `print` call (see below), so
  replacing a standard handle affects only the next call, never a process-lifetime cache; the
  same applies to `is_attached`/`size`, which classify fresh on every call.
- `IO.write` remains a byte-stream operation with no text conversion. Redirected `print` output
  keeps the original UTF-8 bytes on every target; only an attached Windows console additionally
  converts and delivers `print`'s text through native UTF-8-to-UTF-16 conversion and
  `WriteConsoleW`, through a fixed-size stack buffer with no heap allocation, chunked so a
  multibyte scalar (and the UTF-16 surrogate pair it can produce) is never split. Quoted text
  rendering batches each run of unescaped text through this same conversion instead of
  emitting it one byte at a time. `print` remains write-all and source-ordered on every target;
  conversion or console-write failure retains the exact runtime trap
  `[Runtime Error] standard output write failed`.
- Errors: Terminal operations first apply the shared IO native-error mapper (the same one File,
  IO, and Bytes use; Terminal keeps no second native-error classification table). Beyond the two
  local `InvalidInput` cases above, any other classification failure uses fixed message
  `terminal detection failed` and any other size-query failure uses fixed message
  `terminal size query failed`. Messages name no native error number, and every Error carries the
  Hexal call site's source location.
- Selection: constructing or matching a `TerminalSize` value alone emits only
  `hexal/terminal.h`'s type definition. A reachable `std/terminal.is_attached` or `size` call
  additionally emits `hexal/terminal.c`; neither operation selects the event bridge, the
  scheduler, the shared handle registry, or (on the qualified Windows target) libuv. A collection
  specialized over `TerminalSize` is rendered in each consuming module's own header rather than
  the shared collection component, the same header-ordering accommodation Signal uses, with no
  effect on program behavior.
- Raw mode, terminal input, resize notification, escape-sequence parsing, a full-screen UI
  framework, and a public libuv handle are not part of this implementation.

## Layout intrinsics

```text
size_of<T>() -> Size
align_of<T>() -> Size
```

- `size_of<T>()` and `align_of<T>()` require one explicit complete finite type and return Size C
  constant expressions. Reference-like types report source handle size. These operations do not make
  arbitrary Array lengths valid. `String<N>` reports its inline size and alignment (`size_of<String<31>>()`
  is 40 and `align_of<String<31>>()` is 8 on `x86_64-linux-gnu`) and requires a literal capacity;
  `size_of<Error>()` is 432 there.

## Volatile operations

```text
Ptr<T>.read_volatile() -> T
Ptr<mut T>.read_volatile() -> T
Ptr<mut T>.write_volatile(value: T) -> no value
```

- `read_volatile()` exists on `Ptr` in either mode; `write_volatile(value)` requires `Ptr<mut T>`. T is a fixed-width
  integer, Byte, or Size. Receiver/value evaluate once; nullable pointers narrow first. Volatile adds
  only C observability: no atomicity, synchronization, fence, device ordering, address exposure, or
  pointer arithmetic.

## C23 output contract

- Generated private identifiers apply one unconditional prefix to the full source spelling:

| Declaration | Prefix |
| --- | --- |
| binding | `hex_v_` |
| type | `hex_t_` |
| member | `hex_m_` |
| function/method | `hex_f_` |

- `HEX_` is reserved for generated macros. Names are never conditionally escaped, hashed, or
  truncated; an existing prefix is prefixed again. Foreign C names are outside this rule.
- Generated C preserves Hexal semantics instead of inheriting C undefined behavior for overflow,
  shifts, division edges, bounds, union payloads, or conversions. Target qualification (8-bit bytes,
  exact-width integers, IEC 60559 binary32/binary64 floats) is a supported GCC/Clang plus
  compatible-C-library contract, not a generated probe; only source-dependent target assertions
  (target-sized `Size` literals) are emitted.
- `<stdckdint.h>` is selected demand-first through `hexal.h` when checked runtime arithmetic or a
  selected signed wrapping specialization uses `ckd_add`/`ckd_sub`/`ckd_mul`; it is never emitted
  for a program with no selected checked arithmetic, and no private fallback definition is emitted.
  The qualified GCC/Clang plus compatible-C-library target provides the header, and
  the pinned compilers' overflow builtins provide the signed modulo-width stored result required
  by Hexal's wrapping contract.
- Generated C uses the standard facility whenever one implements the required semantics exactly: a
  C23 header, a C23 language feature, or a builtin documented by both GCC and Clang. It is used
  directly, never behind a helper that only delegates. A compiler-owned helper or lowering formula
  exists only where no standard facility applies, and reproducing a standard facility with generated
  predicates or target-width reasoning is a conformance bug. This is a contract on generated output;
  the compiler's own implementation language is unconstrained by it.
- `<string.h>` is selected demand-first when a generated copy, compare, or zero operation needs
  `memcpy`, `memcmp`, or `memset`; the standard function is called directly, never reimplemented with
  generated byte loops or delegating wrappers. A copy of a nonzero byte count guards the call so a
  null pointer is never passed with a zero count.
- Every generated diagnostic trap reports through one program-wide `hex_runtime_trap` (declared in
  `hexal.h`, defined once in the root module's C file, `[[noreturn]]`, owning `<stdio.h>`/`<stdlib.h>`
  selection). No per-family trap function exists. The one exception is the Task stack-overflow trap,
  which a signal handler (POSIX) or vectored exception handler (Windows) emits directly because the
  faulted stack cannot run `hex_runtime_trap`; it keeps the same `[Runtime Error]` message shape. An
  impossible compiler-internal union tag guard may retain a direct `abort()`.
- Nil renders the C23 `nullptr` keyword, no generated C spells `NULL`, and `nullptr_t` never appears
  as a type spelling. Nil alone
  selects no standard header; `<stddef.h>` is selected only by a real declaration consumer such as
  `size_t`.
- Objects/ADTs lower to source-ordered structs; unions to checked tagged values except pointer-null
  niches; generics are monomorphized. Object forward typedefs precede source-ordered definitions.
- `Fun<...>` uses its ordinary complete C function-pointer declarator in every position - binding,
  parameter, field, collection element - except a function's own return type, which cannot nest
  that declarator inside its own. There, and in any other position needing a standalone type
  specifier, the recursive spelling uses C23 `typeof` around the same declarator
  (`typeof(int32_t (*)(int32_t))`); no function-pointer typedef family is generated. A fixed binding
  reached through `typeof` qualifies the pointer value (`typeof(...) const name`); a mutable one
  omits that qualifier. Named module functions and anonymous literals all lower to ordinary C
  functions with no closure, environment, or dispatcher. An anonymous literal, including one bound
  directly to a local name, is file-scope `static` and named `hex_fun_<ordinal>`, sharing one
  module-local ordinal stream in checked-tree preorder; its binding name, if any, is checker
  metadata, never a C symbol. A private module-level function or method gets a `static` prototype
  in the module C file, emitted before every definition in source order, so a forward call or
  mutual recursion between module-level declarations compiles; an anonymous literal helper's own
  prototype is emitted the same way. An exported function or method's prototype is declared once,
  in the module header, and never duplicated as a static prototype in the module C file.
- Pointer qualification follows type layers only: Ptr adds pointee `const`, `Ptr<mut T>` does not, and a
  fixed binding adds trailing `const`. Object members themselves are unqualified. No
  qualifier-discarding cast is emitted.

| Hexal | C23 |
| --- | --- |
| `Ptr<Int32>` | `const int32_t *` |
| `Ptr<mut Int32>` | `int32_t *` |
| `Ptr<Ptr<Int32>>` | `const int32_t *const *` |
| `Ptr<mut Ptr<Int32>>` | `const int32_t **` |
| `Ptr<Ptr<mut Int32>>` | `int32_t *const *` |
| `Ptr<mut Ptr<mut Int32>>` | `int32_t **` |
| `Ptr<Unknown>` / `Ptr<mut Unknown>` | `const void *` / `void *` |

- Nil and EoS are zero-state language values. Nil exists only as a union member; EoS remains a valid
  standalone type. Neither has a stable foreign ABI. Their payload storage may be elided. Nil uses a
  null pointer only in a pointer-plus-Nil niche, spelled with the C23 `nullptr` keyword; general
  unions represent Nil and EoS with distinct active-member tags.

### Generated artifact split

- The in-memory compiler entrypoint is `Compile(sources map[string]string, entrypoint string,
  project Project) CompilationResult`: `sources` maps logical `.hex` filenames to complete source
  strings and `entrypoint` names the selected root module. `project` carries build-time settings
  that are not part of the language; its zero value selects every default. The compiler performs
  no filesystem operations.
- `project.Target` selects one compiler-owned target profile identity. Empty selects no profile:
  generated runtime components keep both platform paths, chosen at C-compile time, and output
  is host-neutral. Two identities are qualified: `x86_64-linux-gnu` (x86-64 Linux, glibc, LP64)
  and `x86_64-windows-gnu-ucrt` (x86-64 Windows, MinGW-w64 ABI over dynamic UCRT). Any other
  non-empty identity fails before lexing, including the older ambiguous `x86_64-windows-gnu`
  spelling, and callers cannot supply individual ABI facts. An explicit target selects its
  platform's entrypoint widening and runtime path; a Windows-only branch that remains in the
  text stays guarded by `#if defined(_WIN32)`. Each profile is native on its own host: `x86_64-linux-gnu`
  on `linux/amd64` and `x86_64-windows-gnu-ucrt` on `windows/amd64`.
- The result's `Files` map is the sole generated-artifact surface: `CompilationResult` has no
  `MainC`/`MainH` or other mirrored root-file fields, and `Files` is non-nil on every result.
- `CompilationResult.Stats` is one project-level summary per compilation call. It aggregates only
  the entrypoint and reachable modules, exposes no per-module statistics, and on failure reports
  work completed before failure. `Stats.PhaseSubtotal` sums the lex/check/generate stage durations;
  `Stats.TotalDuration` also covers the entry point's own overhead.
- `CompilationResult.HasCompilerDefect` reports whether `Stderr` carries an Unknown Error diagnostic
  — a compiler defect rather than a rejection of the program. It is derived from the structured
  diagnostics before rendering, is false on success and for every ordinary rejection, and lets the
  driver attribute a bug without parsing message text.
- Each compiler diagnostic in `CompilationResult.Stderr` renders as
  `[<Category> <stable-key>] <message>` followed, when positioned, by `at <logical-module>:<line>:<column>`
  or `at <line>:<column>` when no logical module is known. Locationless diagnostics omit the
  suffix. The public result remains `[]string`, with one rendered diagnostic per entry in unchanged
  order.
- A successful compilation produces exactly `hexal.h`, one C/header pair per reachable module
  under `modules/<canonical-path>.c/.h`, and the demand-driven component artifacts under
  `hexal/` that the reachable program selects; it returns `ExitSuccess` and has empty `Stderr`. A
  failed compilation produces no artifacts: `Files` is empty, `ExitCode` is `ExitFailure`, and
  `Stderr` carries the structured diagnostics. No failure C program is emitted.
- A module header emits each required foreign C include once, after `hexal.h` and the component
  headers and before any declaration that names a foreign type, in deterministic first-use order.
  The system and quoted forms are emitted exactly as written. The module C file still includes only
  its own generated header. A prepared or handwritten binding module emits no foreign function or
  type definition.
- `hexal.h` is the mandatory small program-support header, generated from the program-wide
  aggregate of all reachable modules. It opens with the demand-driven umbrella of portable standard
  headers (deterministic lexical order, only for families the reachable generated program selects;
  `<stdbool.h>`, `<limits.h>`, and `<float.h>` are never emitted), followed by the retained
  source-dependent `Size`-literal `SIZE_MAX` assertions, the shared `hex_eos` typedef exactly when
  generated C represents EoS, and the declaration of the one program-wide `hex_runtime_trap` when a
  selected path can trap. It contains no Heap, Slice, String, `String<N>`, Error, List, Dict, Array, Task,
  Channel, Mutex, or Atomic representation or helper, no String literal storage, no process-wide
  runtime state, no generic integer, byte-width, or float target probe, and no user-declared
  module-type definition or exported/cross-module user prototype. Its guard is `HEXAL_H`; every
  module header includes it, and it includes no other compiler-owned header.
- The component artifacts under `hexal/` own the generated runtime support, one family per file,
  emitted only when that family is reachable:   `hexal/runtime.c` (the `hex_runtime_trap`
  definition), `hexal/wrap.h`, `hexal/heap.h`/`hexal/heap.c`, `hexal/slice.h`, `hexal/string.h`/
  `hexal/string.c`, `hexal/error.h`, `hexal/list.h`, `hexal/dict.h`, `hexal/array.h`,
  `hexal/numeric.h`, `hexal/print.h`/`hexal/print.c`, `hexal/equality.h`,
  `hexal/concurrency.h`/`hexal/concurrency.c`, `hexal/io.h`/`hexal/io.c`, `hexal/seek.h`,
  `hexal/event.h`/`hexal/event.c`, `hexal/time.h`/`hexal/time.c`, `hexal/handle.h`/
  `hexal/handle.c`, `hexal/file.h`/`hexal/file.c`, and `hexal/network.h`/`hexal/network.c`. A
  program that links libuv (scheduler, Instant, File, or networking) also gets
  `hex_runtime_native_init`, declared in `hexal.h` and defined in
  `hexal/runtime.c`; root `main` calls it first, before any module statement and the scheduler, to
  install mimalloc as libuv's allocator. A program selecting the handle registry (currently: any
  File use) also gets a `hex_handle_registry_init` call immediately after. Their source of truth
  is the compiler's embedded C/header templates; a `.c` artifact is emitted only when it contains
  at least one definition.
  Component headers have stable `HEXAL_<COMPONENT>_H` guards, include `hexal.h` first and then only
  their declared dependencies (heap, slice, string, error, list, dict, array, numeric, print,
  equality, concurrency follow the acyclic component graph), and are emitted once per compilation.
  Component `.c` files include their matching header first and own the externally linked definitions
  and mutable state of that component; no module header or C file defines them.
- `hexal/numeric.h` is selected only when a reachable checked conversion, guarded integer division
  or remainder, guarded shift, same-width `bit_cast`, or endian conversion needs a helper. Direct
  and identity conversions select none. The header contains the merged canonical-key-sorted helper
  set once; endian helpers include the Array component they name.
- `hexal/print.h` and `hexal/print.c` are selected atomically when any reachable `print` call exists.
  Primitive `hex_print_*` declarations and definitions have one program-wide owner in that pair;
  module-owned aggregate print adapters remain in the consuming module header and include
  `hexal/print.h`.
- Text comparison demand is independent: `hex_equal_text` is emitted in the String component only
  for a text equality expression, a text Dict key, or a reachable recursive equality helper that
  compares a text member; `hex_compare_text` is emitted only for text ordering; `hex_hash_text`
  only for a text Dict key. Each is emitted once per program and reads text through the same
  `hex_text` view for every form and capacity, so no per-capacity or per-type text helper exists.
  The `String<N>` struct for each demanded capacity is defined once in `hexal/string.h`, before
  every header that names it, and none is emitted for a capacity the program does not use.
- `hexal/equality.h` owns one helper per canonical program-owned equality aggregate: builtin-element
  Array, Slice, and List specializations and the compiler-owned Error object, including recursively
  composed program-owned forms. User objects, ADTs, structural unions, and collections whose
  definitions are module-owned retain helpers in module headers. A program-owned helper is never
  duplicated in a module header; the component includes every required type-family header and
  standard header for its emitted bodies.
- `modules/<canonical>.h` is one module's header: it includes `hexal.h` first, then exactly the
  component headers that module's generated content requires (in dependency order), holds the
  module's types (ADTs, unions, objects) and stateless inline helpers (module-owned equality,
  module-owned print adapters, typed heap allocation helpers, typed atomic and
  channel/mutex/task inline helpers), the entry-adapter argument frames of its spawn sites,
  referenced complete type definitions, and its exported and referenced cross-module prototypes.
  Root selection adds nothing to this header. Its guard is
  `HEX_MODULE_<encoded-owner>_H`; it includes no module header and declares no `main()`. C consumers
  include the desired module header, not `hexal.h` directly.
- An entry module that captures at least one root binding emits one private
  owner-qualified environment struct type before every prototype and definition
  that names it, and never in a generated header. The one environment instance
  is an automatic local in `main`; no mutable C file-scope object is emitted. A
  captured binding's initializer assigns its environment field at the original
  source position, and an environment-dependent function or method receives one
  mutable environment pointer as its first C parameter in both its definition
  and every direct call.
- Each module constant gets one immutable definition in its owning module's `.c` file, in checked
  declaration order, using generated symbol `hex_v_<encoded-owner>_<name>`. A private constant is
  `static const`; an exported constant is `const` and additionally gets one `extern const`
  declaration in its owning module's header. An importer never includes another module's header for
  this: every module referencing a foreign module constant declares its own `extern const`
  prototype for it, exactly like a foreign function or method prototype. No module-init function or
  accessor wrapper exists; a consumer reads or takes the address of the same immutable object the
  owning module defines.
- `modules/<canonical>.c` is one module's translation unit: it includes only its own module
  header, and declares a `static` prototype for each of its private functions and methods, in
  source order, before any of that module's function or method definitions. It then defines its
  private functions and methods with internal `static` linkage, its exported functions and methods
  and spawned functions with external linkage, its monomorphized specializations, and its spawn
  entry adapters (external linkage, declared in the concurrency component). The selected root
  module's C file owns `int main(void)`, which executes the root
  module's executable statements and returns `0`, C's successful termination status;
  with concurrency it initializes the scheduler first and completes the
  root task before returning. The root module C file is not the runtime container: process-wide
  runtime definitions and state live in the component artifacts. No non-root module declares or
  defines `main()` or process-wide runtime state.
- Every external runtime symbol has exactly one declaration (in its owning component header or
  `hexal.h`) and one definition (in its owning component C file). A build driver must compile every
  `.c` entry returned in `Files`, not only those under `modules/`.
- Module artifacts map to the source file with `#line` directives naming the module's logical
  source key; compiler-generated runtime machinery has no user-source mapping.
- A module owner encodes as `m` followed, for each canonical path component, by its decimal UTF-8
  byte length, `_`, and case-preserved source spelling. Module-owned symbols are
  `hex_<kind>_<encoded-owner>_<name>`; guards are `HEX_MODULE_<encoded-owner>_H`.
- Generated definition-keying type names are injective on canonical type identity: two distinct
  Hexal types never share a generated C name that introduces a definition. Nominal objects and
  ADTs spell `hex_t_<encoded-owner>_<Name>`; structural unions spell `hex_t_` plus each canonical
  member's sanitized display name joined with `_`. Uniqueness is established once per compilation
  by the shared constructed-type arena: every concrete nominal name is reserved before any union
  is constructed and never moves, while a union whose base name another distinct type already owns
  appends `_0`, `_1`, and so on. The same union written in any module spells one C type.
- General tagged unions and concrete ADTs share one program-wide discriminant enum `hex_tag`,
  emitted once in `hexal.h` before every module-header use and omitted when no
  reachable general union or ADT exists. Each canonical union-member type and each canonical ADT
  variant resolves to exactly one `hex_tag_<label>` constant, deduplicated by canonical identity
  and sorted by it; labels are the encoded module owner plus the sanitized name for nominal types
  (`hex_tag_m3_app_Shape_Circle`) and the bare sanitized name for compiler-owned builtins
  (`hex_tag_Int32`, `hex_tag_Nil`). Colliding labels resolve in identity order: the first keeps
  the base, later ones append `_0`, `_1`, and so on. A union's payload member is an inline
  anonymous union with one `hex_m_<label>` field per member; Nil, EoS, and payload-free ADT
  variants have a discriminant and no payload field. Every union and ADT struct carries
  `hex_tag tag`; widening copies the source tag. Generated tag spellings flow only through the
  registry: a lookup for an identity that was never collected is a compiler error, never a
  locally reconstructed name or ordinal.
- Collection C names derive from the element's display name (`hex_list_`, `hex_dict_`, `hex_slice_`,
  `hex_mut_slice_`, `hex_array_`, `hex_task_`, `hex_channel_`, `hex_atomic_`). When same-named elements from distinct
  modules would derive one C name, the later interned specialization appends `_` plus the encoded
  owner of its element's defining module; resolution happens once at interning, so a typedef and
  every helper suffix derived from its name stay paired.
- The artifact set contains no top-level `main.c`, `main.h`, or compatibility header; the
  entrypoint's canonical module C file supplies `main()`.
- Invalid or unsupported source produces a structured diagnostic and is never silently omitted or
  partially generated. Syntax failures, static-semantic failures (Name and Type Errors), Module
  Errors, dynamic traps, and Unknown Error are distinct externally visible classes. Unknown Error
  identifies an unclassifiable compiler inconsistency, not a source-program error.

## Build modes

- Two modes exist: `debug` and `release`. `-mode` selects one; an omitted or empty value defaults
  to `debug`. An unrecognized value fails before compilation.
- **Mode-independence:** generated C is byte-identical in both modes. No mode changes program
  semantics, stdout, stderr, exit status, trap messages, or evaluation order. A mode selects how the
  generated C is compiled, never what it is. Bounds, overflow, division, conversion, freed-state,
  and handle checks are language semantics, so no mode may drop them.
- The one documented exception: a mode may change the point at which a stack-overflow resource
  limit is reached, because optimization changes stack-frame sizes. That is a resource limit, not a
  semantic difference.
- **Debug** keeps the program unoptimized with target-native debug information and a
  non-recoverable undefined-behaviour backstop. The backstop stops the program at the point of the
  fault; it is an instrument for finding generator defects, not a different language. Debug detects
  undefined behaviour on every qualified lane. On the Linux lane the backstop uses the diagnostic
  runtime (`-fsanitize=undefined -fno-sanitize-recover=all`) and reports which check failed and
  where; on the Windows lane it uses trap mode
  (`-fsanitize=undefined -fsanitize-trap=undefined`) because this release ships no MinGW UBSan
  runtime, so the same faults halt with no diagnostic text. Debug on the Linux lane also carries
  `-fsanitize=leak`: allocations still live at exit are reported on stderr with their allocation
  stack traces, and the report does not change exit status — holding an allocation to process exit
  is not an error in a language with explicit manual cleanup. `LSAN_OPTIONS` overrides that
  default. The leak report is absent on lanes without LeakSanitizer and absent in release.
- **Release** optimizes, emits no debug information, strips the executable, and discards unused
  sections. It carries no sanitizer instrumentation.
- Neither mode is a correctness contract: a program correct in one is correct in the other.

## Excluded features

- FFI: automatic C-header binding generation, C exports, callbacks into Hexal, function-pointer
  values, variadic calls, raw C unions, bit-fields, flexible-array members, C atomics, extended
  numeric types, non-default calling conventions, dynamic libraries, and C project manifests are
  deferred. The handwritten and prepared-binding surface in the C interoperability section is
  implemented.
- Memory: pointer arithmetic, pointer casts, and raw address manipulation outside an `unsafe do ... end`
  block.
- Control/iteration: ranges, counted loops, user iterators, mutable iteration binders, exceptions.
- Functions/concurrency: closures, async/await, coroutines, user threads, task groups, `select`,
  unbounded/rendezvous Channels, nonblocking Channel operations, memory-order arguments.
- Extensibility: operator overloading, user truth/display/hash protocols, generic constraints,
  reflection, serialization schemas, runtime type objects.
- Expressions: compound assignment, increment/decrement, conditional operator, numeric suffixes,
  wrapping/saturating conversion or arithmetic modes.
- I/O: the protected built-in `File`/`FileMode`/`Stdio` names (the library surfaces are
  `std/fs`, `std/io`, and `std/net`), Path manipulation, asynchronous I/O, and in-memory output
  builders.
