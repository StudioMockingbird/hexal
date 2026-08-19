# RFC 0089: Composed Name Encoding

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented
- Created: 2026-08-19
- Scope: stop re-embedding the `hex_` prefix inside composed union C names
- Depends on: nothing
- Coordinates with: `docs/reference.md` (generated-symbol identity), RFC 0039
  (C interop shares this namespace), `docs/status.md`
- Does not change: type identity, name resolution, uniqueness guarantees, or any
  language surface

## Summary

A union's C name is a length-delimited encoding of its members' C names. Because
every compiler-owned member name already starts with `hex_`, that prefix is
re-embedded once per member inside a name that already begins with `hex_union_`.

```c
hex_union_11_hex_t_Error14_hex_task_Int64_tag_member_1     /* 54 chars */
```

`hex_` appears three times. Strip it from the embedded components and the same
identifier carries the same information in 8 fewer characters, before counting
the `_tag`, `_payload`, `_truthy`, `_equal`, and `_tag_member_N` names derived
from it.

## Evidence

Measured over one program using an Array, a `List<String>`, a Task, and a
`try`-propagated Error union:

- **10,863 bytes of `hex_`-prefixed identifiers**, of which **2,684 (25%) are the
  prefix itself**.
- **138 distinct identifiers.** The eight longest are all unions:

```
hex_union_11_hex_t_Error14_hex_task_Int64_tag_member_1     54
hex_union_11_hex_t_Error14_hex_task_Int64_payload          49
hex_union_7_int64_t11_hex_t_Error_tag_member_1             46
```

The encoder (`compiler/types/unions.go:119`):

```go
func unionCName(members []Type) string {
    builder.WriteString("hex_union_")
    for _, member := range members {
        builder.WriteString(strconv.Itoa(len(member.CName)))
        builder.WriteString("_")
        builder.WriteString(member.CName)
    }
}
```

## The change

Strip a leading `hex_` from each member's `CName` before embedding it, and
length-delimit the stripped form:

```
hex_union_11_hex_t_Error14_hex_task_Int64
hex_union_7_t_Error11_task_Int64
```

Nothing else changes. The encoding stays deterministic, stays keyed only on
members, and stays independent of the module that spells the union — so the same
union written anywhere still produces the same C type.

## Why this stays injective

The encoding's injectivity currently rests on the length delimiter. Stripping a
constant prefix preserves it **only if two distinct members cannot map to the
same embedded form**. They cannot, and the argument has two halves:

1. Every member `CName` either begins with `hex_` — user types (`hex_t_*`),
   specializations (`hex_list_*`, `hex_array_*`, `hex_view_*`, `hex_dict_*`,
   `hex_task_*`, `hex_channel_*`), and builtins (`hex_string`, `hex_strand`,
   `hex_heap`, `hex_mutex`, `hex_rune_cursor`, `hex_eos`) — or is a fixed C
   spelling that does not.

   **The non-`hex_` set is wider than the obvious four and the prose here is not
   its authority.** Real unions carry `int8_t` through `int64_t`, `uint8_t`
   through `uint64_t`, `float`, `double`, `bool`, `size_t`, `void`, and
   `nullptr_t` — the last from every nullable member. Derive the set from the
   `types` package, never from this list.
2. Stripping maps the first set to names beginning with a compiler category
   token (`t_`, `list_`, `array_`, `view_`, `dict_`, `task_`, `channel_`,
   `string`, `strand`, `heap`) and leaves the second set untouched. No C scalar
   spelling begins with any of those tokens, so the two result sets are
   disjoint.

**This is an invariant, not an observation, and it must be tested rather than
argued.** A property test over every constructible member type asserting that
the stripped forms are pairwise distinct is the deliverable that makes the change
safe; without it, a future builtin named to collide with a scalar spelling would
silently merge two unions into one C type — the RFC 0073 D19 failure mode.

## Not doing: dropping or shortening the `hex_` prefix

Recorded because it is the obvious next question and the answer is not a
preference.

**`_` is unusable.** C23 §7.1.3 reserves every identifier beginning with an
underscore for use as a file-scope identifier in both the ordinary and tag name
spaces. Generated C is almost entirely file-scope typedefs and `static`
functions, so `_t_Point` and `_v_grid` would be reserved identifiers — undefined
behaviour, against goal 16. This is a conformance question, not a style one.

**`_t` as a type suffix is POSIX-reserved** for future type names. It is widely
used in practice, but adopting it deliberately sits badly beside the rule never
to collide with a standard facility, and it would not even read as a marker:
`hex_union_7_int64_t11_...` already ends in `_t` incidentally.

**`hex_` itself stays.** It is what keeps generated symbols clear of the C
library and of foreign symbols, and RFC 0039 strengthens that: C interop imports
foreign names into this same namespace, so the prefix becomes more valuable, not
less.

## Not doing: dropping the category letter

`hex_v_` / `hex_t_` / `hex_f_` look redundant because Hexal already unifies type
and value names into one namespace, so no program can declare both a type and a
variable called `Point`.

The letter is not separating user names from each other — it separates **user
names from compiler-generated helper names**. Without `t_`, a user type named
`list_Int32` lowers to `hex_list_Int32` and collides with the `List<Int32>`
specialization. That collision is currently impossible by construction, and the
letter is what makes it so.

## Not doing: collapsing sanitized underscore runs

`SanitizeIdentifier` (`compiler/types/generics.go:139`) replaces every character
outside `[A-Za-z0-9_]` with `_`, so `Array<Int32, 3>` becomes `Array_Int32__3_`
and `Task<Int64>` becomes `Task_Int64_`. Collapsing runs and trimming trailing
underscores would read better.

**It also breaks injectivity.** `A<B>` and a user type literally named `A_B`
would both collapse to `A_B`. Collection names have a resolver for exactly this
(`uniqueCollectionCName`), but relying on collision resolution to paper over an
encoding that stopped being injective is the wrong direction — it converts a
structural guarantee into a runtime-of-the-compiler one. Out of scope; if it is
ever wanted, it needs its own injectivity argument.

## Invariants

1. Type identity, canonical keys, and name resolution are unchanged. This is an
   encoding change in one function.
2. The union encoding remains deterministic, injective, member-only, and
   module-independent.
3. Every union member still maps to exactly one embedded form, and distinct
   members to distinct forms.
4. No language surface changes; no program's acceptance changes.
5. Generated C is semantically identical — same types, same members, same
   layout, different spelling.

## Validation

- A property test asserts the stripped embedded forms are pairwise distinct
  across every constructible member type. **Enumerate the scalar spellings from
  the `types` package, not from this spec** — the prose list is illustrative and
  will rot. Cover every builtin, a user type, and one specialization of each
  collection and handle kind.
- The same union written in two modules still produces one C type with one name.
- Two structurally different unions still produce two distinct names, including
  the RFC 0073 D19 shape — `M.Point | Bool` versus `S.Point | Bool`.
- Nested unions encode correctly: a union whose member is itself a union.
- The snippet manifest moves once, for every snippet containing a union — expect
  wide movement, since `try` produces one on every fallible call.
- `go test ./...`, `go vet ./...`.

## Non-goals

- Everything under the four "Not doing" headings above.
- Shortening module encodings (`m1_m`, `m3_app`). They are already minimal and
  carry cross-module uniqueness.
- Changing how any non-union composed name is built.
- Debug or demangling tooling for generated symbols.

## Drawbacks

- The manifest moves widely for a change that alters no behaviour, because `try`
  puts a union in most non-trivial programs. That churn is one-time and worth
  doing before RFC 0039 pins any generated symbol into a binding.
- The injectivity argument becomes something a reader must hold, where today the
  length delimiter alone carries it. The property test is what turns that back
  into something checked rather than reasoned.
