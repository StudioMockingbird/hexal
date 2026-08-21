# RFC 0095: Generated C Naming

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed
- Created: 2026-08-20
- Updated: 2026-08-20
- Scope: make generated structural-union and ADT type names injective on type
  identity, remove redundant structural-union encoding, and restore
  order-independent structural-union interning for same-named nominal members
- Depends on: nothing. Replaces RFC 0089's encoder entirely — both its basis
  and its length-prefix scheme
- Coordinates with: `docs/reference.md`, `docs/status.md`, the snippet manifest
- Does not change: Hexal syntax or program acceptance, the general union
  representation, discriminant or payload-member naming schemes, payload
  storage, or the `hex_` prefix scheme. It restores the existing documented
  order-independent union-identity contract and therefore changes canonical
  member/tag order only where the current comparator incorrectly treats two
  distinct nominal members as equal

## Summary

Two problems in one identifier, found from one report that union names read as
noise:

```c
hex_union_7_int32_t9_nullptr_t_tag_member_1
```

**It is too long**, and 13 of its 43 characters carry no information.

**And two families are not injective on type identity, which produces generated
C that does not compile.** Both were found while looking into the verbosity:

- `Rune | Nil` and `UInt32 | Nil` are distinct types sharing one union name,
  because `Rune` and `UInt32` share the C spelling `uint32_t`.
- Two modules each declaring a `Shape` ADT produce one name for two distinct
  types, because an ADT name carries no module owner.
- `M.Point | S.Point` and `S.Point | M.Point` can intern as two types because
  the member comparator omits module identity when source coordinates and short
  names are equal.

The first two defects emit conflicting definitions with one C name. The third
emits two wrapper types and a conversion helper for one structural Hexal type.

One cause underlies all three problems. **List, View, Array, and Dict build
their names from the Hexal name and establish uniqueness by asking the shared
arena. Unions encode uniqueness into the name instead, and ADTs assume it.**
Adopting the established mechanism for both fixes the defects, and drops the
union wrapper name from 30 characters to 15 as a side effect. This RFC changes
generated identity spelling and repairs the already-required structural
identity normalization. RFC 0099 separately owns discriminants, payload field
names, and union/ADT representation.

## Evidence — the collision

Enumerated over 68 named types: every builtin, plus one specialization of List,
View, Array, Ptr, Task, Channel, Atomic, Dict, and a union, each over `Rune`,
`UInt32`, `Byte`, `UInt8`, and `Int32`. A collision is two types that are not
`Equal` sharing one `CName`. Three exist, all rooted in one:

| Types | Shared `CName` | Harmful |
|---|---|---|
| `Rune` / `UInt32` | `uint32_t` | root cause |
| `Ptr<Rune>` / `Ptr<UInt32>` | `uint32_t*` | **no** — see Non-goals |
| `Rune \| Nil` / `UInt32 \| Nil` | `hex_union_8_uint32_t9_nullptr_t` | **yes** |

`Byte` and `UInt8` do *not* collide: they are one identity with two spellings,
so `Equal` holds and one union results, correctly.

Reproduced:

```hexal
a: Rune | Nil := nil
b: UInt32 | Nil := nil
```

`modules/app.h` contains `typedef struct hex_union_8_uint32_t9_nullptr_t {`
twice, along with a doubled tag enum and payload union. Redefining a struct tag
with a body is a constraint violation; the translation unit does not compile.
No test observes this, because no test compiles generated C.

**Why the guard did not catch it.** RFC 0089 added a property test for exactly
this hazard and its comment names this pair, then excludes it:

```go
// one C spelling reached by two Hexal names is fine and pre-existing
```

The stated reason was that a union of two same-spelled members collapses to one
member. That is true and irrelevant: the failure is not two members inside one
union, it is two *different* unions landing on one name. The test proved the
`hex_` stripping is injective over its inputs and never asked whether the
inputs were injective over type identity.

## Evidence — the second collision, in ADTs

An ADT's C name carries no module segment. Object types do — `hex_t_m3_app_Point`
— but an ADT is spelled `hex_Shape`. Two modules each declaring a `Shape` is an
ordinary program, and the reference's nominal-identity rule makes those
distinct types. Probed:

```hexal
-- m.hex
export type Shape = | Circle as { r: Int32 } | Square as { a: Int32 }
export fun make(): Shape do ... end

-- app.hex
type Shape = | Circle as { r: Int32 } | Square as { a: Int32 }
b: M.Shape := M.make()
```

`modules/app.h` contains `typedef struct hex_Shape { … } hex_Shape;` **twice**,
once for each distinct type. Same failure as the union case and reachable
without any exotic type: two modules, one common type name.

The fix is the same shape — route the name through the arena and give it the
module owner the object family already carries — so this RFC covers both rather
than leaving a known duplicate for a later one.

## Evidence — the scope a union name must be unique over

A union definition is **not** emitted once per program. Probed with a union
crossing a module boundary as a function result:

```
modules/m.h      defines the union struct 1 time
modules/app.h    defines the union struct 1 time
```

Module headers never include one another, so each consuming module re-emits the
definition into its own translation unit. `M.get()` is declared in `app.h`
against `app.h`'s struct tag and defined in `m.c` against `m.h`'s struct tag. C
requires those to be compatible types, which for structs means the same tag and
the same members. **So the name must agree across independently emitted headers.**

That rules out a name derived from anything module-local — a per-module counter,
a per-file ordinal, a source position — because `app.h` would choose
independently of `m.h`.

It does **not** rule out a compilation-wide counter. `types.go:318`:

> Every module of one compilation must share one arena so constructed types
> intern once per compilation; the checker creates the arena in `CheckModules`
> and passes it to each module.

The arena is the shared registry, it already holds `unionTypes`, and every
module resolves the same interned `Type` and reads the same `CName`. An earlier
revision of this RFC claimed a counter "would need a global registry" that does
not exist. That was wrong; it exists and four families already depend on it.

## Evidence — unions are the only family doing this the hard way

```
hex_list_Rune       hex_list_UInt32        distinct
hex_view_Rune       hex_view_UInt32        distinct
hex_array_Rune_2    hex_array_UInt32_2     distinct
hex_dict_Int32_Rune hex_dict_Int32_UInt32  distinct
hex_union_8_uint32_t9_nullptr_t            both
```

Collections build `hex_list_` + `SanitizeIdentifier(element.Name)` and pass it
to `uniqueCollectionCName` (`types/collections.go`, `types/arena.go`), which
returns the base name when free and otherwise appends the module owner, then a
counter, until it is not taken. Two facts follow: the common name is short and
readable, and uniqueness is *checked* rather than *encoded*.

`unionCName` uses `member.CName` and neither helper. It carries length prefixes
because it must be injective by construction, having no registry to consult —
except that the registry was there all along.

## Evidence — union identity is not fully order-independent

`compareUnionMembers` can return equality for distinct canonical types in three
branches of `unionDisplayKey`:

- objects use source line, source column, and short name, but omit module
  identity;
- pointers use `Ptr`/`MutPtr` plus the element's short name, but omit the
  element's canonical identity;
- the default branch, including ADTs, uses only the short type name.

The stable sort therefore preserves source-written order for each tie, and
`unionKey` then joins differently ordered, module-qualified `CanonicalKey`
values.

Verified with two imported `Point` types:

```hexal
a: M.Point | S.Point := M.make()
b: S.Point | M.Point := a
```

The compiler currently emits two wrapper types and lowers `b := a` through
`hex_internal_widen_...`. Focused probes reproduced the same two-wrapper and
widening output for reversed unions of same-named ADTs and of pointers to
same-named objects. Each pair denotes one structural union under
`reference.md`; no widening exists between a type and itself.

## The change

Build the union name the way every other constructed family builds one:

```
hex_t_Int32_Nil                  (15, was 30)
hex_t_Rune_Nil                   distinct from
hex_t_UInt32_Nil
hex_t_List_Int32__Nil            composed member, sanitized
```

- Prefix `hex_t_`. A union is a type, and `reference.md`'s naming table already
  assigns `hex_t_` to types. This shares a C namespace with nominal types;
  lowercase source type names can deliberately form a nominal-looking union
  base, so the program-wide reservation mechanism below is the guarantee. No
  spelling-shape argument substitutes for that check.
- Join `SanitizeIdentifier(member.Name)` for each member with `_`.
- Reserve definition-keying C names in one program-wide arena registry. The
  registry maps a generated C name to its canonical type key and covers fixed
  compiler-owned types, every reachable nominal object/ADT, and every
  constructed type whose C name introduces a definition.
- Before resolving bodies or constructing unions, `CheckModules` reserves the
  module-qualified object and ADT names from every reachable module. Nominal
  names remain fixed; a later union can never claim or rename one.
- Reserve the union candidate against that registry. Reuse the stored name for
  the same canonical key. If another canonical key owns the candidate, try the
  structural-union base followed by `_0`, `_1`, and so on until reservation
  succeeds. Do not append a module-owner segment: a structural union has no
  declaring module. This is the counter phase of the existing constructed-type
  convention, stated explicitly for unions.
- Keep the existing family maps as the type interners. The definition-name
  registry owns only cross-family generated-C uniqueness; it is not a Hexal
  symbol table and performs no source name resolution.
- Make `compareUnionMembers` a total order: retain its existing display rank
  and display key, then compare `CanonicalKey` when those are equal. Use that
  one ordered member list for the interning key, display name, C name, and
  representation. Do not derive the final tie-break from source order, module
  traversal order, `CName`, or process-global identity.
- No length prefixes. Uniqueness comes from the registry, not the encoding.
- **Case is preserved.** Lowercasing to `hex_t_int32_nil` reads better and
  reintroduces this RFC's own defect by another route: `Foo` and `foo` are both
  legal, distinct Hexal type names — verified by compiling a program declaring
  both, with `Foo | Nil` and `foo | Nil` beside each other — so a case fold
  collapses two distinct unions onto one name. Preserving case also keeps
  unions consistent with `hex_list_Int32`, which preserves it today.

`Int32_Nil_Foo` is reachable from both `Int32 | Nil | Foo` and a two-member
union whose first member is a user type named `Int32_Nil`. The shared registry
resolves that ambiguity by retaining the base for its first canonical owner and
adding a numeric suffix to the other. All reachable nominal names are reserved
first, so traversal order can never make a union steal a nominal name.

The existing union-local tag enum, payload-union typedef, ordinal
`_tag_member_N` constants, and `member_N` fields remain unchanged apart from
deriving their helper-type stems from the corrected wrapper name. RFC 0099
removes or replaces those constructs; changing them here would create an
intermediate naming scheme that is immediately discarded.

### ADTs gain the module owner they already lack

An ADT becomes `hex_t_<encoded-owner>_<Name>`, matching the object family
exactly — `hex_t_m3_app_Shape`. Variant tags and payload fields keep their
current variant-name spelling; only the type stem changes, so
`hex_t_m3_app_Shape_Circle` and `.payload.Circle`.

This is the object family's rule applied to the one nominal family that does
not follow it. It costs characters rather than saving them, which is the
correct trade when the alternative is two distinct types sharing one struct tag.

## Options considered

| Option | `Int32 \| Nil` | Fixes the defect | Verdict |
|---|---|---|---|
| **Hexal names + arena uniqueness** | `hex_t_Int32_Nil` (15) | yes | **taken** |
| Hexal names + length prefixes | `hex_union_5_Int32_3_Nil` (22) | yes | rejected — encodes what the registry can check |
| Compilation-wide counter, `hex_t_1` | `hex_t_1` (7) | yes | rejected — see below |
| Give `Rune` its own C spelling | 30, unchanged | yes, at the root | deferred — see below |
| Module-local `typedef … hex_u1;` alias | short at every use | no | rejected |

**A bare compilation-wide counter is correct and is still rejected**, on two
grounds. It is opaque: `hex_t_1` tells a reader nothing, and `AGENTS.md`
requires generated C to stay as plain as the compiler source. And it makes
*every* union name depend on compilation-wide discovery order, so adding one
`try` early in one module renumbers unions in every module after it and moves
manifest entries for unrelated snippets. The descriptive name has neither
property: it depends only on the member set.

The counter is not discarded — it survives exactly where it belongs, as
`uniqueCollectionCName`'s last-resort disambiguator, reached only when two
distinct types genuinely want one name. Rare by construction, and identical to
how the other four families behave today.

**Giving `Rune` a distinct C spelling** — a `hex_rune` typedef over `uint32_t` —
is the more principled root fix and would close all three collisions rather than
the one harmful one. Deferred because this RFC closes the harmful case, and
respelling every `Rune` in every generated program is a larger change than the
remaining benign row justifies. Recorded because the question recurs.

**A module-local alias** puts two names on one type in output meant to be read
directly, and the reader must chase the alias to learn anything.

## Invariants

1. Union representation shape, tag/payload member naming, and `sizeof` are
   unchanged. Member/tag order changes only for distinct canonical members that
   the old comparator incorrectly treated as equal; the new order is total and
   canonical.
2. Two distinct Hexal types never share a generated C name that keys a
   definition. `Ptr` is exempt because it names no definition.
3. Uniqueness is established by the shared arena, not by the encoding. The
   member list need not be recoverable from the name.
4. One interned type has one name across every module of a compilation, because
   every module reads it from the shared arena.
5. Union identity and representation order are independent of source-written
   member order. Display rank/key may group members, but `CanonicalKey` is the
   mandatory final tie-breaker.
6. Nominal names are reserved before constructed names; no construction order
   can rename or collide with a nominal type.
7. Hexal syntax and program acceptance do not change. The union-identity repair
   restores the existing order-independent structural-type contract rather than
   introducing a new one.

## Coordination with RFC 0099

RFC 0099 consumes the injective wrapper and ADT identities established here,
then separately changes their representation:

- one program-wide `hex_tag` replaces union-local and ADT-local tag enums;
- canonical discriminant and payload labels replace ordinal union labels;
- Nil, EoS, and payload-free ADT variants remain tag-only;
- union payload storage becomes an unnamed union inside the wrapper.

Those changes are intentionally excluded from this RFC. Its implementation
therefore remains valid whether RFC 0099 lands immediately afterward or later.

## Validation

This section is exhaustive.

- `Rune | Nil` and `UInt32 | Nil` in one program produce two distinct union
  types, and each struct, tag enum, and payload union is defined exactly once
  per translation unit.
- A property test asserts that no two non-`Equal` types share a `CName` that
  keys a definition. Enumerate builtins from the `types` package registry, not
  from a list written in the test, and cover one specialization of every
  constructed family. `Ptr` is excluded with the reason recorded in the test.
- The test fires: constructing two distinct types with one definition-keying
  `CName` fails it.
- `Int32 | Nil` generates wrapper type `hex_t_Int32_Nil`. Its union-local tag
  and payload helper types derive from that corrected stem, while ordinal
  `_tag_member_N` constants and `member_N` payload fields retain their existing
  order and meaning. Nil has no payload field.
- Two modules each declaring a `Shape` ADT produce two distinct C types, each
  defined once, named `hex_t_<owner>_Shape` with the owner encoded as the
  object family encodes it.
- An ADT variant tag and payload keep their variant-name spelling:
  `hex_t_m3_app_Shape_Circle` and `.payload.Circle`.
- A union whose member is a composed type — `List<Int32> | Nil` — generates a
  valid C identifier containing no `<`, `>`, `,`, `|`, or space.
- Two distinct unions whose sanitized member names would produce one string
  both compile, receive distinct names, and each is defined once.
- A deliberately constructed union base that equals a reachable nominal C name
  receives a suffixed union name; the nominal name remains unchanged regardless
  of module traversal or source-map insertion order.
- `Foo | Nil` and `foo | Nil`, over two type names differing only in case,
  produce two distinct union types each defined once. This is the case a
  lowercased scheme would break.
- A union name never collides with a nominal type sharing the `hex_t_` prefix:
  the uniqueness check consults nominal names, and a test asserts it does.
- The same union written in two modules still produces one C type with one
  name, and a union crossing a module boundary as a function result is spelled
  identically in both headers.
- `Math.Point | Shapes.Point` and `Shapes.Point | Math.Point` share one interned
  type and one C name even when the two nominal declarations have identical
  source coordinates and short display names.
- Assigning a value between those two spellings lowers as an ordinary same-type
  assignment and emits no `hex_internal_widen_` helper or call.
- Repeat the preceding identity, assignment, and no-widening assertions for
  `Math.Shape | Shapes.Shape` versus its reverse, where both same-named members
  are ADTs. Module-qualifying each ADT's C name does not satisfy this case:
  ADTs use the separate default `unionDisplayKey` branch, which currently reads
  `Type.Name` rather than `CName`.
- Repeat them for `Ptr<Math.Point> | Ptr<Shapes.Point>` versus its reverse,
  covering the pointer `unionDisplayKey` branch whose current key uses only the
  element's short name.
- Two structurally different unions still produce two names, including
  `M.Point | Bool` versus `S.Point | Bool`.
- Nested and widened unions, union equality helpers, truthiness helpers, and
  `try` results all name the same type consistently after the change.
- Two compilations of the same sources produce byte-identical files.
- `docs/reference.md` records that generated definition-keying names are
  injective on canonical type identity; this RFC adds no discriminant or
  payload-member naming contract.
- The snippet manifest moves for every snippet containing a union or ADT —
  expect wide union movement, since `try` produces one on most non-trivial
  programs — and for no snippet containing neither.
- `go test ./...`, `go vet ./...`.

## Required sweep

- Replace `TestStrippingHexPrefixNeverMergesTwoCSpellings`; it guards the
  deleted length-delimited encoder but excludes the identity collision this RFC
  fixes. The new definition-keying `CName` property test supersedes it.
- Replace the literal old-encoder assertion in `TestNestedUnionEncoding` with
  the canonical-name and registry invariants. Preserve the source-level nested
  union coverage in `TestNestedUnionEncodingIntegration`, updating only its
  generated-name expectations.
- Update every generated-C text assertion containing the old `hex_union_`
  encoding. Do not weaken behavioral assertions around injection, widening,
  equality, truthiness, `try`, or nesting.
- Remove encoder-only helpers and imports that have no caller after
  `unionCName` is replaced. Preserve ordinal tag/payload emission until RFC
  0099; it is not part of this sweep.
- Replace `unionCName`'s false comment that calls the current member-C-name
  encoding injective. The replacement comment states the arena reservation and
  canonical-key contract implemented by this RFC; it must not claim that C
  spellings alone distinguish Hexal type identities.

## Non-goals

- **`Ptr<Rune>` versus `Ptr<UInt32>`.** They share the spelling `uint32_t*` and
  always will, because a pointer names no definition — nothing is emitted twice
  and the two are interchangeable at the C level. Hexal has already type-checked
  the distinction. Recorded so the enumeration's third row is not read as an
  open defect.
- Changing `Rune`'s C spelling. See Options.
- **Capitalizing generated type names** to `Hex_t_Int32_Nil`. Raised as a
  convention that a type name should start with a capital. Two facts argue
  against doing it here. Hexal does not require capitalized type names — `type
  foo = { b: Int32, }` compiles — so the convention is a generated-C choice,
  not a reflection of the source. And `hex_` is a single uniform namespace claim
  across every generated identifier: capitalizing only unions would make them
  the odd family out, which is the exact problem this RFC exists to remove.
  Doing it coherently means every generated type name, including the runtime
  templates (`hex_string`, `hex_heap`, `hex_mutex`, `hex_rune_cursor`) and the
  four collection families. That is a wide, purely cosmetic change with a real
  benefit — types become visible at a glance beside `hex_v_` values — and it
  deserves its own spec rather than riding along with a defect fix.
- Renaming Hexal declarations or generated value, member, and function
  identifiers. `hex_v_`, `hex_m_`, and `hex_f_` are unchanged.
- Renaming the other constructed families. `hex_list_`, `hex_array_`,
  `hex_view_`, and `hex_dict_` keep their family prefixes: those encode a
  structure the reader benefits from seeing, whereas a union is just a type and
  the naming table already spells types `hex_t_`.
- Compiler-generated counters (`hex_for_N`, `hex_try_N`, `hex_lit_N`), which are
  already minimal.
- Escaping C keywords in source-derived ADT variant or member identifiers. A
  variant such as `double` already produces invalid C; this RFC neither creates
  nor expands that defect because union tags and payloads retain ordinal
  `member_N` names. It requires a separate language-wide generated-identifier
  decision rather than a union-type naming exception.

## Drawbacks

- Wide manifest movement for a change that alters no behaviour. Unavoidable:
  the name appears in every union construction, narrowing test, and helper, and
  the ADT rename touches every program declaring one.
- ADT names get longer, not shorter. A module owner is the price of two modules
  being allowed to declare the same type name, which they are.
- A name disambiguated by the arena depends on which of two colliding types
  interned first. That is discovery-order dependent, exactly as it already is
  for collections — but it now applies to a family where collisions are more
  reachable, since union member names come from user type names.
- The union name no longer states its members' C spellings, so a reader wanting
  the payload's C type reads the payload union rather than the type name. It is
  adjacent in the same file.
- It replaces an encoder that landed one day earlier, which is churn. The
  alternative is leaving generated C that does not compile.
