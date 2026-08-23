# Hexal Language Reference

This file is the sole normative syntax and semantic reference. `status.md` tracks implementation.
Compiler behavior that disagrees with this file is a conformance bug.

## Grammar

The grammar defines source shape only. Semantic rules in the remainder of this file may reject a
grammatically valid form.

Two lexical/parser rules are not expressible in EBNF:

- Tokens use maximal munch. Inside nested type-argument lists only, one `>>` token may close two
  levels; in expression position it is always one shift token.
- `|` is a union separator in type position and bitwise-or in expression position; parser context
  selects the grammar.

```ebnf
program = lexical-separation , { top-level-item } ;
lexical-separation = ? whitespace and comments are discarded between tokens,
                       except where same-line is required ? ;
same-line = ? no line break occurs before the next token ? ;

top-level-item = import-declaration | [ "export" ] , declaration-item
                 | statement ;
import-declaration = "module" , identifier , "=" , "import"
                     , module-path-literal ;
module-path-literal = ? a quoted literal scanned only when the previous
                        token is "import" on the same line; the payload
                        between quotes is taken verbatim (no escape decoding);
                        a backslash in the payload is invalid ? ;
declaration-item = type-declaration | function-declaration
                   | implementation-declaration ;
type-declaration = "type" , identifier , [ generic-parameter-list ]
                   , "=" , type-definition-expression ;
function-declaration = "fun" , identifier , [ generic-parameter-list ]
                       , signature , "do" , block , "end" ;
implementation-declaration = "impl" , type-expression , "." , identifier
                             , [ generic-parameter-list ] , signature
                             , "do" , block , "end" ;
signature = "(" , [ parameter-list ] , ")" , [ ":" , type-expression ] ;
parameter-list = parameter , { "," , parameter } ;
parameter = identifier , ":" , type-expression ;
generic-parameter-list = "<" , identifier , { "," , identifier } , ">" ;

statement = non-control-statement | return-statement
            | if-statement | while-statement | for-statement
            | local-function-declaration ;
non-control-statement = declaration | assignment | call-statement
                        | try-statement
                        | "break" | "continue"
                        | defer-statement | errdefer-statement ;
local-function-declaration = "fun" , identifier
                             , [ generic-parameter-list ]
                             , signature , "do" , block , "end" ;
block = { statement } ;
declaration = [ "mut" ] , identifier
              , ( ":" , type-expression , ":=" | ":=" ) , expression ;
assignment = assignment-target , "=" , expression ;
assignment-target = place-expression ;
call-statement = ? call-expression whose first token is identifier or "self" ? ;
try-statement = "try" , unary-expression ;
return-statement = "return" , [ same-line , expression ] ;
defer-statement = "defer" , expression ;
errdefer-statement = "errdefer" , expression ;
if-statement = "if" , expression , "then" , block
               , { "elseif" , expression , "then" , block }
               , [ "else" , block ] , "end" ;
while-statement = "while" , expression , "do" , block , "end" ;
for-statement = "for" , for-binders , "in" , expression
                , "do" , block , "end" ;
for-binders = identifier
              | identifier , "," , identifier
              | identifier , "," , identifier , "," , identifier ;

type-definition-expression = object-type-expression
                             | adt-type-expression | type-expression ;
object-type-expression = "{" , member-declaration
                         , { "," , member-declaration } , [ "," ] , "}" ;
member-declaration = [ "mut" ] , identifier , ":" , type-expression ;
adt-type-expression = adt-variant , { adt-variant } ;
adt-variant = "|" , identifier , [ "as" , adt-payload ] ;
adt-payload = "{" , payload-member
              , { "," , payload-member } , [ "," ] , "}" ;
payload-member = identifier , ":" , type-expression ;

type-expression = union-type-expression ;
union-type-expression = primary-type-expression
                        , { "|" , primary-type-expression } ;
primary-type-expression = named-type | generic-type | array-type
                          | pointer-type | function-type-expression
                          | "(" , type-expression , ")" ;
named-type = identifier - special-form-type-constructor
             | identifier - special-form-type-constructor , "." , identifier
               , { "." , identifier } ;
generic-type = generic-type-name , type-argument-list ;
generic-type-name = identifier - special-form-type-constructor ;
special-form-type-constructor = "Array" | "Ptr" | "MutPtr" | "Fun" ;
type-argument-list = "<" , type-expression
                     , { "," , type-expression } , ">" ;
array-type = "Array" , "<" , type-expression
             , "," , positive-decimal-literal , ">" ;
positive-decimal-literal = nonzero-decimal-digit
                           , { decimal-digit | "_" , decimal-digit } ;
pointer-type = pointer-constructor , "<" , type-expression , ">" ;
pointer-constructor = "Ptr" | "MutPtr" ;
function-type-expression = "Fun" , "<" , "(" , [ type-list ] , ")"
                           , [ ":" , type-expression ] , ">" ;
type-list = type-expression , { "," , type-expression } ;

expression = or-expression ;
or-expression = and-expression , { "or" , and-expression } ;
and-expression = bitwise-or-expression , { "and" , bitwise-or-expression } ;
bitwise-or-expression = bitwise-xor-expression
                        , { "|" , bitwise-xor-expression } ;
bitwise-xor-expression = bitwise-and-expression
                         , { "^" , bitwise-and-expression } ;
bitwise-and-expression = equality-expression
                         , { "&" , equality-expression } ;
equality-expression = type-test-expression
                      , { equality-operator , type-test-expression } ;
type-test-expression = relational-expression , [ "is" , type-expression ] ;
relational-expression = shift-expression
                        , { relational-operator , shift-expression } ;
shift-expression = additive-expression , { shift-operator , additive-expression } ;
additive-expression = multiplicative-expression
                      , { additive-operator , multiplicative-expression } ;
multiplicative-expression = unary-expression
                            , { multiplicative-operator , unary-expression } ;
unary-expression = unary-operator , unary-expression
                   | "try" , unary-expression
                   | "spawn" , call-expression
                   | "ref" , place-expression
                   | postfix-expression ;

place-expression = place-primary , { place-suffix } ;
place-primary = identifier | "self" ;
place-suffix = "." , identifier | index-suffix ;
postfix-expression = postfix-base , { postfix-suffix } ;
postfix-base = primary-expression | type-qualified-primary ;
postfix-suffix = member-suffix | index-suffix | call-suffix ;
member-suffix = "." , identifier ;
type-qualified-primary = identifier , type-argument-list
                         , "." , identifier , [ variant-payload ] ;
call-suffix = call-arguments | type-argument-list , call-arguments ;
call-expression = postfix-base , { postfix-suffix } , call-suffix ;
call-arguments = same-line , "(" , [ argument-list ] , ")" ;
argument-list = expression , { "," , expression } ;
index-suffix = "[" , expression , "]" ;

primary-expression = identifier | "self" | object-literal
                     | qualified-record-variant
                     | array-literal | match-expression
                     | anonymous-function-literal
                     | integer-literal | decimal-floating-literal
                     | byte-literal | rune-literal | string-literal
                     | "true" | "false" | "nil" | "eos"
                     | "(" , expression , ")" ;
anonymous-function-literal = "fun" , [ generic-parameter-list ]
                             , signature , "do" , block , "end" ;
object-literal = identifier , [ type-argument-list ]
                 , "{" , [ member-initializer-list ] , "}" ;
qualified-record-variant = identifier , "." , identifier , variant-payload ;
variant-payload = "{" , [ member-initializer-list ] , "}" ;
member-initializer-list = member-initializer
                          , { "," , member-initializer } , [ "," ] ;
member-initializer = identifier , "=" , expression ;
array-literal = "[" , [ expression-list , [ "," ] ] , "]" ;
expression-list = expression , { "," , expression } ;

match-expression = "match" , match-scrutinee , [ "is" ]
                   , match-arm , { match-arm } , "end" ;
match-arm = "|" , match-pattern , "then" , match-arm-expression ;
match-scrutinee = ? expression ending before the first unparenthesized
                    "is" type-mode marker or match-arm "|" ? ;
match-arm-expression = ? expression ending before the next unparenthesized
                         match-arm "|" or the matching "end" ? ;
match-pattern = "else" | "true" | "false"
                | qualified-variant-pattern | primary-type-expression ;
qualified-variant-pattern = identifier , [ type-argument-list ]
                            , "." , identifier ;

unary-operator = "-" | "!" | "~" ;
multiplicative-operator = "*" | "/" | "%" ;
additive-operator = "+" | "-" ;
shift-operator = "<<" | ">>" ;
relational-operator = "<" | "<=" | ">" | ">=" ;
equality-operator = "==" | "!=" ;

identifier = identifier-text - reserved-word ;
identifier-text = ASCII-letter , { ASCII-letter | decimal-digit | "_" } ;
reserved-word = "true" | "false" | "nil" | "eos" | "mut" | "ref"
                | "type" | "and" | "or" | "is" | "fun" | "impl"
                | "end" | "return" | "if" | "elseif" | "else"
                | "while" | "break" | "continue" | "defer" | "try"
                | "errdefer" | "spawn" | "as" | "match" | "then"
                | "self" | "for" | "in" | "do" | "module" | "import"
                | "export" ;
integer-literal = decimal-integer | hexadecimal-integer
                  | binary-integer | octal-integer ;
decimal-integer = "0" | nonzero-decimal-digit
                  , { decimal-digit | "_" , decimal-digit } ;
hexadecimal-integer = "0x" , hex-digit
                      , { hex-digit | "_" , hex-digit } ;
binary-integer = "0b" , binary-digit
                 , { binary-digit | "_" , binary-digit } ;
octal-integer = "0o" , octal-digit
                , { octal-digit | "_" , octal-digit } ;
decimal-floating-literal = decimal-integer , "." , decimal-digit-sequence
                           , [ exponent-part ]
                           | decimal-integer , exponent-part ;
exponent-part = ( "e" | "E" ) , [ "+" | "-" ]
                , decimal-digit-sequence ;
decimal-digit-sequence = decimal-digit
                         , { decimal-digit | "_" , decimal-digit } ;

string-literal = '"' , { string-character | string-escape } , '"' ;
string-escape = '\' , ( "'" | '"' | '\' | "n" | "r" | "t" | "0"
                | unicode-escape ) ;
byte-literal = "b" , "'" , byte-literal-body , "'" ;
byte-literal-body = printable-ASCII-character | byte-escape ;
byte-escape = '\' , ( '\' | "'" | "n" | "r" | "t" | "0"
              | "x" , hex-digit , hex-digit ) ;
rune-literal = "'" , rune-literal-body , "'" ;
rune-literal-body = Unicode-scalar | rune-escape ;
rune-escape = '\' , ( '\' | "'" | '"' | "n" | "r" | "t" | "0"
              | unicode-escape ) ;
unicode-escape = "u{" , hex-digit , { hex-digit } , "}" ;

line-comment = "--" , { non-newline-character } ;
multiline-comment = "--[" , { comment-character } , "]--" ;
whitespace = " " | horizontal-tab | carriage-return | newline ;
string-character = ? any Unicode scalar except '"', "\", CR, or LF ? ;
printable-ASCII-character = ? one ASCII byte from 0x20 through 0x7E,
                              except "'" or "\" ? ;
Unicode-scalar = ? one Unicode scalar except "'", "\", CR, or LF ? ;
non-newline-character = ? any character except CR or LF ? ;
comment-character = ? any character not beginning the sequence "]--" ? ;
horizontal-tab = ? U+0009 ? ;
carriage-return = ? U+000D ? ;
newline = ? U+000A ? ;

ASCII-letter = "A" | "B" | "C" | "D" | "E" | "F" | "G" | "H" | "I"
               | "J" | "K" | "L" | "M" | "N" | "O" | "P" | "Q" | "R"
               | "S" | "T" | "U" | "V" | "W" | "X" | "Y" | "Z"
               | "a" | "b" | "c" | "d" | "e" | "f" | "g" | "h" | "i"
               | "j" | "k" | "l" | "m" | "n" | "o" | "p" | "q" | "r"
               | "s" | "t" | "u" | "v" | "w" | "x" | "y" | "z" ;
nonzero-decimal-digit = "1" | "2" | "3" | "4" | "5"
                        | "6" | "7" | "8" | "9" ;
decimal-digit = "0" | nonzero-decimal-digit ;
binary-digit = "0" | "1" ;
octal-digit = "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" ;
hex-digit = decimal-digit | "a" | "b" | "c" | "d" | "e" | "f"
            | "A" | "B" | "C" | "D" | "E" | "F" ;
```

## Language boundary

- Hexal is a statically typed systems language that lowers to readable C23 with `#line` mappings.
- Invalid or unsupported source fails closed under the diagnostic contract below.
- Values follow C-style shallow copying. Allocation and cleanup are explicit; there are no moves,
  borrow states, retain counts, implicit destructors, or compiler-enforced exactly-once cleanup.
  That names the mechanisms the language lacks, not a limit on what it diagnoses: see Allocation
  and lifetime for which cleanup misuses are rejected.
- Native modules are implemented; each `.hex` source is one module.
- C interop, Arena, and Pool remain draft features and are not part of this language.

## Programs, names, and bindings

- A source file contains ordered type, function, method, and executable declarations/statements.
  Executable statements occur only in the root program and lower to automatic locals in `main`.
- Hexal has no native globals, global constants, `global`, or `static`. State is local, allocated,
  or passed explicitly.
- Functions and methods are file-scope declarations. Nested functions and closures do not exist;
  functions cannot capture root or lexical locals.
- `return` is valid only inside a function or method body. The root program has no declared result.
- Declarations become visible in source order. A function may call itself or an earlier function;
  forward calls and mutual recursion are unavailable.
- Type and value names share one namespace. Protected names cannot be redeclared or shadowed.
  Protected types are every scalar plus `Size`, `Byte`, `Rune`, `String`, `Strand`, `Nil`, `EoS`,
  `Unknown`, `Heap`, `Error`, `RuneCursor`, `Mutex`, and constructors `Ptr`,
  `MutPtr`, `Fun`, `Array`, `View`, `List`, `Dict`, `Task`, `Channel`, `Atomic`.
  Protected operations are `print`, `size_of`, and `align_of`.
- Every value-binding declaration uses `:=` and states its type exactly once, on one side or the
  other. `name: T := initializer` states it on the left; `name := initializer` says the
  initializer states it, and is rejected when the initializer is contextual — an integer, float,
  or string literal, `nil`, an array literal, or a `match` whose every arm is contextual. Stating
  it on neither side is an error. Written parameters, members, ADT payloads, and results always
  require an explicit type. Compiler-typed `self` and `for` binders are the remaining exceptions.
- `=` assigns to an existing writable place. It does not introduce a value binding. Type
  definitions, module aliases, object literal member initializers, and ADT payload initializers
  retain their grammar-defined uses of `=`.
- Bindings and object members are fixed by default. `mut` permits replacement and appears only on
  their declarations. Parameters, `self`, and `for` binders are fixed and cannot be shadowed in
  their own scopes.
- Member assignment requires a writable root and `mut` at every object-member step. Dereference
  writability comes from the pointer type.

## Modules

- `Compile` receives a source map and one entrypoint logical key. Source-map keys use `/`, are
  case-sensitive, and do not denote or inspect host filesystem paths. The entrypoint must exactly
  name one supplied source.
- A module's canonical identity is its logical key without the trailing `.hex`. The logical key,
  not an absolute host path or import alias, determines nominal type, function, method, generic,
  specialization, generated-symbol, and artifact identity. Same-named declarations in distinct
  canonical modules are distinct.
- `module Alias = import "<path>"` binds `Alias` only in the importing module. Imports form the
  module prefix and must precede every other item. Import aliases occupy their own namespace and
  cannot shadow or be shadowed.
- An import path starts with `./` or one or more `../`, uses `/`, and contains identifier path
  components with an optional terminal `.hex`. Resolution is lexical relative to the importing
  module's directory, strips the optional `.hex`, and cannot walk above the logical source-map
  root. Resolution consults only the supplied source map and requires exactly one case-sensitive
  logical key with the resulting canonical identity.
- Only the entrypoint and its transitive dependencies are compiled. Unreachable source-map entries
  produce no diagnostics, artifacts, or statistics. Each reachable canonical module is processed
  once. Duplicate imports of one canonical module and every dependency cycle are Module Errors.
- For identical source strings and entrypoint, traversal, diagnostics, statistics, and generated
  file contents are deterministic. `Files` map iteration order has no meaning.
- The entrypoint module may contain declarations and executable statements. Every imported module
  is declarations-only: it has no executable statements, value bindings, initializer, runtime
  Heap, import-time effects, or final-expression result.
- Declarations are private by default. `export` may prefix only a module-level type, function, or
  implementation declaration. An importer accesses exported declarations only through its local
  alias; wildcard and unqualified imports do not exist.
- An exported declaration's complete interface closes over builtins and exported types only,
  including types reached through aliases, aggregates, generic arguments, parameters, results,
  receivers, members, and ADT payloads. Private types may remain inside an exported function or
  generic body when absent from its interface.
- Qualified types, functions, ADT variants, and exported methods retain the defining module's
  identity; renaming an import alias changes no identity. Within each module, declarations retain
  source-order visibility. Successfully checked exports are available to importers regardless of
  the export's textual position in the defining module.
- Only a nominal type's defining module may declare implementations for it. Imported types and
  transparent aliases of imported types may call exported methods but cannot receive new methods.
- Generated module artifacts, symbol linkage, header ownership, and source mapping are specified
  exclusively under Generated artifact split.

## Values, copying, and evaluation

- **Representation follows ownership, not shape.** A type that owns an allocation is a
  pointer-sized handle: it is passed as a pointer and copies alias one allocation. These are
  exactly the types exposing `free` — `String`, List, Dict, Channel, Mutex — plus Task, whose
  storage the scheduler reclaims through join or detach. A type that is inline or borrows
  storage is a value: it is passed by value and a copy copies its region. These are scalars,
  `Strand`, Array, objects, ADTs, and View. A new type derives its representation from this
  rule rather than from resemblance to an existing one.
- The rule is ownership because the C struct shape does not decide it: `String` and `View<T>`
  are both a pointer and a length, and differ only in that `String` owns its bytes while a View
  borrows them. `String` is therefore a handle and a View is a descriptor value.
- Every value is stored inline. Every copy copies the C representation. Scalars and
  inline aggregates (`Strand`, Array, objects, ADTs) copy all inline bytes. Pointers and
  `String`, List, Dict, Task, Channel, Mutex copy their handle representation. View copies
  its pointer-length descriptor. Heap copies a compile-time allocator identity.
- Assignment, arguments, returns, object/ADT construction, collection insertion, union injection,
  and Task capture are shallow copies. Copying does not invalidate the source.
- Values referring to external state include String, List, Dict, Task, Channel, Mutex,
  RuneCursor, View, and aggregates containing them. Copies alias the same state. Freeing one
  alias leaves others dangling; losing the last handle can leak.
- Every value is copyable except `Atomic<T>` and inline aggregates transitively containing one.
  Atomic containment traversal stops at every pointer and handle indirection.
- Full statements execute in source order. Unless stated otherwise, operand order, call-argument
  order, receiver-versus-argument order, and object-initializer order are C23-unspecified.

## Position eligibility

These are the positions that hold a value. Other sections name them directly when narrowing what
they accept.

```text
Binding          ObjectMember     ADTPayload       UnionMember
ArrayElement     ViewElement      ListElement      DictValue
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
| `Rune` | Unicode scalar value | `uint32_t` |
| `Nil` | zero-state `nil`; valid only as a union member | no stable foreign ABI |
| `EoS` | zero-state completion `eos`; valid standalone | no stable foreign ABI |

- `Byte` is the canonical spelling wherever the value is raw storage rather than a number:
  `View<Byte>`, `Array<Byte, N>`, `List<Byte>`, and byte-oriented parameters and results. `UInt8`
  is canonical wherever the value is an 8-bit integer participating in arithmetic, comparison, or
  conversion. Both remain the same canonical type; this rule governs spelling, not semantics
  (RFC 0063).

- `Size` always lowers directly to the selected C compiler's `size_t`; that target decides width,
  range, alignment, and representation. Hexal has no Size width, no width assertion, and never
  rejects a conforming target for its `sizeof(size_t)`. Size remains canonically distinct from
  fixed-width integers.
- Rune is distinct from UInt32 and excludes surrogates. `Int`, `UInt`, `Float`, `Double`, `Char`,
  `Long`, `ISize`, and `Void` are not built-ins.
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

- `type Alias = T` is transparent: identical canonical type, representation, and operations, with
  no C typedef. Targets resolve in source order; recursive aliases are invalid.
- Objects are nominal, ordered inline values with at least one member. Identical layouts remain
  distinct. Object literals name every member exactly once in any order; trailing comma is allowed.
  Initializer evaluation order is unspecified.
- Identity is canonical and recursive, never derived from display names: same-named nominal types
  in distinct modules are distinct, identical layouts included, and constructed builtin generic
  types (pointer, nullable, function, Array, View, List, Dict, Task, Channel, Atomic, union) intern
  once per compilation and are shared by every module. `List<Int32>` written in two modules is one
  type, while `List<m.Point>` and `List<s.Point>` over same-named `Point` types are two.
- Direct and mutual by-value recursive layouts are invalid; pointer-indirect recursion is valid.
- Pointer member access auto-dereferences. `.value` explicitly accesses the whole pointee and is
  required for non-object pointees.

### Pointers and nullability

- `Ptr<T>` is non-null, non-owning, and read-only through the pointer. `MutPtr<T>` is non-null,
  non-owning, and writable through the pointer.
- `ref place` is the only address-taking form: writable places yield MutPtr, fixed places Ptr.
- MutPtr weakens implicitly to Ptr at the outermost layer only. No upgrade or nested weakening.
- `.value` dereferences. Nullability is explicit `P | Nil`; nullable data pointers must be narrowed
  with `== nil`, `!= nil`, or match before dereference. The null niche adds no tag or allocation.
- `Unknown` is incomplete and valid only behind Ptr/MutPtr. One pointer layer may erase to or recover
  from Unknown; Unknown cannot be stored or dereferenced by value.
- String, List, Dict, and View cannot be Ptr/MutPtr pointees. Each already carries its own
  aliasing and invalidation rules over borrowed or allocated storage, and a pointer to one would add
  a second aliasing layer with no defined semantics. This is not a general handle exclusion:
  `Task<R>`, `Channel<T>`, and `Mutex` are shared by handle copy and are valid pointees.
- `Atomic<T>` cannot be a direct Ptr/MutPtr pointee. `Ptr<Atomic<T>>` and
  `MutPtr<Atomic<T>>` are invalid type expressions.
- Pointers name one object. Arithmetic, indexing, ordering, subtraction, integer conversion,
  `bit_cast`, one-past values, increment/decrement, and compound assignment are unavailable.

### Functions and methods

- `fun` declares a function, not mutable storage. `Fun<(P1, P2) : R>` is a function-pointer type;
  omit `: R` for no result.
- Fun is valid as a binding, function parameter, parameter inside another Fun, function result,
  object member, ADT payload, Array/View/List element, Dict value, Task argument or result,
  Channel element, or union member. It is invalid as a Ptr/MutPtr pointee, a `ref` target, a
  Dict key (function values have no equality or hash contract), and a direct heap-allocation
  type. Function declarations are not addressable. Every accepted position stores or copies one
  ordinary C function pointer; no position adds an environment, ownership operation, or
  allocation. An object may store a Fun value as an explicit dispatch table; the field holds one
  ordinary C function pointer and is called as `table.operation(args)` with no hidden receiver or environment.
- Calls require exact arity and assignable arguments. No-result calls are statements only. Results
  must match their declarations; result-producing bodies cannot fall through.
- Infallible commands with no payload return no value. Fallible commands with no success payload
  return `Nil | Error`.
- `impl Receiver.method(...)` adds an implicit fixed `self`, no fields or runtime dispatch. User
  targets are nominal `T`, `Ptr<T>`, or `MutPtr<T>`. Value receivers copy; Ptr reads caller storage;
  MutPtr may write its `mut` members.
- Receiver adaptation order: exact target; outermost MutPtr weakening; pointer dereference to copied
  `T`; implicit `ref` from a capability-compatible addressable `T`.
- One method name exists at most once across an object's receiver forms. It cannot equal a member
  name or be extracted as a function value.
- `receiver.name(arguments)` resolves to a method first. Only when no method named `name` exists,
  and the receiver's type has a member `name` of an exact or nullable `Fun<...>` type, does the
  call resolve to an indirect call through that member instead, with the member's signature
  governing arity and argument types; a nullable member must be narrowed before the call. This
  needs no precedence rule beyond the existing one: a member and a method already share one
  namespace, so a type cannot declare both under the same name. A member matching the name but not
  `Fun<...>` is rejected with `member <name> is not callable; its type is <type>`, distinct from
  `<type> has no method named <name>` when the name is neither a method nor a member.
- There is no overloading, default/named/variadic argument syntax, static method, or closure.

#### Anonymous function literals and local named functions

- `fun (p1: T1, p2: T2): R do ... end` is a non-capturing function value with type
  `Fun<(T1, T2) : R>`; omitting `: R` gives `Fun<(T1, T2)>` and forbids a value-returning `return`.
  A generic literal declares type parameters between `fun` and its signature: `fun<T>(value: T): T do ... end`.
  The literal is a postfix base: a same-line call suffix invokes it directly, valid only where an
  expression is expected, never as a call statement. It declares no source name and cannot use `export`.
- `fun name(...) do ... end` at statement position (inside a function or method body, or nested
  inside module-level control flow) is a local named function declaration: the same syntax as a
  module function without `export`. `export fun` remains rejected outside module scope.
- An inferred fixed declaration (`name := ...`) whose initializer is directly a function literal,
  after stripping only grouping-only parentheses, is declaration sugar over the same function form
  as a named declaration: it emits the helper function and no function-pointer storage, is fixed
  and self-recursive, and is accepted in a declaration-only imported module. It remains private,
  since `export` prefixes only the named function-declaration form. A written type, `mut`, a call
  or other suffix on the initializer, or a binding initialized from an existing function value all
  remain ordinary runtime data with no self-recursion name.
- Named functions, local named functions, and anonymous literals share one signature, body,
  return-flow, defer, and generic-specialization implementation; they differ only in how their own
  name and result are bound. A literal or local function is checked in a fresh closed scope: it may
  use its own parameters, bindings it declares, named functions visible at its source position
  (module functions and earlier local functions in an enclosing block, including its own body when
  it has a self-recursion name), and Fun values it receives or declares. It cannot read an enclosing
  local, parameter, `self`, or root Fun/data binding; no environment, capture, or heap allocation is
  generated for this. A mutable receiving binding cannot supply a stable self-recursion identity, so
  referring to it from the literal's own body remains an invalid capture.
- A local named function or a direct inferred fixed literal binding is visible from its declaration
  onward in its containing lexical block, including from later local declarations in that same
  block, and is hidden outside the block. A call to a not-yet-declared later local function in the
  same block is rejected; mutual recursion through a later declaration is unavailable. Two
  same-named local declarations in disjoint blocks are distinct.
- A literal or local function may itself declare generic type parameters. Its own parameter names
  must be distinct from every generic parameter active in an enclosing generic function, method, or
  local declaration; redeclaring an enclosing name is a duplicate-parameter error, while an
  unshadowed enclosing parameter remains usable in the inner signature and body. Every open generic
  function, named or local, has a compiler-owned template identity distinct from its source name, so
  two local generic declarations that reuse a name in disjoint scopes specialize independently.
  Contextual specialization (an exact expected `Fun<...>` type, or a call's own arguments) applies
  to a generic literal exactly as it does to a named generic function.

### Generics

- User parameters are types only. Compiler-owned `Array<T, N>` uses a positive integer literal N.
- Specializations are invariant and keyed by declaration identity plus canonical arguments; repeated
  requests reuse one. Only reachable concrete specializations emit C; there is no erasure or runtime
  generic representation.
- Explicit type arguments must be complete. Otherwise inference uses typed arguments, expected
  result, and initializer fields; conflicts or unresolved parameters are errors.
- A balanced `<...>` is generic syntax only when immediately followed by call arguments, a qualified
  constructor/member, or object literal. Otherwise `<`, `>`, and `>>` are operators.
- A generic function value needs an exact expected Fun type. Generic methods inherit receiver
  arguments and infer or explicitly receive their own.
- Bodies are checked structurally at declaration and rechecked after substitution. Same-argument
  recursive specialization is allowed; argument-changing recursive cycles are rejected.

### Structural unions

- A union holds exactly one active member; injection is implicit and allocation-free. Unions are
  flattened, duplicate-free, structural, and order-independent. Written order only chooses among
  contextual initializer candidates.
- A union contains at least two distinct canonical members. A written union must name each canonical
  member exactly once: a member repeated after alias resolution and generic substitution is an error
  naming the later member, so `Int32 | Int32` and `A | Int32` where `type A = Int32` are both
  invalid, and a written union is never an alias for a surviving member. Distinct members are then
  flattened, canonically ordered, and interned as one structural identity. Nil is valid only as one
  member of a union satisfying this rule.
- Widening is allowed only when every source member fits the destination; implicit narrowing and
  declaration-time union inference do not exist.
- `is` tests an exact active member. Narrowing applies to direct local reads; assignment or writable
  address escape invalidates it.
- `is Nil` is invalid, and `T | Nil` also rejects `is T`; use `== nil`/`!= nil`. Larger nullable
  unions may test non-Nil members, and match type patterns may name Nil.
- A pointer type is exactly Ptr, MutPtr, or Fun after transparent-alias resolution. A union of
  exactly Nil and one pointer type uses the null-pointer niche without a tag. All other unions,
  including handle-plus-Nil unions, use the general tag-plus-inline-payload representation. Member
  operations require narrowing.
- Union equality requires identical canonical union types and equality-capable members; ordering is
  unavailable. Members may be any storable value. Atomic and Unknown cannot be members.

### Algebraic data types and match

- An ADT is a nominal closed sum with at least two distinct qualified variants. Unit variants are
  values; record variants require exhaustive named payload initialization. Payload fields are fixed.
- Direct by-value recursion is invalid; pointer-indirect recursion and generic
  specialization are valid.
- `match` is an expression and evaluates its scrutinee once. Value mode matches `true`/`false`.
  Type mode (`match value is`) matches exact complete types, individual union members, Nil, or ADT
  variants; a union type itself is not one pattern.
- Arms are `| pattern then expression`; optional final `else` is catch-all. Match is exhaustive;
  duplicates and patterns unable to match remaining values are errors. Arms run in source order.
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
  Surrounding result context does not change that choice. Rune never widens implicitly.

### Explicit conversion

- `value.to<T>()` is the only explicit scalar conversion; T is mandatory and the call has no value
  arguments. Identity conversions are no-ops and Byte canonicalizes to UInt8.
- Constants outside the destination domain fail compilation. Dynamic invalid values trap before an
  unsafe C conversion.
- Integer conversion preserves the mathematical value. Integer/float and float/float round nearest,
  ties-to-even; finite overflow traps. Float/integer truncates toward zero then checks range; NaN and
  infinities are invalid. Rune conversions also check Unicode scalar validity.
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
- Bitwise operations accept fixed integers, excluding Size (whose width follows the target), Rune,
  Bool, pointers, aggregates, and managed values. Shift counts must be `0..width-1`; bad constants
  fail and bad dynamic counts trap. Signed right shift is arithmetic, unsigned zero-filling.
- Rune supports equality, ordering, and checked `to<T>()` conversion. Rune is invalid for `+`, `-`,
  `*`, `/`, `%`, unary `-`, `~`, `&`, `^`, `|`, `<<`, and `>>`.
- `bit_cast<T>()` supports equal-width fixed integers and Float32/64, excluding pointers, Size, Rune,
  and aggregates. Fixed integers provide `to_le_bytes()`/`to_be_bytes()` and
  `T.from_le_bytes(array)`/`T.from_be_bytes(array)` through exact `Array<Byte, N>`.

### Equality, ordering, and truthiness

- Numeric comparison uses the lossless common type. Other comparisons require identical canonical
  types. Bool, Rune, EoS compare by value; pointers by identity; text by UTF-8 bytes; objects by
  members; ADTs by tag/payload; unions by member; Array/View/List by length then elements.
- `== nil` and `!= nil` test whether a union's active member is Nil. They require a union containing
  Nil, are the only Nil comparison, and read no payload. Nil has no standalone value to compare.
- String and Strand are not mutually comparable. Functions, allocators, and Dicts have no
  equality. An aggregate is comparable only when all recursively compared components are.
- Ordering exists only for numeric scalars, Rune, String, and Strand. Text uses unsigned-byte
  lexicographic order with shorter prefix first.
- Only `false` and `nil` are falsey. Truthiness applies to conditions and `!`, `and`, `or`; it is not
  Bool conversion or union narrowing. Logical operators return Bool and short-circuit left-to-right,
  while both operands must still be valid expressions.

## Control flow and cleanup

- Every structured body opens with an explicit delimiter (RFC 0061): function, method, `while`, and
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
| Array, View, List, String, Strand | 1 | value |
| Array, View, List, String, Strand | 2 | `index: Size`, value |
| Dict | 2 | key, value |
| Dict | 3 | `index: Size`, key, value |

Every other source/arity combination is invalid.

- Text iterates decoded Runes; Dict order is unspecified.
- Finite-source traversal boundaries are captured once. Array places iterate in place; temporary
  Arrays and Strands materialize once; handles copy shallowly.
- Binders are fresh immutable copies each iteration and names in one header are distinct. Nullable or
  union sources must first narrow to one iterable type.
- Array and View traversal has a fixed boundary. Element replacement is valid; there is no structural resize operation.
- List traversal captures the source's structural version. `push`, `pop`, `clear`, `free`, and any operation that changes storage or length invalidate the traversal. A `push` that would extend the traversal traps with `collection modified during iteration` rather than extending or terminating.
- Dict traversal captures the source's structural version. `insert`, replacement, `remove`, `free`, and any bucket/topology change invalidate the traversal.
- Mutation through any alias observes and updates the same version because copied handles refer to the same collection state.
- A traversal checks its version immediately before each iteration body with `if (version != captured) hex_runtime_trap("[Runtime Error] collection modified during iteration\\n")`. No check is required at the loop increment; the next body's check covers the transition. When the checker proves that no operation in the traversing scope or any reachable call can mutate the source (proven-safe elision), the check may be omitted.
- The version is a monotonic `Size` (`size_t`) counter incremented on every structural change; it wraps modulo `2^N` and a wrapped version that coincides with a live traversal's captured token is an accepted false negative.
- Freeing the traversed List or Dict, or an alias that refers to it, is always rejected while the traversal is active. Passing the traversed collection or an alias to an unproven call is rejected; the checker must not rely on a post-call version check after a possible free.
- A mutation after `break` or after the traversal's scope exits is valid when no separate lifetime rule rejects it. Nested traversals capture independent versions. Array/List element replacement remains valid.

## Errors

```text
Error.new(header: Strand, message: String) -> Error
```

- Protected nominal `Error` has fixed immutable fields `file: String`, `line: Size`, `column: Size`,
  `header: Strand`, `message: String`.
- `Error.new(header, message)` is the only constructor and injects the current module's logical
  source key plus one-based line and UTF-8 byte column. Propagation preserves the location.
- Fallible functions return structural unions containing Error; there are no exceptions or hidden
  result channels. Error copying is shallow. Runtime `message` String storage must remain live while
  any alias can be inspected or printed.
- A try expression or try statement requires exactly one Error member and at least one success
  member. It evaluates once and returns Error unchanged. A try expression yields the normalized
  success value/union; a try statement discards it. Neither catches traps.
- `try` and `errdefer` are valid only inside a function whose declared result accepts Error; both are
  invalid at root scope. `try` is additionally invalid inside any cleanup action.

## Allocation and lifetime

```text
Heap.new() -> Heap
Heap.allocate<T>(initial: T) -> MutPtr<T>
Heap.free<T>(pointer: Ptr<T>) -> no value
Heap.free<T>(pointer: MutPtr<T>) -> no value
```

- `Heap.new()` selects the default allocator without runtime allocation; Heap operations are
  thread-safe.
- `h.allocate<T>(initial)` allocates and initializes one complete finite T, returning non-owning
  `MutPtr<T>`. T must be valid in HeapAllocation; direct Atomic allocation is invalid. Failure or
  unrepresentable size traps.
- `h.free(ptr)` accepts Ptr/MutPtr and requires the matching allocator.
- Heap-backed library values receive their Heap explicitly; allocation and cleanup never choose a
  hidden allocator.
- Freeing a container releases only its own header/backing region. It never frees allocations its
  elements or nested handles refer to. Referenced owned allocations require cleanup before loss of
  reachability, exactly once per distinct allocation rather than per alias or slot.
- The shallow rule applies at every depth. Replacing/dropping the last handle may leak; freeing one
  alias dangles all others. Runtime metadata may catch live mismatch or double-free, but later
  lifetime misuse is not guaranteed to be detected.
- Cleanup misuse is rejected at compile time wherever a local analysis decides it. Three are
  rejected: freeing a pointer traceable to `ref`, freeing a local binding already freed on every
  path to that point, and reading through one. Misuse requiring interprocedural, alias, or escape
  analysis is never rejected — a pointer arriving as a parameter, read from a member or collection,
  or copied to a second binding is not tracked, and leaks are not diagnosed. An undecided case is
  always accepted.

## Collections

### Common rules

- Signature metavariable Integer means any Hexal integer type. `place<T>` and
  `read-only-place<T>` describe writable and read-only expression results; they are not source types.
- Lengths, capacities, indices, and normalized bounds use Size. Index arguments may be any integer
  and are normalized with compile-time rejection or dynamic traps.
- Ranges are zero-based and end-exclusive. `length`, indexing, and `slice` use the
  same bounds where available. `at` and every `is_empty` method were removed
  (RFC 0063 for Array/View/List, RFC 0087 for String/Strand once a cached rune
  count made String `length()` O(1)): `receiver[index]` and
  `receiver.length() == 0` are their identical replacements. No collection or
  text type has `is_empty`.
- Array/View/List equality compares length then elements. No collection ordering; no Dict equality.

### `Array<T, N>`

```text
Array<T,N>.length() -> Size
Array<T,N>[index: Integer] -> place<T>
Array<T,N>.slice(start: Integer, end: Integer) -> View<T>
```

- Fixed inline sequence; N is a positive integer literal. A contextual `[a, ...]` must contain
  exactly N elements, evaluated left-to-right.
- Assignment, arguments, and returns copy the inline region. Element writes require a writable Array
  place. Indexing is checked; slice returns View.
- T follows general storability, including nested Arrays. Arrays free nothing; external-state
  elements copy only their references.

### `View<T>`

```text
View<T>.from_pointer(pointer: Ptr<T> | MutPtr<T>, length: Size) -> View<T>
View<T>.empty() -> View<T>
View<T>.length() -> Size
View<T>[index: Integer] -> read-only-place<T>
View<T>.slice(start: Integer, end: Integer) -> View<T>
```

- Non-owning read-only contiguous pointer-length descriptor. T follows general storability; MutPtr
  elements retain pointee capability.
- `from_pointer` accepts statically non-null Ptr/MutPtr, evaluates pointer then length once, weakens
  MutPtr, and performs no allocation, copy, mutation, or pointer arithmetic.
- `from_pointer` requires contiguous initialized aligned storage with sufficient lifetime. It rejects
  pointers locally traceable to `ref` and accepts heap or opaque parameter pointers. Interprocedural
  provenance from a caller argument is not checked.
- Views have no storage-position exception. Separately, they cannot root in temporary Array/List
  storage or be addressed with `ref`; these are provenance/address rules, not placement rules.
  Root-level View bindings are locals. Bounds checks remain active after construction.
- A View may return when rooted in a parameter, parameter-reached storage, `from_pointer` region, or
  empty View. A directly returned local-rooted View is rejected. Direct View return analysis does not
  inspect Views nested in returned objects, ADTs, unions, or collections.
- Resize invalidation and `from_pointer` region lifetime are not tracked. View validity requires a
  valid source.

### `List<T>`

```text
List<T>.new(heap: Heap) -> List<T>
List<T>.length() -> Size
List<T>[index: Integer] -> place<T>
List<T>.slice(start: Integer, end: Integer) -> View<T>
List<T>.push(value: T) -> no value
List<T>.pop() -> T
List<T>.clear() -> no value
List<T>.free(heap: Heap) -> no value
```

- Growable allocated sequence. A fixed handle can mutate its List; `mut` only reassigns the handle.
  `pop` traps when empty; indexed access is bounds-checked.
- T follows general storability. Every operation copies/discards T shallowly, including String.
  Index assignment, `clear`, and `free` drop slots without freeing referents; free releases only List storage.
- Values read or popped are aliases. Each distinct referenced owned allocation requires exactly one
  cleanup before loss of reachability. Repeated aliases must not be freed per slot. Reverse defer
  order runs later-registered element cleanup before earlier-registered container cleanup.

### `Dict<K, V>`

```text
Dict<K,V>.new(heap: Heap) -> Dict<K,V>
Dict<K,V>.insert(key: K, value: V) -> no value
Dict<K,V>.get(key: K) -> V
Dict<K,V>.find(key: K) -> V | Nil
Dict<K,V>.contains(key: K) -> Bool
Dict<K,V>.remove(key: K) -> V
Dict<K,V>.length() -> Size
Dict<K,V>.free(heap: Heap) -> no value
```

- Open-addressing allocated dictionary. K is exactly Int32 or Strand; V follows List eligibility.
  Missing get/remove trap; find returns Nil for a missing key; insert replaces.
- Keys and values copy shallowly. Reads/removal return aliases; replacement/free drop entries without
  freeing referents. Free releases only buckets/header. Overwriting the final reachable handle leaks
  its referent.
- Hashing is internal and infallible for supported keys. Equal values hash equally; Strand hashes
  logical payload excluding terminator/zero tail. Algorithm, seed, and iteration order are unstable
  and unspecified; no source hash operation exists.

## Text

```text
String.length() -> Size
String.bytes() -> View<Byte>
String.slice(start: Integer, end: Integer) -> View<Byte>
String.rune_cursor() -> RuneCursor
String.to_string(heap: Heap) -> String
String.concat(heap: Heap, other: String) -> String
String.free(heap: Heap) -> no value
String.from_bytes(heap: Heap, bytes: View<Byte>) -> String
String.from_runes(heap: Heap, runes: View<Rune>) -> String
Strand.length() -> Size
Strand.to_string(heap: Heap) -> String
RuneCursor.has_next() -> Bool
RuneCursor.next() -> Rune
```

- Byte is UInt8. A byte literal contains exactly one printable ASCII byte or one of
  `\\ \' \n \r \t \0 \xHH`.
- A Rune literal contains one Unicode scalar and also supports `\"` and `\u{HEX}`; it is not a
  grapheme cluster.
- String is immutable UTF-8 behind a non-null pointer-sized handle. Runtime values use one
  header-plus-bytes allocation; literals use static storage. Strand is immutable literal-only inline
  32 bytes: at most 31 UTF-8 bytes, NUL, then zero fill; embedded NUL/invalid UTF-8/overflow reject.
- String and Strand `length()` counts Runes; byte Views count bytes. String stores the count in its
  heap header, set at every construction, so `length()` is a field read and `slice` validates its
  bounds without scanning. Strand has no room for a count and scans, bounded by its 31 payload bytes.
  Neither is indexable: reaching the
  nth Rune of UTF-8 walks from the start, so a positional loop would be quadratic behind O(1)
  syntax. `rune_cursor` walks Runes in one pass and `bytes` gives indexed byte access.
- String slice uses Rune bounds and returns the corresponding zero-copy UTF-8 bytes. `from_bytes`
  validates before allocation and traps on malformed UTF-8.
- RuneCursor borrows String; `next` traps after exhaustion. Copies hold independent positions over
  the same storage.
- Runtime String allocations require one matching free; all aliases then dangle. Literals must never
  be freed. Collection reads produce aliases without ownership transfer or lifetime protection.
- String and Strand dispatch separately; Strand exposes no View into inline bytes.

## Output

### `print`

```text
print(first: Printable, rest: Printable...) -> no value
```

- `print(arg, ...)` is protected, requires at least one argument, inserts no separator/newline, and
  returns no value. Arguments evaluate once left-to-right; output starts only after all evaluation.
- Directly printable: Bool, fixed-width integers, Size, Byte, Float32, Float64, Rune, String, Strand,
  Nil, and Error. Objects, ADTs, Array, View, List, and Dict are printable exactly when every
  recursively visited component is printable. Every other canonical type is non-printable; unions
  must narrow to a printable member first. Failure identifies the first non-printable member path in
  declaration order.
- A print argument is the one position that admits standalone Nil, so a union narrowed to Nil and the
  bare `nil` literal are both printable. Nil prints `nil` directly and nested.
- Direct text/Rune is raw; nested text/Rune is quoted/escaped; Byte is numeric. Structural forms are
  fixed, one line, and exactly:

```text
object:             <Type> { <member> = <value>, ... }
unit ADT variant:   <ADT>.<Variant>
record ADT variant: <ADT>.<Variant> { <field> = <value>, ... }
Array/View/List:    [<value>, ...]
Dict:               {<key>: <value>, ...}
```

Object members use declaration order and ` = `. Record variants print only the active payload.
Array/View/List use `[]` when empty. Dict uses `:`, `{}` when empty, and unspecified entry order.

- Float32/64 use `%g` precision 9/17; signed zero and `inf`, `-inf`, `nan` are preserved. A direct
  Error prints `file:line:column: header: message` with no trailing newline; nested, it uses the
  object form with declaration-ordered fields and quoted text.
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

- Spawn evaluates arguments once left-to-right and shallow-copies them; failure starts no task. R
  must be valid in FunctionResult and TaskResult, complete, finite, and copyable. Spawn Error is
  separate from returned R.
- `join()` waits, copies the exact result, and reclaims storage. `detach()` discards result and
  arranges reclamation. Exactly one successful join or detach is allowed across aliases.
- Scheduler-owned stacks/control/queues need no allocator. `Task.yield()` is the explicit scheduling
  point in one cooperative M:N scheduler over C23 worker threads.
- Targets are Windows x64 and POSIX x86-64 with verified C23 `<threads.h>`; otherwise Task features
  produce Unsupported Error. Root is pinned to worker zero; root return does not join tasks. Stacks
  reserve 1 MiB by default with an 8 KiB initial commit, both `Project` build-time settings; the
  initial commit is a Windows-only knob, and the usable region is the reserve less one guard page.
  Exceeding the reserve traps with `[Runtime Error] task stack overflow` rather than corrupting
  memory.
- Every repeating path through task-reachable literal `while true` visibly executes `Task.yield()` or
  compilation fails.
- Spawn, join, Mutex, Channel, and sequentially consistent Atomic operations provide their specified
  C23 synchronization edges. Unsynchronized conflicting access is a data race with no guarantee.

### `Channel<T>`

```text
Channel<T>.new(heap: Heap, capacity: Size) -> Channel<T> | Error
Channel<T>.send(value: T) -> Nil | Error
Channel<T>.receive() -> T | EoS
Channel<T>.close() -> no value
Channel<T>.free(heap: Heap) -> no value
Channel<T>.length() -> Size
Channel<T>.capacity() -> Size
Channel<T>.is_closed() -> Bool
```

- Bounded MPMC FIFO; capacity zero fails at compile time when known, otherwise with Error. Full send
  and empty receive park Task, not worker.
- T must be valid in ChannelElement, complete, finite, and copyable, which excludes top-level EoS and
  any value transitively containing Atomic. Elements copy shallowly. Error is a valid T.
- Send after close returns Error. Close is idempotent, preserves queued values, and wakes waiters;
  closed/drained receive returns eos. Receive adds no Error result member.
- Free requires closed, empty, unused state and releases only Channel storage.

### `Mutex`

```text
Mutex.new(heap: Heap) -> Mutex | Error
Mutex.lock() -> no value
Mutex.unlock() -> no value
Mutex.free(heap: Heap) -> no value
```

- Allocated scheduler-aware non-recursive lock owned by Task identity. Waiting parks Task. Recursive
  lock, wrong-owner/double unlock, or freeing locked/waited Mutex is programmer error. Invalid states
  detectable from a live control block trap, including recursive lock and wrong-owner unlock. Freed
  control blocks need not be retained to diagnose stale aliases; use after free is not guaranteed to
  trap.

### `Atomic<T>`

```text
Atomic<T>.new(initial: T) -> Atomic<T>
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
- `Atomic<T>.new(value)` directly initializes fresh binding or object-member storage; these are its
  only placements. Nested object construction initializes each member in place. The resulting object
  is non-copyable but may be shared through Ptr/MutPtr. `ref` of Atomic or an Atomic member is
  independently invalid. Pointers to enclosing Atomic-containing objects remain valid.

## Byte streams

```text
IO.stdin()  -> IO | Error
IO.stdout() -> IO | Error
IO.stderr() -> IO | Error
Bytes.over(buffer: List<Byte>) -> Bytes

IO.read(into: List<Byte>, max: Size)            -> Size | EoS | Error
IO.write(from: View<Byte>)                       -> Size | Error
IO.seek(to: Seek)                                -> Size | Error
IO.close()                                       -> Nil | Error
MutPtr<Bytes>.read(into: List<Byte>, max: Size)  -> Size | EoS | Error
MutPtr<Bytes>.write(from: View<Byte>)             -> Size | Error
MutPtr<Bytes>.seek(to: Seek)                      -> Size | Error

type Seek = | Start(Size) | Current(Int64) | End(Int64)
```

- `IO`, `Bytes`, and `Seek` are reserved protected type names; redeclaration is a Type Error.
  `Start`, `Current`, and `End` remain available as unqualified names. Seek variants construct as
  qualified record variants with payload fields `position` (Start) and `offset` (Current, End).
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
  clamped per target (`SSIZE_MAX` POSIX, `UINT32_MAX` Windows); `max == 0` and an empty View return
  `Size(0)` touching nothing. POSIX `EINTR` before transfer returns Error and is never retried by
  the primitive.
- Read appends at most `max` bytes to the destination list, preserving prior contents; destination
  capacity grows once through the internal List reserve helper before one platform call.
- Bytes write overwrites at the cursor and extends the list past its end; Bytes seek resolves within
  `[0, buffer.length]` only — sparse holes do not exist. Self-read (destination identity equal to
  the backing list) and writes from a View overlapping the backing allocation return Error before
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
  compound-write atomicity is promised. Descriptor operations may block their OS worker.
- Failures carry a bounded ASCII Strand header `IO <operation> errno=<code>` (POSIX) or
  `winerr=<code>` (Windows), zero-filled with one NUL, plus a static message such as `read failed`
  or `stream is not writable`; no Heap is required on a failure path. Native codes are diagnostic
  data; portable source must not depend on their values.
- `print` shares the descriptor write-all backend of stdout: one buffering domain, short-write and
  EINTR retries inside print's private sink, trap only when a complete print cannot finish.
- Generated C confines all platform branches to `hexal/io.c`; no signature contains `#ifdef`,
  `FILE *`, or a platform type. Selecting IO, Bytes, or print selects the pair plus the
  `List<UInt8>` specialization once; programs using none emit no IO artifact.

## Layout intrinsics

```text
size_of<T>() -> Size
align_of<T>() -> Size
```

- `size_of<T>()` and `align_of<T>()` require one explicit complete finite type and return Size C
  constant expressions. Reference-like types report source handle size. These operations do not make
  arbitrary Array lengths valid.

## Volatile operations

```text
Ptr<T>.read_volatile() -> T
MutPtr<T>.read_volatile() -> T
MutPtr<T>.write_volatile(value: T) -> no value
```

- `read_volatile()` exists on Ptr/MutPtr; `write_volatile(value)` requires MutPtr. T is a fixed-width
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
  (target-sized `Size` literals) are emitted (RFC 0062).
- `<stdckdint.h>` is selected demand-first through `hexal.h` when checked runtime arithmetic or a
  selected signed wrapping specialization uses `ckd_add`/`ckd_sub`/`ckd_mul`; it is never emitted
  for a program with no selected checked arithmetic, and no private fallback definition is emitted
  (RFC 0069). The qualified GCC/Clang plus compatible-C-library target provides the header, and
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
  null pointer is never passed with a zero count (RFC 0069 Amendment 2).
- Every generated diagnostic trap reports through one program-wide `hex_runtime_trap` (declared in
  `hexal.h`, defined once in the root module's C file, `[[noreturn]]`, owning `<stdio.h>`/`<stdlib.h>`
  selection). No per-family trap function exists. The one exception is the Task stack-overflow trap,
  which a signal handler (POSIX) or vectored exception handler (Windows) emits directly because the
  faulted stack cannot run `hex_runtime_trap`; it keeps the same `[Runtime Error]` message shape. An
  impossible compiler-internal union tag guard may retain a direct `abort()` (RFC 0069 Amendment 2).
- Nil renders the C23 `nullptr` keyword, no generated C spells `NULL`, and `nullptr_t` never appears
  as a type spelling. Nil alone
  selects no standard header; `<stddef.h>` is selected only by a real declaration consumer such as
  `size_t` (RFC 0069 Amendment 2).
- Objects/ADTs lower to source-ordered structs; unions to checked tagged values except pointer-null
  niches; generics are monomorphized. Object forward typedefs precede source-ordered definitions.
- `Fun<...>` uses its ordinary complete C function-pointer declarator in every position - binding,
  parameter, field, collection element - except a function's own return type, which cannot nest
  that declarator inside its own. There, and in any other position needing a standalone type
  specifier, the recursive spelling uses C23 `typeof` around the same declarator
  (`typeof(int32_t (*)(int32_t))`); no function-pointer typedef family is generated. A fixed binding
  reached through `typeof` qualifies the pointer value (`typeof(...) const name`); a mutable one
  omits that qualifier. Named module functions, local named functions, and anonymous literals all
  lower to ordinary C functions with no closure, environment, or dispatcher. A local named function
  or literal is file-scope `static` and named `hex_fun_<ordinal>`, sharing one module-local ordinal
  stream in checked-tree preorder; its source name is checker metadata, never a C symbol. Every
  helper's prototype is emitted before any definition can reference it.
- Pointer qualification follows type layers only: Ptr adds pointee `const`, MutPtr does not, and a
  fixed binding adds trailing `const`. Object members themselves are unqualified. No
  qualifier-discarding cast is emitted.

| Hexal | C23 |
| --- | --- |
| `Ptr<Int32>` | `const int32_t *` |
| `MutPtr<Int32>` | `int32_t *` |
| `Ptr<Ptr<Int32>>` | `const int32_t *const *` |
| `MutPtr<Ptr<Int32>>` | `const int32_t **` |
| `Ptr<MutPtr<Int32>>` | `int32_t *const *` |
| `MutPtr<MutPtr<Int32>>` | `int32_t **` |
| `Ptr<Unknown>` / `MutPtr<Unknown>` | `const void *` / `void *` |

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
- The result's `Files` map is the sole generated-artifact surface: `CompilationResult` has no
  `MainC`/`MainH` or other mirrored root-file fields, and `Files` is non-nil on every result.
- `CompilationResult.Stats` is one project-level summary per compilation call. It aggregates only
  the entrypoint and reachable modules, exposes no per-module statistics, and on failure reports
  work completed before failure.
- A successful compilation produces exactly `hexal.h`, one C/header pair per reachable module
  under `modules/<canonical-path>.c/.h`, and the demand-driven component artifacts under
  `hexal/` that the reachable program selects; it returns `ExitSuccess` and has empty `Stderr`. A
  failed compilation produces no artifacts: `Files` is empty, `ExitCode` is `ExitFailure`, and
  `Stderr` carries the structured diagnostics. No failure C program is emitted.
- `hexal.h` is the mandatory small program-support header, generated from the program-wide
  aggregate of all reachable modules. It opens with the demand-driven umbrella of portable standard
  headers (deterministic lexical order, only for families the reachable generated program selects;
  `<stdbool.h>`, `<limits.h>`, and `<float.h>` are never emitted), followed by the retained
  source-dependent `Size`-literal `SIZE_MAX` assertions, the shared `hex_eos` typedef exactly when
  generated C represents EoS, and the declaration of the one program-wide `hex_runtime_trap` when a
  selected path can trap. It contains no Heap, View, String, Strand, Error, List, Dict, Array, Task,
  Channel, Mutex, or Atomic representation or helper, no String literal storage, no process-wide
  runtime state, no generic integer, byte-width, or float target probe, and no user-declared
  module-type definition or exported/cross-module user prototype. Its guard is `HEXAL_H`; every
  module header includes it, and it includes no other compiler-owned header.
- The component artifacts under `hexal/` own the generated runtime support, one family per file,
  emitted only when that family is reachable: `hexal/runtime.c` (the `hex_runtime_trap`
  definition), `hexal/wrap.h`, `hexal/heap.h`/`hexal/heap.c`, `hexal/view.h`, `hexal/string.h`/
  `hexal/string.c`, `hexal/error.h`, `hexal/list.h`, `hexal/dict.h`, `hexal/array.h`,
  `hexal/numeric.h`, `hexal/print.h`/`hexal/print.c`, `hexal/equality.h`, and
  `hexal/concurrency.h`/`hexal/concurrency.c`. Their source of truth is the compiler's embedded C/
  header templates; a `.c` artifact is emitted only when it contains at least one definition.
  Component headers have stable `HEXAL_<COMPONENT>_H` guards, include `hexal.h` first and then only
  their declared dependencies (heap, view, string, error, list, dict, array, numeric, print,
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
- String comparison demand is independent: `hex_equal_hex_string` is emitted in the String
  component only for a String equality expression or a reachable recursive equality helper that
  compares a String member; `hex_compare_hex_string` is emitted only for String ordering.
  Strand equality and ordering use direct fixed-width byte comparison and select neither helper.
- `hexal/equality.h` owns one helper per canonical program-owned equality aggregate: builtin-element
  Array, View, and List specializations and the compiler-owned Error object, including recursively
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
- `modules/<canonical>.c` is one module's translation unit: it includes only its own module
  header, and defines its private functions and methods with internal `static` linkage, its exported
  functions and methods and spawned functions with external linkage, its monomorphized
  specializations, and its spawn entry adapters (external linkage, declared in the concurrency
  component). The selected root module's C file owns `int main(void)`, which executes the root
  module's executable statements and returns `0`, C's successful termination status (RFC 0062);
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
  appends `_0`, `_1`, and so on. The same union written in any module spells one C type (RFC 0095).
- General tagged unions and concrete ADTs share one program-wide discriminant enum `hex_tag`
  (RFC 0099), emitted once in `hexal.h` before every module-header use and omitted when no
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
- Collection C names derive from the element's display name (`hex_list_`, `hex_dict_`, `hex_view_`,
  `hex_array_`, `hex_task_`, `hex_channel_`, `hex_atomic_`). When same-named elements from distinct
  modules would derive one C name, the later interned specialization appends `_` plus the encoded
  owner of its element's defining module; resolution happens once at interning, so a typedef and
  every helper suffix derived from its name stay paired.
- The artifact set contains no top-level `main.c`, `main.h`, or compatibility header; the
  entrypoint's canonical module C file supplies `main()`.
- Invalid or unsupported source produces a structured diagnostic and is never silently omitted or
  partially generated. Syntax failures, static-semantic failures (Name and Type Errors), Module
  Errors, dynamic traps, and Unknown Error are distinct externally visible classes. Unknown Error
  identifies an unclassifiable compiler inconsistency, not a source-program error.

## Excluded features

- FFI: C imports/exports and foreign ABI remain draft and are not part of this language; native
  modules are implemented.
- Memory: Arena, Pool, source pointer arithmetic/casts, `unsafe`, mutable View.
- Control/iteration: ranges, counted loops, user iterators, mutable iteration binders, exceptions.
- Functions/concurrency: closures, async/await, coroutines, user threads, task groups, `select`,
  unbounded/rendezvous Channels, nonblocking Channel operations, memory-order arguments.
- Extensibility: operator overloading, user truth/display/hash protocols, generic constraints,
  reflection, serialization schemas, runtime type objects.
- Expressions: compound assignment, increment/decrement, conditional operator, numeric suffixes,
  wrapping/saturating conversion or arithmetic modes.
- I/O: all File APIs (the built-in `File`/`FileMode`/`Stdio` were removed by RFC 0064; a library API
  returns later through C interop), Path, sockets, asynchronous I/O, builders, in-memory
  output streams.
