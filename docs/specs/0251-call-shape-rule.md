# RFC 0251: One Call-Shape Rule for Methods, Type Functions, and Module Functions

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft. Decision pending: option A (rule only), A+ (rule plus
  nominal-union receivers), and/or E (conversions move onto the source
  value); E is independent of A/A+ and may combine with either. Options B, C,
  and D are recorded as rejected alternatives.

## Summary

Hexal has three call shapes. Which one an operation gets is currently decided
by who owns the type, not by what the operation is:

```hexal
list.push(x)                   -- instance method
String.from_bytes(h, bytes)    -- type-level function (compiler-owned types only)
Fs.open(path, mode)            -- module function
```

This RFC writes down the one rule that already explains almost every existing
call, and fixes the one place where the rule cannot be followed today: a module
that declares a union cannot give it methods, so a resource-owning union is
released with `Json.free(h, v)` while every compiler-owned resource is released
with `v.free(h)`.

## The rule

An operation `f` is declared by exactly one owner: a module (user or std), or
a compiler-owned type, which has no module.

1. **Value-first**: if the first operand of `f` is a value of a nominal type
   declared by the same owner, `f` is a method and is called
   `value.f(rest...)`.
2. **Otherwise** (constructors from foreign input, value-less operations,
   operations whose primary operand is a type the owner does not declare), `f`
   is called `Owner.f(args...)`:
   - `Owner` is the import alias for a user or std module;
   - `Owner` is the type name for a compiler-owned type (`String`, `Slice<T>`,
     `Rune`, `Task`, integer types), because it has no module to name.
3. **Construction** of a nominal value by its fields stays `Type(...)` /
   `Owner.Type(...)`; compiler-owned canonical constructors stay `Type(...)`
   (`Heap()`, `List<T>(h)`).

User types never get type-level functions: a user module's `Owner` is its
alias, so `Geo.origin()` is the one spelling (RFC 0186 stays in force).

Allocator direction follows from rule 1 and is not an exception:

- `heap.free(ptr)`: `Ptr<T>` does not record which allocator made it, so the
  allocator is the value the operation is about.
- `list.free(heap)`, `s.free(heap)`: the collection owns its storage; the heap
  is where it goes back to.

## Inventory (from `docs/reference.md`)

### Instance methods (rule 1)

| Owner | Methods |
| --- | --- |
| `Heap` | `allocate`, `allocate_aligned`, `free` |
| `Stash` | `allocate`, `reset`, `destroy` |
| `Pool<T>` | `allocate`, `free`, `destroy` |
| `Ptr<T>` | `offset`, `cast`, `read_volatile`, `write_volatile` |
| `std/io` `Ptr<mut Bytes>` | `read`, `write`, `seek` |
| `Slice<T>` | `length`, `slice`, `to_list` |
| `List<T>` | `length`, `push`, `pop`, `clear`, `slice`, `mut_slice`, `map`, `filter`, `free` |
| `Dict<K,V>` | `insert`, `get`, `find`, `contains`, `remove`, `length`, `clear`, `keys`, `values`, `entries`, `to_list`, `free` |
| `String` | `length`, `rune_length`, `grapheme_length`, `bytes`, `byte_cursor`, `rune_cursor`, `grapheme_cursor`, `slice`, `copy`, `concat`, `casefold`, `normalize`, `widen`, `c_pointer`, `free` |
| `Rune` | `value`, `to_lower`, `category` |
| `Channel<T>` | `send`, `receive`, `close`, `length`, `capacity`, `is_closed`, `free` |
| `Mutex` | `lock`, `unlock`, `free` |
| `Atomic<T>` | `load`, `store`, `exchange`, `fetch_add`, `fetch_sub`, `compare_exchange` |
| `Task` | `join`, `detach` |
| `Error`, `ErrorKind` | `header` |
| `std/io` `IO` | `read`, `write`, `seek`, `close` |
| `std/fs` `File` | `read`, `write`, `seek`, `flush`, `close` |
| `std/time` `Duration` | `as_nanoseconds`, `as_microseconds`, `as_milliseconds`, `as_seconds` |
| `std/time` `Instant` | `elapsed`, `duration_since` |
| `std/time` `WallTime` | `seconds`, `nanosecond` |
| `std/net` `Address` | `format` |
| `std/net` `TcpConnection` | `read`, `write`, `shutdown`, `no_delay`, `close` |
| `std/net` `TcpListener` | `accept`, `close` |
| `std/process` `Process`, `Pipe` | `wait`, `terminate`, `close`; `read`, `write`, `shutdown`, `close` |
| `std/signal` `Signals` | `next`, `close` |
| user struct | any `method T.name(...)` |

### Type-level functions on compiler-owned types (rule 2, `Owner` = type)

| Call | Why not a method |
| --- | --- |
| `String.from_bytes(h, bytes)`, `String.from_runes(h, runes)` | constructs from a non-String value |
| `String.interpolate(h, template)` | constructs; the template is compiler-owned syntax |
| `Slice<T>.from_pointer(ptr, n)`, `Slice<T>.empty()` | constructs; no Slice yet |
| `Rune.from(value)` | constructs from `UInt32` |
| `T.from_le_bytes(b)`, `T.from_be_bytes(b)` | constructs an integer from bytes |
| `Task.yield()` | value-less |

### Module functions (rule 2, `Owner` = module alias)

| Module | Functions | Why not a method |
| --- | --- | --- |
| `std/io` | `stdin()`, `stdout()`, `stderr()`, `bytes_over(buffer)` | value-less / constructs |
| `std/fs` | `open(path, mode)` | constructs from a path |
| `std/time` | `nanoseconds`, `microseconds`, `milliseconds`, `seconds`, `now()`, `wall_time()`, `sleep(d)` | construct / value-less |
| `std/net` | `parse_address`, `resolve`, `connect`, `listen` | construct from text or Address |
| `std/process` | `start(...)` | constructs |
| `std/signal` | `subscribe(subscriptions)` | constructs |
| `std/terminal` | `is_attached(stream)`, `size(stream)` | operand `IO` is owned by `std/io` |
| `std/ascii` | `is_digit`, `is_alpha`, `is_space`, `to_lower`, `to_upper` | operand `Byte` is compiler-owned |
| `std/program` | `arguments()`, `executable_path()` | value-less |
| proposed `std/json` (0233) | `parse(h, text)`, `stringify(...)` | constructs / see below |

### Outliers

| Call | Rule says | Blocked by |
| --- | --- | --- |
| `Json.free(h, value)` (0233) | `value.free(h)`: `Value` is declared by `std/json` | `Value` is a union; `method` accepts only structs (`method receiver must be a struct type`) |
| any user ADT owning a resource | `value.close()` | same |

No other existing call violates the rule. `Ascii.to_lower(b)` beside
`rune.to_lower()` is not an outlier: `std/ascii` does not own `Byte`, while
`Rune` is compiler-owned and declares its own methods.

## Options considered

### A. Codify the rule; no language change

Add the rule to `docs/reference.md`. The outliers stay: 0233 keeps
`Json.free(h, value)` and the reference records it as the forced exception for
union-typed values.

### A+. Codify the rule; allow `method` on nominal unions (recommended)

Widen the receiver rule from "nominal struct" to "nominal struct or nominal
union (ADT)". Everything else about methods is unchanged: `self` is an
immutable copy, the receiver must be shallow-copyable, only the declaring
module may add methods, no type-level functions, no mutation through `self`.

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

```hexal
-- std/json declares: method Value.free(heap: Heap)
let root: Json.Value = try Json.parse(h, text)
root.free(h)                   -- same shape as list.free(h), s.free(h)
```

### E. Conversions live on the source value

Independent of A/A+; combinable with either. Rule 2's type-level branch
narrows: a compiler-owned type keeps a type-level function only when no value
exists to receive the call. Every conversion becomes an instance method on its
source value, declared by the source's owner.

| Today | Under E | Note |
| --- | --- | --- |
| `String.from_bytes(h, b)` | `b.to_string(h)` on `Slice<Byte>` | round-trips with `s.bytes()` |
| `String.from_runes(h, r)` | `r.to_string(h)` on `Slice<Rune>` | same name; element type selects |
| `String.interpolate(h, "Hi {{name}}")` | `"Hi {{name}}".interpolate(h)` | lexer already tokenizes `{{ }}` in every interpreted string; only the checker's permitted position moves from argument to receiver |
| `String<N>.interpolate(t)` | `let s: String<64> = "...".interpolate()` | inline vs heap form selected by arity plus expected type |
| `Slice<T>.from_pointer(p, n)` | `p.to_slice(n)` | joins `offset`, `cast` on `Ptr` |
| `Rune.from(v)` | `v.to_rune()` or `v.to<Rune>()` | open question E2 |
| `T.from_le_bytes(b)` / `T.from_be_bytes(b)` | `b.decode_le<T>()` / `b.decode_be<T>()`, or unchanged | open question E3 |
| `Slice<T>.empty()` | unchanged | no value exists |
| `Task.yield()` | unchanged | no value exists |

```hexal
let s = try bytes.to_string(h)
let back: Slice<Byte> = s.bytes()
let greeting = "Hi {{name}}".interpolate(h)
let view = p.to_slice(n)
let r = try code.to_rune()
```

User code already follows E: `point.to_string(h)` is declared by the module
owning `Point`; `String` to `Point` stays `Geo.parse(text)` because `Geo` does
not own `String`.

Costs:

- **Element-specific built-in methods are new.** No built-in method today
  exists only for a particular element type. `Slice<Byte>.to_string` and
  `Slice<Rune>.to_string` would be the first; any other element type is
  rejected with a dedicated diagnostic
  (`to_string requires Slice<Byte> or Slice<Rune>; got <type>`).
- **`Rune.from` meets the single-conversion rule.** `value.to<T>()` is the only
  explicit scalar conversion (`reference.md:815`) and traps on invalid dynamic
  values; `Rune.from` returns `Rune | Error`. Folding it into `.to<Rune>()`
  trades the Error for a trap; `v.to_rune()` keeps the Error but is a second
  scalar-conversion spelling, requiring line 815 to admit fallible
  `to_<type>()` conversions.
- **Byte-order readers have no source-shaped name.** The source
  `List<Byte, N>` does not determine the result (`Int32` vs `UInt32`), so a
  type argument is required; `b.from_le<Int32>()` reads backwards and
  `b.decode_le<Int32>()` no longer mirrors `x.to_le_bytes()`.
- **Discoverability moves.** "How do I make a String" is answered on `Slice`
  and `Ptr`, not `String`; the reference's String section cross-references
  them.

### B. Functions only (Odin) — rejected

Every method becomes `Owner.f(value, ...)`: `List.push(list, x)`. Removes
"methods on objects", a stated language goal, and rewrites every snippet and
every std contract for no semantic gain.

### C. Type is a namespace (Zig) — rejected

Any nominal type may declare methods and type-level functions
(`Point.origin()`), and a method may take an explicit `Ptr<mut Self>`
receiver. Rejected: it reverses RFC 0186; value-less operations gain two
spellings (`Geo.origin()` and `Geo.Point.origin()`), breaking "one obvious
way"; and mutating methods add a second mutation idiom beside
`fun f(p: Ptr<mut T>)`.

### D. Uniform call syntax — rejected

`x.f(a)` resolves to any visible `f(x, a)`. Rejected: every call gains two
spellings, method resolution becomes scope-dependent (an import can change
what `x.f` means), and the checker gains a lookup order to specify and test.

## Design (A+)

### Language

- `method T.name(...)` accepts `T` naming a nominal struct or nominal union
  declared by the current module, including generic unions under the same
  generic-receiver rules structs use. Pointer, nullable, anonymous union,
  primitive, and builtin-generic receivers stay rejected.
- The diagnostic `type.method-receiver-must-be-struct` becomes
  `type.method-receiver-must-be-nominal` with text
  `method receiver must be a struct or union type; got <type>`.
- A union has no members, so the member/method shared namespace rule has
  nothing to collide with; variant names live on the type (`T.Variant`) and
  methods on values (`v.name`), so they cannot collide either.

### Compiler

- `compiler/checker/methods.go:288` accepts `target.Adt != nil`; the
  defining-module check reads `Adt.ModuleID`.
- `methodTable` is keyed by `*ObjectType`; rekey it by nominal identity
  (`nominalIdentity`, `compiler/checker/equality.go:121`, already returns the
  `*ObjectType` or `*AdtType`). `MethodDeclaration.Object` becomes an owner
  identity usable by the generator's method C naming and the module export
  path; every current reader of `.Object` on a method is updated in the same
  change.
- `methodReceiverCopyDiagnostic` already classifies ADTs through the ordinary
  copy classification; no new rule.
- Generated C: a union method lowers exactly as a struct method does (a
  function taking the union's C struct by value); no new generator construct.

### Documentation

- `docs/reference.md`: add the rule above to the functions/methods section as
  the single authority; update the method receiver bullet (line 594) and the
  "no static method" bullet to cite it.
- `GRAMMAR.ebnf`: no change (receiver is already a type name).
- RFC 0233 is closed and archived. Its `Json.free(heap, value)` function
  remains the current API in `docs/reference.md`; if this ADR changes that
  surface, update the implementation and reference, and leave the archived RFC
  unchanged.

### Swept code

None under A+: no existing code works around the struct-only receiver.

## Validation

Integration tests in `compiler/tests/integration/functions_test.go` beside the
existing receiver tests:

1. A method on a local union compiles; calling it on a value returns the
   expected variant-dependent result (assert the generated C contains the
   method definition and the call).
2. A method on a local generic union (`type Opt<T> is union ...`) compiles and
   specializes.
3. Declaring a method on an imported union is rejected with the existing
   imported-type diagnostic.
4. A method on a union containing `Atomic<T>` is rejected with
   `method receiver must be shallow-copyable; got <type>`.
5. `Int32`, `Ptr<T>`, `Bool | Int32`, and `Fun<...>` receivers are rejected
   with `method receiver must be a struct or union type; got <type>`; the
   existing struct-only assertions are updated to the new text.
6. Writing to `self` inside a union method is rejected with the existing
   struct-method diagnostic.
7. The snippet manifest moves only for snippets whose diagnostics text changed;
   none is expected.

## Open questions

1. A or A+? A costs nothing and leaves `Json.free(h, v)` as a documented
   exception; A+ removes it for the price of the method-table rekey.
2. Should `std/ascii` byte classification move onto `Byte` as compiler-owned
   methods (`b.is_digit()`), mirroring `rune.to_lower()`? The rule does not
   require it; recommendation: no, `std/ascii` is a source stdlib module and
   the split is principled.
3. Adopt E?
   - E1. Should `List<Byte>` / `List<Rune>` also get `to_string(h)`, or must
     callers write `list.slice().to_string(h)`? Adding it gives the
     conversion two routes.
   - E2. `v.to_rune()` (keeps `Rune | Error`, amends the single-conversion
     rule) or `v.to<Rune>()` (one conversion spelling, traps instead).
     Recommendation: `v.to_rune()`.
   - E3. Byte-order readers: `b.decode_le<T>()` / `b.decode_be<T>()`, or keep
     `T.from_le_bytes(b)` as a type-level exception.

Design, Validation, and Documentation above cover A+ only; choosing E adds
its own sections before this spec is implementation ready.
