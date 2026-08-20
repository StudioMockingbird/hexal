# RFC 0095: Generated C Naming

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-20
- Scope: make generated union names injective on type identity, and stop
  spending characters that carry no information
- Depends on: nothing. Supersedes RFC 0089's encoder basis, not its
  length-prefix scheme
- Coordinates with: `docs/reference.md`, `docs/status.md`, the snippet manifest
- Does not change: union representation, tag layout, payload layout, the
  `hex_` prefix scheme, or any language surface

## Summary

Two problems in one identifier, found from one report that union names read as
noise:

```c
hex_union_7_int32_t9_nullptr_t_tag_member_1
```

**It is too long**, and 13 of its 43 characters carry no information.

**It is also not injective on type identity, which is a defect.** `Rune | Nil`
and `UInt32 | Nil` are distinct Hexal types that produce the same C name,
because `Rune` and `UInt32` share the C spelling `uint32_t`. A program using
both emits the struct twice into one header, which does not compile.

Both fall out of the same cause: the union encoder names its members by their
**C spelling**, while every other generated family names them by their **Hexal
name**. Switching unions to the established basis fixes the defect and shortens
the name at once.

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

## Evidence — unions are the only family that does this

```
hex_list_Rune       hex_list_UInt32        distinct
hex_view_Rune       hex_view_UInt32        distinct
hex_array_Rune_2    hex_array_UInt32_2     distinct
hex_dict_Int32_Rune hex_dict_Int32_UInt32  distinct
hex_union_8_uint32_t9_nullptr_t            both
```

Collections build their name from `SanitizeIdentifier(element.Name)` — the
Hexal name — and pass it through `uniqueCollectionCName`, which disambiguates
with the module owner and then a counter (`types/collections.go`,
`types/arena.go`). `unionCName` uses `member.CName` and neither helper.

Hexal names are distinct per type identity by construction, which is why the
other four families have no collision and unions do.

## The change

`unionCName` composes the member's **Hexal name**, sanitized, keeping RFC
0089's length prefix:

```
hex_union_5_Int32_3_Nil          (22, was 30)
hex_union_4_Rune_3_Nil           distinct from
hex_union_6_UInt32_3_Nil
```

The length prefix stays. List and Array can omit it because their arity is
fixed; a union is variadic, so the member boundary must be recoverable from
the name, and `_` is the only joiner legal in a C identifier. This is the same
reason the Itanium C++ ABI length-prefixes its components.

Composed members keep using their `CName`, because a composed type's `Name`
contains `<`, `>`, `,`, `|`, and spaces. `SanitizeIdentifier` already maps
those to `_`, so the rule is uniform: sanitize `Name` when it is a scalar or
nominal type, otherwise embed the member's `CName` with `hex_` stripped as
today. Both branches are length-prefixed.

**And the tag suffix loses 10 characters it does not need.** `_tag_member_N`
exists only to keep an enum constant unique in the ordinary identifier
namespace; `_mN` does that identically:

```c
hex_union_5_Int32_3_Nil_m1       (26, was 43)
```

## Options considered

| Option | Result for `Int32 \| Nil` | Fixes the defect | Verdict |
|---|---|---|---|
| **Hexal names, keep length prefix** | `hex_union_5_Int32_3_Nil` | yes | **taken** |
| Give `Rune` its own C spelling | name unchanged at 30 | yes, at the root | rejected — see below |
| Tag suffix `_tag_member_N` → `_mN` | tag 43 → 33 | no | **taken**, orthogonal |
| Drop the tag enum, use integer literals | `.tag = 0`, `tag != 1` | no | rejected |
| Module-local `typedef … hex_u1;` alias | short at every use | no | rejected |
| Drop the length prefix for builtins only | `hex_union_Int32_Nil` | partly | rejected |

**Giving `Rune` a distinct C spelling** — a `hex_rune` typedef over `uint32_t` —
is the more principled root fix and would close all three collisions at once.
Rejected for now because the only harmful one is unions, it changes the spelling
of every `Rune` in every generated program, and `uint32_t` is an honest
rendering of a Rune's representation. Recorded because the question recurs; it
remains available and additive if a second harmful consequence appears.

**Dropping the tag enum** trades a real readability property for characters:
`tag != 1` does not say what 1 is, and `reference.md` requires generated C to
stay as plain as the compiler source.

**A module-local alias** puts two names on one type in output meant to be read
directly, and the reader must chase the alias to learn anything.

**Dropping the length prefix for the closed builtin set** works only while the
vocabulary stays prefix-free, and mixing prefixed and unprefixed segments makes
the injectivity argument much harder to state than the saving is worth.

## Invariants

1. Union representation, tag ordering, payload layout, and `sizeof` are
   unchanged. This RFC changes spellings only.
2. Two distinct Hexal types never share a generated C name that keys a
   definition. `Ptr` is exempt because it names no definition.
3. The encoding stays injective: the member list is recoverable from the name.
4. Names remain content-addressed. No hash, no truncation, no global counter —
   `reference.md` forbids the first two and the third would renumber one
   module's types when another module changes.
5. No language surface changes; no program's acceptance changes.

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
- `Int32 | Nil` generates `hex_union_5_Int32_3_Nil` with tag constants
  `hex_union_5_Int32_3_Nil_m0` and `_m1`.
- A union whose member is a composed type — `List<Int32> | Nil` — generates a
  valid C identifier with a recoverable member boundary.
- The same union written in two modules still produces one C type with one
  name; two structurally different unions still produce two names, including
  `M.Point | Bool` versus `S.Point | Bool`.
- Nested and widened unions, union equality helpers, truthiness helpers, and
  `try` results all name the same type consistently after the change.
- `docs/reference.md` records that generated names are injective on type
  identity and that union members are named by Hexal name.
- The snippet manifest moves for every snippet containing a union — expect wide
  movement, since `try` produces one on most non-trivial programs — and for no
  snippet without one.
- `go test ./...`, `go vet ./...`.

## Non-goals

- **`Ptr<Rune>` versus `Ptr<UInt32>`.** They share the spelling `uint32_t*` and
  always will, because a pointer names no definition — nothing is emitted twice
  and the two are interchangeable at the C level. Hexal has already type-checked
  the distinction. Recorded so the enumeration's third row is not read as an
  open defect.
- Changing `Rune`'s C spelling. See Options.
- Renaming bindings, members, functions, or types. `hex_v_`, `hex_m_`, `hex_f_`,
  and `hex_t_` are unchanged: they are one prefix on a source spelling and carry
  no redundancy.
- Shortening `hex_union_` itself, or the `hex_` namespace claim.
- Compiler-generated counters (`hex_for_N`, `hex_try_N`, `hex_lit_N`), which are
  already minimal.

## Drawbacks

- Wide manifest movement for a change that alters no behaviour. Unavoidable:
  the name appears in every union construction, narrowing test, and helper.
- The union name no longer states its members' C spellings, so a reader wanting
  the payload's C type reads the payload union rather than the type name. The
  payload union is adjacent in the same file.
- It supersedes part of a spec closed one day earlier, which is churn. The
  alternative is leaving generated C that does not compile.
