# RFC 0095: Generated C Naming

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-20
- Updated: 2026-08-20
- Scope: make generated union names injective on type identity, and stop
  spending characters that carry no information
- Depends on: nothing. Replaces RFC 0089's encoder entirely — both its basis
  and its length-prefix scheme
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

Both fall out of the same cause: the union encoder is the only one in the
compiler that builds a name from **C spellings** and guarantees uniqueness by
**encoding**. Every other constructed family builds from the **Hexal name** and
guarantees uniqueness by **asking the arena**. Adopting the established
mechanism fixes the defect and shortens the name from 30 characters to 19.

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

## The change

Build the union name the way every other constructed family builds one:

```
hex_union_Int32_Nil              (19, was 30)
hex_union_Rune_Nil               distinct from
hex_union_UInt32_Nil
hex_union_List_Int32__Nil        composed member, sanitized
```

- Join `SanitizeIdentifier(member.Name)` for each member with `_`.
- Pass the result through `uniqueCollectionCName` against `arena.unionTypes`.
- No length prefixes. Uniqueness comes from the registry, not the encoding.

`Int32_Nil_Foo` is reachable from both `Int32 | Nil | Foo` and a two-member
union whose first member is a user type named `Int32_Nil`. That is exactly the
case `uniqueCollectionCName` exists to resolve, and it resolves it the same way
it already does for `List<T>`: the second type to intern gets the module owner
appended, then a counter if that is still taken.

**And the tag suffix loses 10 characters it does not need.** `_tag_member_N`
exists only to keep an enum constant unique in the ordinary identifier
namespace; `_mN` does that identically:

```c
hex_union_Int32_Nil_m1           (22, was 43)
```

## Options considered

| Option | `Int32 \| Nil` | Fixes the defect | Verdict |
|---|---|---|---|
| **Hexal names + arena uniqueness** | `hex_union_Int32_Nil` (19) | yes | **taken** |
| Hexal names + length prefixes | `hex_union_5_Int32_3_Nil` (22) | yes | rejected — encodes what the registry can check |
| Compilation-wide counter, `hex_t_1` | `hex_t_1` (7) | yes | rejected — see below |
| Give `Rune` its own C spelling | 30, unchanged | yes, at the root | deferred — see below |
| Tag suffix `_tag_member_N` → `_mN` | tag 43 → 33 | no | **taken**, orthogonal |
| Drop the tag enum, use integer literals | `.tag = 0` | no | rejected |
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

**Dropping the tag enum** trades a real readability property for characters:
`tag != 1` does not say what 1 is.

**A module-local alias** puts two names on one type in output meant to be read
directly, and the reader must chase the alias to learn anything.

## Invariants

1. Union representation, tag ordering, payload layout, and `sizeof` are
   unchanged. This RFC changes spellings only.
2. Two distinct Hexal types never share a generated C name that keys a
   definition. `Ptr` is exempt because it names no definition.
3. Uniqueness is established by the shared arena, not by the encoding. The
   member list need not be recoverable from the name.
4. One interned type has one name across every module of a compilation, because
   every module reads it from the shared arena.
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
- `Int32 | Nil` generates `hex_union_Int32_Nil` with tag constants
  `hex_union_Int32_Nil_m0` and `_m1`.
- A union whose member is a composed type — `List<Int32> | Nil` — generates a
  valid C identifier containing no `<`, `>`, `,`, `|`, or space.
- Two distinct unions whose sanitized member names would produce one string
  both compile, receive distinct names, and each is defined once.
- The same union written in two modules still produces one C type with one
  name, and a union crossing a module boundary as a function result is spelled
  identically in both headers.
- Two structurally different unions still produce two names, including
  `M.Point | Bool` versus `S.Point | Bool`.
- Nested and widened unions, union equality helpers, truthiness helpers, and
  `try` results all name the same type consistently after the change.
- Two compilations of the same sources produce byte-identical files.
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
- Shortening `hex_union_` itself, or the `hex_` namespace claim. The family
  prefix is what makes `hex_list_`, `hex_array_`, `hex_view_`, and `hex_dict_`
  legible, and unions should match rather than move to a bare `hex_t_`.
- Compiler-generated counters (`hex_for_N`, `hex_try_N`, `hex_lit_N`), which are
  already minimal.

## Drawbacks

- Wide manifest movement for a change that alters no behaviour. Unavoidable:
  the name appears in every union construction, narrowing test, and helper.
- A name disambiguated by the arena depends on which of two colliding types
  interned first. That is discovery-order dependent, exactly as it already is
  for collections — but it now applies to a family where collisions are more
  reachable, since union member names come from user type names.
- The union name no longer states its members' C spellings, so a reader wanting
  the payload's C type reads the payload union rather than the type name. It is
  adjacent in the same file.
- It replaces an encoder that landed one day earlier, which is churn. The
  alternative is leaving generated C that does not compile.
