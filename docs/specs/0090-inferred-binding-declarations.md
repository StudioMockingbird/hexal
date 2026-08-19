# RFC 0090: Inferred Binding Declarations

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design decision required
- Created: 2026-08-19
- Scope: `name := initializer` — omitting a binding's type annotation exactly
  when the initializer already determines it
- Depends on: nothing
- Coordinates with: `docs/reference.md`, `AGENTS.md` (which currently forbids
  reintroducing `:=`), `docs/status.md`
- Does not change: type identity, literal defaulting, `mut`, parameters,
  members, or generated C

## Summary

Twenty-one percent of binding declarations in the snippet catalog spell their
type twice:

```hexal
names: Dict<Int32, Strand> = Dict<Int32, Strand>.new(h)
values: List<Int32> = List<Int32>.new(h)
entry: Entry = Entry { x = 1, y = 2 }
```

Introduce `:=`, which omits the annotation **only when the initializer has a
type independent of context**. A bare literal keeps its annotation, because in a
language with eight integer widths the annotation is the only thing pinning the
width.

## Evidence

252 binding declarations across the 98-snippet catalog:

| Category | Count | The annotation is |
|---|---|---|
| `T = T { … }` object literals | 32 | **redundant** — the type is spelled twice |
| `T = T.new(…)` and other constructors | 22 | **redundant** — spelled twice |
| `Int32 = 5`, `String = "…"` and other literals | 55 | **load-bearing** — the only statement of the type |
| function results, expressions, reads | ~143 | determined, but not textually duplicated |

The redundant 54 are the concentrated cost: `Dict<Int32, Strand>` twice is
forty-four characters where twenty-two carry the same information.

## Three facts that make this a smaller change than it appears

**Hexal already infers.** `docs/reference.md:279` names `self` and `for` binders
as compiler-typed exceptions. Inference where the type is unambiguous is
already the language's position; this extends the existing principle rather
than introducing one.

**The defaults already exist.** `:412` — integer literals default to Int32 and
floats to Float64 without context. No new inference machinery is required.

**`:=` was never removed.** `AGENTS.md` records it as "syntax the language never
had", proposed in RFCs 0016, 0017, 0029, and 0036 and dropped. This revisits a
rejected proposal, not an implemented decision.

## The rule

`name := initializer` declares a binding whose type is the initializer's type.
It is accepted **only when the initializer is a typed value**, and rejected when
the initializer is an untyped literal or a contextual form.

The distinction is not new. `docs/reference.md:416` already separates the two:
*"Expected types reach untyped literals transitively through arithmetic and
never retype a typed value."* `:=` requires the right-hand side of that
sentence.

### Accepted — the initializer determines its own type

```hexal
values := List<Int32>.new(h)         -- constructor
entry  := Entry { x = 1, y = 2 }     -- named object literal
total  := compute()                  -- declared function result
copy   := existing                   -- another typed binding
first  := cursor.next()              -- method result
```

### Rejected — the initializer is contextual

```hexal
total := 0                -- integer literal: Int8 through Int64 all plausible
ratio := 1.5              -- float literal
label := "hexal"          -- String or Strand, both valid
empty := nil              -- requires a contextual union containing Nil
grid  := [1, 2, 3]        -- array literal needs an element type and a length
sum   := 1 + 2            -- arithmetic over untyped literals stays untyped
```

Diagnostic:

> `:=` requires an initializer whose type does not depend on context; annotate
> the binding instead

`sum := count + 1` where `count: Int32` **is** accepted: an untyped literal
combined with a typed value yields a typed value, per the same rule at `:416`.

### `mut` is orthogonal

```hexal
mut total := compute()
```

`mut` governs rebinding, not the annotation. Both forms accept it.

## Why literals are excluded

`total := 0` would silently produce Int32. In Go that is harmless — there is one
`int`. Hexal has eight integer widths, and an accidental Int32 where Int64 was
meant is a real defect class that the annotation currently prevents outright.

The 55 literal-initialized declarations measured above are exactly the ones where
the annotation carries information found nowhere else. Inference would weaken
precisely the declarations that need it most, to save the least typing.

This is also what keeps the feature from being a second way to declare: the
annotation becomes optional **exactly where it carries no information**, which
is the same principle that already makes `self` and `for` binders implicit.

## Rejected alternative — blanket `:=`

Allow `x := 0`, taking the documented Int32 default.

Rejected on width safety, above. Its one advantage is uniformity — no rule to
learn about which initializers qualify — and that is real. But the failure it
permits is silent and typed, which is the worst combination: the program
compiles, runs, and is wrong at a width boundary.

If this RFC is adopted and the accepted/rejected split proves annoying in
practice, widening it later is additive and breaks nothing. Narrowing a blanket
rule later would break programs.

## Documentation changes

`docs/reference.md:279` currently reads:

> Every binding and written parameter has an explicit type. Compiler-typed
> `self` and `for` binders are the exceptions; `:=` does not exist.

becomes a statement of the rule, with `:=` listed beside the existing
exceptions.

`AGENTS.md` currently instructs that `:=` must not be reintroduced, naming the
closed RFCs that proposed it. That instruction is reversed **only** for the
narrow form specified here; the note should say so explicitly, so the closed
specs stay superseded rather than becoming authority again. Those RFCs proposed
blanket inference, which this RFC rejects.

Both edits land with the implementation.

## Invariants

1. Parameters, object members, ADT payloads, and function results still require
   explicit types. This RFC changes binding declarations only.
2. Literal defaulting is unchanged. `:=` does not consult the Int32/Float64
   defaults; it rejects the cases that would need them.
3. Generated C is byte-identical for every existing program, and for any program
   that uses `:=`, identical to the annotated form it replaces.
4. `mut`, shadowing rules, and name resolution are unchanged.
5. No program that compiles today stops compiling.

## Validation

- Each accepted form above declares a binding of the expected type, verified by
  a subsequent use that would fail under any other type.
- Each rejected form produces the diagnostic above, at the `:=` token.
- `sum := count + 1` with `count: Int32` is accepted and yields Int32.
- `mut total := compute()` is accepted and rebindable; `total := compute()`
  followed by assignment is rejected as a constant.
- A `:=` binding and its annotated equivalent generate identical C.
- The snippet manifest does not move: this RFC adds a form and rewrites no
  existing snippet.
- `go test ./...`, `go vet ./...`.

## Non-goals

- Inference for parameters, results, members, or ADT payloads.
- Changing literal defaulting, or removing the Int32/Float64 defaults.
- `var`/`let` keywords, or any other declaration spelling.
- Rewriting the catalog to use `:=`. Adopting it in existing snippets is a
  separate, optional pass — and a good way to judge whether the split is drawn
  in the right place before it is documented as final.

## Drawbacks

- **Two spellings exist for the ~143 declarations that are inferable but not
  textually redundant.** `x: Int32 = compute()` and `x := compute()` are both
  legal and nothing chooses between them. That is a genuine tension with goal 2
  and no framing dissolves it; the bet is that removing the doubled generics is
  worth it.
- An author must learn which initializers qualify. The diagnostic teaches it at
  the point of failure, but it is a rule where there was none.
- It reverses a documented instruction in `AGENTS.md`, which is a cost to the
  credibility of such instructions. Reversing it explicitly and narrowly, rather
  than quietly, is the mitigation.
