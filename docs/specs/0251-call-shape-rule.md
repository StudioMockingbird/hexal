# RFC 0251: One Call-Shape Rule for Methods, Type Functions, and Module Functions

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started. Decided: value
  types carry instance methods, including nominal unions (A+); std/regex and
  std/json move to methods; conversions move onto the source value through
  `.to<T>()` or source methods (E); methods are never called on literals.
  The decisions are recorded below.
- Sequencing: Phase 1 (union methods) lands after RFC 0254 (reference `self`,
  explicit `method mut`) and extends that receiver contract to unions.

## Summary

Hexal has three call shapes plus bare builtins, and today which one an
operation gets is decided case by case:

```hexal
list.push(x)                        -- instance method
String.from_bytes(h, bytes)         -- type-level function (compiler-owned types only)
Fs.open(path, mode)                 -- module function
print(x)                            -- builtin function
```

This RFC fixes one guideline for placing an operation, and aligns the
language and std with it:

- every value type, including a nominal union, can carry instance methods;
- std/regex's and std/json's operations on an existing value become methods;
- conversions on compiler-owned types move onto the source value: to another
  type through `.to<T>()`, otherwise as a source method, leaving type-level
  functions only where no value exists;
- a method is never called on a literal: `"carol".copy(h)` binds the literal
  first.

## The guideline

Every operation `f` has exactly one owner: a module (user or std), or a
compiler-owned type. Each compiler-owned type is its own owner, so `String`
and `Heap` are different owners.

1. **Receiver.** An operation that acts on an existing value (its *primary
   operand*) is an instance method on that value's type: `value.f(rest...)`.
   An allocator argument follows the receiver: `list.free(h)`,
   `pattern.test(h, subject)`. A conversion acts on its source value, and a
   conversion to a type is spelled `.to<T>()`: `bytes.to<String>(h)`.
2. **Owner call.** An operation with no value to act on is called
   `Owner.f(args...)`: constructors from outside input and value-less
   operations. `Owner` is the import alias for a module, and the type name for
   a compiler-owned type.
3. **Construction** by fields stays `Type(...)` / `Owner.Type(...)`, and
   compiler-owned canonical constructors stay `Type(...)` (`Heap()`,
   `List<T>(h)`).
4. **Builtins** (`print`, `size_of<T>()`, `align_of<T>()`) are bare
   compiler-owned functions over any type, with no owner to name.

User types never get type-level functions: a user module's owner is its
alias, so `Geo.origin()` is the one spelling (RFC 0186 stays in force).

The guideline is API-design policy for std and compiler-owned operations; the
checker enforces only the receiver rules below.

Allocator direction follows rule 1 and is not an exception:

- `heap.free(ptr)`: `Ptr<T>` does not record which allocator made it, so the
  allocator is the value acted on.
- `list.free(heap)`, `s.free(heap)`: the collection owns its storage; the heap
  is where it goes back to.

One recorded exception: std/io's stream operations are methods on
`Ptr<mut Bytes>` (`read`, `write`, `seek`). `Ptr` is compiler-owned and
`Bytes` belongs to std/io; the pointer receiver shares a stream without
copying.

## Inventory (from `docs/reference.md`)

### Instance methods

| Owner | Methods |
| --- | --- |
| `Heap` | `allocate`, `allocate_aligned`, `free` |
| `Stash` | `allocate`, `reset`, `destroy` |
| `Pool<T>` | `allocate`, `free`, `destroy` |
| `Ptr<T>` | `offset`, `cast`, `read_volatile`, `write_volatile` |
| std/io `Ptr<mut Bytes>` (exception) | `read`, `write`, `seek` |
| `Slice<T>` | `length`, `slice`, `to_list` |
| inline `List<T, N>`, `List<T>` | `length`, `push`, `pop`, `clear`, `slice`, `mut_slice`, `map`, `filter`; `List<T>` also `free` |
| `Dict<K,V>` | `insert`, `get`, `find`, `contains`, `remove`, `length`, `clear`, `keys`, `values`, `entries`, `to_list`, `free` |
| `String`, `String<N>` | `length`, `rune_length`, `grapheme_length`, `bytes`, `byte_cursor`, `rune_cursor`, `grapheme_cursor`, `slice`, `copy`, `concat`, `casefold`, `normalize`, `widen`, `c_pointer`, `free`, per form as the reference lists |
| byte, rune, and grapheme cursors | `has_next`, `next`, `peek`, `offset` |
| `Grapheme` | `bytes`, `rune_length` |
| `Rune` | `value`, `utf8_length`, `category`, `is_lower`, `is_upper`, `is_alphabetic`, `is_numeric`, `is_whitespace`, `to_lower`, `to_upper`, `to_title`, `display_width`, `combining_class` |
| `Channel<T>` | `send`, `receive`, `close`, `length`, `capacity`, `is_closed`, `free` |
| `Mutex` | `lock`, `unlock`, `free` |
| `Atomic<T>` | `load`, `store`, `exchange`, `fetch_add`, `fetch_sub`, `compare_exchange` |
| `Task` | `join`, `detach` |
| `Error`, `ErrorKind` | `header` |
| std/io `IO` | `read`, `write`, `seek`, `close` |
| std/fs `File` | `read`, `write`, `seek`, `flush`, `close` |
| std/time `Duration` | `as_nanoseconds`, `as_microseconds`, `as_milliseconds`, `as_seconds` |
| std/time `Instant` | `elapsed`, `duration_since` |
| std/time `WallTime` | `seconds`, `nanosecond` |
| std/net `Address` | `format` |
| std/net `TcpConnection` | `read`, `write`, `shutdown`, `no_delay`, `close` |
| std/net `TcpListener` | `accept`, `close` |
| std/process `Process`, `Pipe` | `wait`, `terminate`, `close`; `read`, `write`, `shutdown`, `close` |
| std/signal `Signals` | `next`, `close` |
| user struct | any `method T.name(...)` |

### Type-level functions on compiler-owned types

| Call | Under this RFC |
| --- | --- |
| `String.from_bytes(h, bytes)`, `String<N>.from_bytes(bytes)` | becomes `bytes.to<String>(h)` / `bytes.to<String<N>>()` (E) |
| `String.from_runes(h, runes)` | becomes `runes.to<String>(h)` (E) |
| `String.interpolate(h, template)`, `String<N>.interpolate(template)` | stays: the template is compile-time syntax, not a value |
| `Slice<T>.from_pointer(ptr, n)`, `Slice<mut T>.from_pointer(ptr, n)` | becomes `ptr.to_slice(n)` (E) |
| `Rune.from(value)` | becomes `value.to<Rune>()` (E2) |
| `T.from_le_bytes(b)`, `T.from_be_bytes(b)` | becomes `b.decode_le<T>()` / `b.decode_be<T>()` (E3) |
| `Slice<T>.empty()` | stays: no value exists |
| `Task.yield()` | stays: value-less |

### Module functions

| Module | Functions | Why not a method |
| --- | --- | --- |
| std/io | `stdin()`, `stdout()`, `stderr()`, `bytes_over(buffer)` | value-less / constructs |
| std/fs | `open(path, mode)` | constructs from a path |
| std/time | `nanoseconds`, `microseconds`, `milliseconds`, `seconds`, `now()`, `wall_time()`, `sleep(d)` | construct / value-less |
| std/net | `parse_address`, `resolve`, `connect`, `listen` | construct from text or Address |
| std/process | `start(options)` | constructs |
| std/signal | `subscribe(subscriptions)` | constructs |
| std/terminal | `is_attached(stream)`, `size(stream)` | operand `IO` is owned by std/io |
| std/ascii | `is_digit`, `is_alpha`, `is_space`, `to_lower`, `to_upper` | operand `Byte` is compiler-owned |
| std/program | `arguments()`, `current_directory(h)`, `home_directory(h)`, `temporary_directory(h)`, `executable_path(h)`, `available_parallelism()` | value-less |
| std/entropy | `fill(into)` | operand `Slice` is compiler-owned |
| std/json | `parse(h, text)` | constructs from text |
| std/regex | `compile(h, source)` | constructs from text |

RFCs 0233 (std/json) and 0234 (std/regex) are closed and archived. Changing
their API changes the implementation and `docs/reference.md`; the archived
RFCs stay unchanged.

## Design, part 1: union methods (A+)

### Language

- `method T.name(...)` and `method mut T.name(...)` accept `T` naming a
  nominal struct or nominal union declared by the current module, including
  generic unions under the generic-receiver rules structs use. Union methods
  follow exactly the receiver contract in force for struct methods (RFC
  0254). Pointer, nullable, anonymous union, primitive, and builtin-generic
  receivers stay rejected.
- The diagnostic `type.method-receiver-must-be-struct` becomes
  `type.method-receiver-must-be-nominal`:
  `method receiver must be a struct or union type; got <type>`.
- A method name may not equal any variant payload field name of its union,
  mirroring the struct rule that a method name may not equal a member name;
  otherwise, after narrowing, `v.run()` could name a method or a callable
  payload field. Variant names do not collide: variants are written on the
  type (`Shape.Circle(...)`), methods on values.
- Calls through `Ptr<U>` / `Ptr<mut U>` to a union method autoderef exactly as
  struct method calls do.

```hexal
type Shape is union | Circle as radius: Int32 end | Rect as width: Int32, height: Int32 end end

method Shape.area(): Int32 do
    return match self is
    | Shape.Circle then self.radius * self.radius
    | Shape.Rect then self.width * self.height
    end
end

let a: Int32 = Shape.Circle(radius = 2).area()
```

Zig allows functions and method-call sugar on `union(enum)` types; Odin has
no methods at all.

### Compiler

The method owner is struct-specific throughout. Each site below changes from
`*ObjectType` to one nominal owner (the `*ObjectType` or `*AdtType` pair that
`nominalIdentity` in `compiler/checker/equality.go` already distinguishes),
with accessors for name, module, and C name:

- `MethodDeclaration.Object` and `methodTable` (`compiler/checker/methods.go`);
- `Expression.Owner` (`compiler/checker/operands.go`);
- `specializeMethod` and generic-method registration and keys
  (`compiler/checker/generic_specialization.go`);
- exported method records and imported method calls
  (`compiler/checker/modules.go`, `checkImportedMethodCall`);
- pointer-receiver autoderef, which tests `Element.Object`;
- generator method C naming, prototypes, definitions, and imported calls.

A union method lowers as a struct method does, through the receiver form RFC
0254 defines.

## Design, part 2: std/regex and std/json methods

`Pattern`, `Match`, and `Json.Value` are compiler-owned core-library types
(`Value` is a core-library union), so their methods need no user union
receivers and do not wait for part 1.

```text
Regex.compile(heap: Heap, source: String) -> Regex.Pattern | Error      (unchanged)
Pattern.test(heap: Heap, subject: String) -> Bool | Error               (was Regex.test)
Pattern.find(heap: Heap, subject: String) -> Regex.Span | Nil | Error   (was Regex.find)
Pattern.capture(heap: Heap, subject: String) -> Regex.Match | Nil | Error (was Regex.capture)
Pattern.free(heap: Heap) -> no value                                    (was Regex.free)
Match.free(heap: Heap) -> no value                                      (was Regex.free_match)

Json.parse(heap: Heap, text: String) -> Json.Value | Error              (unchanged)
Value.stringify(heap: Heap) -> String | Error                           (was Json.stringify)
Value.free(heap: Heap) -> no value                                      (was Json.free)
```

- Every contract (allocation, limits, depth, concurrency, errors) is
  unchanged; only the call shape moves.
- The old module functions are removed (one obvious way).
- Dependency demand keeps its meaning: PCRE2 is linked when `compile`, or the
  `test`, `find`, `capture`, or `free` method on a `Pattern`, is reachable, and
  `Match.free` alone selects nothing; yyjson is linked when `parse` or
  `value.stringify` is reachable, and `value.free` alone selects nothing.
- Core-library functions are rows in `compiler/specdata/corelib.go`, which has
  no method rows today; capability-type methods such as `Instant.elapsed` are
  handwritten in the checker (`compiler/checker/time.go`). This RFC adds
  receiver-keyed method rows to the core-library table (a `Methods` list on
  `CoreModule`, each row naming its receiver `CoreTypeID`, parameters,
  result, error behavior, runtime symbol, and components), and moves the five
  regex operations and the two json operations there. The checker's member-call path resolves a method on
  a core-library type through those rows. Existing handwritten capability
  methods are not moved by this RFC.

## Design, part 3: conversions on the source value (E)

A compiler-owned type keeps a type-level function only when no value exists.
A conversion whose result is named by a type is spelled `.to<T>()`, the one
conversion method; a conversion that needs more than a destination type (a
length, a byte order) is a source method.

Interpolation is not a conversion and stays `String.interpolate(h, "...")` /
`String<N>.interpolate("...")`. Its template is compiled into reads of named
locals, so it exists only as a literal in source; a variable's runtime text
has no names left to resolve (probed: passing a String binding is rejected
with `String.interpolate requires an interpreted interpolation template`).
With no value to act on, it is a rule 2 owner call.

```text
Slice<Byte>.to<String>(heap: Heap) -> String | Error       (was String.from_bytes)
Slice<Byte>.to<String<N>>() -> String<N> | Error           (was String<N>.from_bytes)
Slice<Rune>.to<String>(heap: Heap) -> String | Error       (was String.from_runes)
Ptr<T>.to_slice(length: Size) -> Slice<T>                  (was Slice<T>.from_pointer)
Ptr<mut T>.to_slice(length: Size) -> Slice<mut T>          (was Slice<mut T>.from_pointer)
value.to<Rune>() -> Rune | Error                           (was Rune.from; integer source)
List<Byte, N>.decode_le<T>() -> T                          (was T.from_le_bytes; N = size_of<T>())
List<Byte, N>.decode_be<T>() -> T                          (was T.from_be_bytes)
```

- **Contracts unchanged.** UTF-8 validation, allocation, bounds, error, and
  trap behavior are exactly those of the functions they replace.
- **`.to<T>()` grows.** Today it is the one explicit scalar conversion, with a
  mandatory `T` and no value arguments. It gains three destinations:
  - `String` from `Slice<Byte>` or `Slice<Rune>`, taking one `Heap`
    argument because the result is heap-owned;
  - `String<N>` from `Slice<Byte>`, taking no argument, with `N` stated in
    the type argument;
  - `Rune` from any integer (below).

  These three are fallible (`T | Error`); every scalar destination keeps its
  infallible result and its trap on an invalid dynamic value. A `Heap`
  argument is accepted exactly when `T` is `String`.
- **Diagnostics generalize.** `type.numeric-conversion-unsupported` becomes
  `type.conversion-unsupported`:
  `cannot convert <source> to <T>`, covering every unsupported
  source/destination pair. `type.numeric-conversion-value-argument-count`
  becomes `type.conversion-value-argument-count`:
  `to accepts one Heap argument when converting to String, and no value
  arguments otherwise`. The other conversion diagnostics are unchanged.
- **Unsafe.** `ptr.to_slice(n)` is valid only inside `unsafe do ... end`, as
  `from_pointer` is. A read-only view from `Ptr<mut T>` needs no second
  method: `Slice<mut T>` weakens implicitly to `Slice<T>`.
- **Rune conversion (E2).** `value.to<Rune>()` from any integer returns
  `Rune | Error`: a dynamic value that is a surrogate or above U+10FFFF
  returns an Error of kind `InvalidInput`, exactly as `Rune.from` does today.
  Such a constant fails compilation, as every out-of-domain constant does.
  Rationale for the fallible destinations: invalid text and invalid scalars
  are input-validation failures, which Hexal answers with Error, while
  numeric range failures are program bugs, which trap.
- **Byte-order readers (E3).** `decode_le<T>()` / `decode_be<T>()` exist on an
  inline `List<Byte, N>` for a fixed-width integer `T` with
  `N = size_of<T>()`, with `from_le_bytes`'s contract; any other `N` or `T` is
  rejected. They mirror `x.to_le_bytes()` / `x.to_be_bytes()`.
- **No List route (E1).** `.to<String>` accepts Slice sources only; a List
  converts through its Slice, so the conversion has one route.
- **Removed spellings.** `String.from_bytes`, `String<N>.from_bytes`,
  `String.from_runes`, both `from_pointer` forms, `Rune.from`,
  `T.from_le_bytes`, and `T.from_be_bytes` are removed (one obvious way).

```hexal
let s = try bytes.to<String>(h)
let small = try bytes.to<String<64>>()
let back: Slice<Byte> = s.bytes()
let greeting = String.interpolate(h, "Hi {{name}}")
unsafe do
    let view: Slice<Byte> = p.to_slice(n)
end
```

`.to<T>()` stays compiler-owned: user types cannot add destinations, in line
with the excluded user truth/display/hash protocols, so a user type converts
through its own methods (`point.format(h)`), and `String` to `Point` stays
`Geo.parse(text)`.

## Design, part 4: no method calls on literals

A method call's receiver may not be a literal: an integer, float, string
(interpreted or raw), Bool, `nil`, or array literal, including one wrapped
in parentheses. Every other receiver is unchanged: bindings, members,
elements, `self`, dereferences, call results, and parenthesized non-literal
expressions.

```hexal
"carol".copy(h)              -- rejected: bind it first
(3).to<Int64>()              -- rejected
[1, 2, 3].length()           -- rejected

let name = "carol"
let s = name.copy(h)         -- ok
let x = (a + 1).to<Int64>()  -- ok: an expression, not a literal
let v = xs.filter(f).to_list(h)   -- ok: pipeline chains keep working
```

- A literal has no receiver identity of its own: numeric literals take their
  type from context, and a method on one reads as a disguised type-level
  function. Binding it names the value first. `3.to<Int64>()` is already a
  lexical error (`3.` lexes as a malformed float); this rule covers the
  remaining literal forms.
- Call results and non-literal expressions stay valid receivers, because
  lazy pipelines are consumable only by chaining (a pipeline is not a
  bindable value) and `.to<T>()` converts expressions.
- The parser rejects it, the earliest phase that can prove it: a literal
  primary, with or without enclosing parentheses, followed by a method-call
  suffix. Diagnostic `syntax.method-call-on-literal`:
  `a method cannot be called on a literal; bind it with let first`.
- `GRAMMAR.ebnf`: literal primaries take no member-call suffix, updated in
  the same change as the parser.

Migration: 11 snippet sites (`"carol".copy(h)` and similar) and about 57 test
sites (23 in `compiler/tests/c23validation/fixtures_test.go`, the rest in
`compiler/tests/integration/`, mostly `string_test.go`) bind the literal
first.

## Alternatives rejected

- **Guideline only, no union methods (A).** Leaves user resource-owning ADTs
  unable to carry methods; the author chose that every value type can.
- **Functions only (Odin, B).** Removes "methods on objects", a stated
  language goal.
- **Type is a namespace (Zig, C).** Types would declare type-level functions
  (`Point.origin()`) beside module functions, so value-less operations gain
  two spellings (`Geo.origin()` and `Geo.Point.origin()`), and RFC 0186's
  boundary reverses. Mutating methods, the other half of Zig's model, arrive
  through RFC 0254.
- **Uniform call syntax (D).** `x.f(a)` resolving to any visible `f(x, a)`
  gives every call two spellings, and an import can change what `x.f` means.
- **E in its own RFC.** A review suggested it; the author kept E here.

## Swept code

- Union methods: CARE comments stating that methods belong to structs, in
  `compiler/checker/methods.go`, `compiler/checker/operands.go`, and
  `compiler/generator/declarations.go`, and object-specific names such as
  `receiverObject`.
- std/regex and std/json: the seven module-function rows and their call
  handling for `test`, `find`, `capture`, `free`, `free_match`, `stringify`,
  and json `free`.
- E: the type-level call handling for every removed spelling.
- Part 4: none; the rule only rejects a form that compiles today.

## Documentation

- `docs/reference.md`: add the guideline to the functions and methods section
  as the single authority, with its one recorded exception; update the
  receiver bullet and the "no static method" bullet; replace the std/regex
  and std/json signatures and demand rules; replace the String, Slice, Rune,
  and byte-order signatures; rewrite the explicit conversion rule for the
  `String`, `String<N>`, and `Rune` destinations, their `T | Error` results,
  and the `Heap` argument for `String`; update the std module table; add
  the no-literal-receiver rule to the method call rules.
- `GRAMMAR.ebnf`: literal primaries take no method-call suffix (part 4).

## Implementation plan

1. **Union methods** (after RFC 0254): change the method owner at every site
   listed under part 1; widen the receiver check; add the payload-field
   collision rule; update the diagnostic key and text. Validation 1-10.
2. **std/regex and std/json methods** (independent of phase 1): add
   core-library method rows; move the five regex and two json operations;
   update the demand rules, the fixtures, and every snippet using them.
   Validation 11-14.
3. **Conversions:** add the `String`, `String<N>`, and `Rune` destinations of
   `.to<T>()` and the source methods; remove the old spellings; migrate
   snippets, std sources, and fixtures. Validation 15-23.
4. **No literal receivers** (independent of the others): parser rule,
   `GRAMMAR.ebnf`, diagnostic; migrate the 11 snippet sites and the test
   sites by binding each literal first. Validation 24-27.
5. After each phase: rebuild the snippet manifest and confirm only the
   snippets whose sources changed moved; update `docs/reference.md`.

## Validation

Union methods, integration (`compiler/tests/integration/functions_test.go`):

1. A method on a local union compiles; `match self is` narrows `self` inside
   it, and the generated C contains the method definition and the call.
2. A method on a local generic union compiles and specializes for two type
   arguments.
3. Declaring a method on an imported union is rejected with the existing
   imported-type diagnostic.
4. `Int32`, `Ptr<T>`, `Bool | Int32`, and `Fun<...>` receivers are rejected
   with `method receiver must be a struct or union type; got <type>`; the
   existing struct-only assertions are updated to the new text.
5. A union method whose name equals a variant payload field name is rejected.
6. A union method called through `Ptr<U>` compiles.
7. An exported union method is callable from an importing module.

Union methods, C23 lane (`fixtureCatalog` entries):

8. A local union method runs and returns each variant's result.
9. An exported union method called across modules runs.
10. A generic union method specialized for two type arguments runs.

std/regex and std/json:

11. `pattern.test`, `pattern.find`, `pattern.capture`, `pattern.free`,
    `match.free`, `value.stringify`, and `value.free` compile with the
    documented result types (integration).
12. The seven removed module functions (`Regex.test`, `Regex.find`,
    `Regex.capture`, `Regex.free`, `Regex.free_match`, `Json.stringify`,
    `Json.free`) are rejected as unknown module members (integration).
13. A program calling only `match.free` selects no PCRE2 dependency and one
    calling `pattern.test` selects it; a program calling only `value.free`
    selects no yyjson dependency and one calling `value.stringify` selects it
    (integration, on `Dependencies`).
14. The existing regex and json fixtures, migrated to methods, pass unchanged
    in behavior (C23 lane).

Conversions:

15. `bytes.to<String>(h)`, `runes.to<String>(h)`, and
    `bytes.to<String<64>>()` compile with result type `T | Error`
    (integration).
16. `bytes.to<String>()` without a `Heap` and `bytes.to<String<64>>(h)` with
    one are rejected (integration).
17. `.to<String>(h)` on `Slice<Int32>` and `.to<String<64>>()` on
    `Slice<Rune>` are rejected with `cannot convert <source> to <T>`; the
    existing unsupported numeric conversion tests assert the new key and text
    (integration).
18. `String.interpolate(h, "...")` and `String<N>.interpolate("...")` compile
    unchanged (integration).
19. `p.to_slice(n)` outside `unsafe` is rejected; inside it compiles, and a
    `Slice<T>` binding accepts it from `Ptr<mut T>` (integration).
20. Each removed spelling is rejected, and the existing String, Slice, Rune,
    and byte-order fixtures, migrated, pass unchanged in behavior
    (integration and C23 lane).
21. The constant expressions `(0xD000 + 0x800).to<Rune>()` and
    `(0x100000 + 0x10000).to<Rune>()` fail compilation; `c.to<Rune>()` on a
    `UInt32` binding compiles with result type `Rune | Error`; `.to<Int32>()`
    keeps its infallible result type (integration).
22. A dynamic surrogate converted with `.to<Rune>()` returns an Error of kind
    `InvalidInput`, and a dynamic valid scalar returns the Rune (C23 lane).
23. `b.decode_le<Int32>()` on `List<Byte, 4>` round-trips `x.to_le_bytes()`,
    and `decode_le<Int32>()` on `List<Byte, 3>` is rejected (integration and
    C23 lane).

No literal receivers (`compiler/tests/integration/functions_test.go`):

24. `"carol".copy(h)`, a raw-string receiver, `(3).to<Int64>()`,
    `true.to<Int32>()`, and `[1, 2, 3].length()` are rejected with
    `syntax.method-call-on-literal`.
25. `name.copy(h)` on a String binding, `(a + 1).to<Int64>()`, a method on a
    call result, and a lazy pipeline chain `xs.filter(f).map(g).to_list(h)`
    compile.
26. `TestGrammarIsVerifiable` passes with the updated grammar.
27. The migrated snippets and tests pass, and the snippet manifest moves only
    for the 11 migrated snippets.

## Decisions

- **Conversions to a type use `.to<T>()`:** `.to<String>(h)` and
  `.to<String<N>>()` replace `from_bytes` / `from_runes`; there is no
  `to_string` method.
- **std/json is not an exception:** `value.stringify(h)` and `value.free(h)`.
- **No method calls on literals** (option B): only literal receivers are
  rejected; call results and expressions stay valid so pipelines and
  expression conversions keep working.
- **Interpolation stays type-level:** `String.interpolate(h, "...")`, because
  the template exists only as compile-time syntax.
- **E1:** no `List` route; a List converts through its Slice.
- **E2:** `value.to<Rune>()` returning `Rune | Error`, keeping `Rune.from`'s
  `InvalidInput` path.
- **E3:** `decode_le<T>()` / `decode_be<T>()`.
- **std/ascii** stays module functions: it operates on `Byte`, which it does
  not own.
