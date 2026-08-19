# RFC 0090: Inferred Binding Declarations

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented. The narrow rule is decided: a declaration
  must state its type on one side or the other, and stating it on neither is an
  error.
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

**The governing rule is unchanged from today: a declaration must state its type
exactly once.** What changes is *which side* may state it. Today it is always
the left. With `:=` the right may state it instead, when the right already does.

If **neither** side states the type, that is an error — the same error it is
today, arriving from the other direction. `total := 0` is rejected because
nothing in the declaration says whether it is an Int8 or an Int64.

So this is not a second way to declare. It is one requirement — the type appears
once — with the annotation becoming redundant exactly where the initializer
already carries it.

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

**A declaration still states its type exactly once.** `:=` moves which side
may state it, and rejects a declaration where neither does. That is the same
requirement the language has today, not a relaxation of it.

**Hexal already infers.** `docs/reference.md:279` names `self` and `for` binders
as compiler-typed exceptions. Inference where the type is unambiguous is
already the language's position; this extends the existing principle rather
than introducing one.

**The defaults already exist** — `:412`, integer literals to Int32 and floats to
Float64 — so no new *inference* is required. But they are also why the one new
piece of machinery is needed: the defaults make a bare literal succeed where it
should fail, so something must reject it before the defaults apply. See
Mechanism.

**`:=` was never removed.** `AGENTS.md` records it as "syntax the language never
had", proposed in RFCs 0016, 0017, 0029, and 0036 and dropped. This revisits a
rejected proposal, not an implemented decision.

## The rule

**A binding declaration must determine its type. `:=` says the initializer
does; an annotation says the left side does. Neither is an error.**

`name := initializer` declares a binding whose type is the initializer's. It is
rejected when the initializer is a *contextual* form — one that takes its type
from its surroundings and so has none of its own.

`docs/reference.md:416` already draws that line: *"Expected types reach untyped
literals transitively through arithmetic and never retype a typed value."* A
contextual form is one that expected types reach; everything else determines
itself.

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

## Mechanism — a syntactic predicate, not a checker flag

**An earlier revision of this RFC claimed no new machinery was needed. That was
wrong**, and the correction matters because it is the whole feasibility question.

`checkInitializer` threads the *expected* type **into** the expression; the
annotation is the context, not something compared afterwards. Remove it and the
checker runs with no expected type — and with no context `reference.md:412` says
an integer literal **defaults to Int32**. So `total := 0` would not fail. It
would quietly succeed as Int32, which is the exact outcome this RFC exists to
prevent.

Rejecting it needs the compiler to distinguish *determined* from *defaulted*.
Nothing records that today: `checkedExpression` carries ten fields and none is
an untyped marker.

**The predicate already exists.** `isContextualExpression`
(`compiler/checker/unions.go:11`) decides which forms a contextual union
injection may reach, and it is the same question in a different setting:

```go
case parser.IntegerLiteral, parser.DecimalLiteral, parser.NegatedNumericLiteral:
    return true
case parser.UnaryExpression:
    return expression.Operator.Lexeme == "-" || isContextualExpression(expression.Operand)
case parser.BinaryExpression:
    return isArithmeticToken(...) && isContextual(Left) && isContextual(Right)
default:
    return false
```

**Extend it rather than write a second one.** Two predicates answering "does
this form take its type from context" would drift apart, and the drift would be
silent. Four cases are missing for `:=`:

- **string literals** — `"hexal"` is valid as String and as Strand;
- **`nil`** — requires a contextual union containing Nil;
- **array literals** — need an element type and a length;
- **`match`** — see below, and it is the one that matters.

Everything else — a constructor, a named object literal, a call, a binding read,
a member or index read, `try`, `spawn` — determines itself. The existing
arithmetic recursion is what makes `1 + 2` reject and `count + 1` accept, and it
needs no type information: one operand being a name is enough.

### `match` is the silent-acceptance case

A `match` used as an initializer forwards the expected type into every arm
(`checker/adt.go:548` passes `context.expected` to each arm's expression), so
its arms are typed by the annotation, not by themselves. Verified:

```
x: Int64 = match ready | true then 1 | false then 0 end   →  const int64_t
x: Int32 = match ready | true then 1 | false then 0 end   →  const int32_t
```

Identical source, two types, decided entirely by the annotation. Remove the
annotation and the arms default to Int32 — silently, and with `match` absent
from the predicate the declaration would be **accepted**.

That is the exact failure this RFC exists to prevent, and it arrives in the
direction the Drawbacks section claimed was impossible.

**A `match` is contextual when every arm's expression is contextual.** The
scrutinee is irrelevant — it has its own type and never receives the expected
one. This keeps the predicate purely syntactic and matches the recursion already
there for arithmetic.

The general rule, which is what makes the extension checkable rather than a
list: **a form is contextual if it forwards the expected type to
sub-expressions and all of those are contextual.** Any future construct that
threads `context.expected` inward must be added here in the same change. Grep
for `context.expected` to enumerate them.

This is preferable to threading an `untyped` flag through the checker. It is
local, it runs before any type work, and it cannot drift out of agreement with
the type rules because it never consults them — it only asks whether the source
named anything.

The predicate is the one new piece of machinery **in the checker**. There is
also lexer and parser work, below.

### `:=` is not a token today, and that is a decision

The lexer emits a bare `Colon` and advances one character
(`compiler/lexer/lexer.go`); there is no `:=` lexeme and no lookahead for `=`.
The parser's declaration path then requires a type after the colon
(`parser/statements.go`). So `x := 5` currently lexes as
`Ident Colon Equal Int` and fails in the parser.

Two ways to accept it, and they differ in what else they accept:

**Lex `:=` as one token.** `x : = 5` — colon, space, equals — stays a syntax
error, because the lexer only produces the token when the characters are
adjacent. This is what most languages do, and for the same reason.

**Parse `Colon` followed immediately by `Equal`.** No lexer change, but
whitespace between them becomes invisible, so `x : = 5` is silently accepted as
a declaration. That is a second spelling of the feature, arriving by accident
rather than design — precisely what the Summary argues this RFC avoids.

**Take the token.** It costs a lexeme and a case in the lexer's colon branch,
and it keeps the surface to exactly one spelling. Note that the existing branch
already has the shape to copy: `!` looks ahead for `=` two cases below.

An earlier revision of this section called the checker predicate "the one new
piece of machinery." That was wrong — it is the only *subtle* piece.

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
- `x : = 5` — colon, space, equals — is a syntax error, not a declaration. This
  is the test that distinguishes the token from the lookahead: it passes under
  one and fails under the other.
- `sum := count + 1` with `count: Int32` is accepted and yields Int32.
- **`x := match ready | true then 1 | false then 0 end` is rejected**, and the
  same match with *every* arm typed and agreeing is accepted. This is the
  silent-acceptance case; without it the predicate passes its other tests while
  defaulting to Int32 here.

  Corrected while implementing: this bullet originally said "with a typed arm",
  singular. One typed arm beside a bare literal is also rejected — with no
  expected type the literal defaults to Int32 and disagrees with the other arm,
  so `match arm result types do not agree` fires instead of the `:=`
  diagnostic. Both are rejections and the second is more precise, but the
  original wording claimed acceptance.
- Every form that threads `context.expected` inward is covered. Derive the list
  by grepping `context.expected` in `compiler/checker/` rather than trusting the
  enumeration in Mechanism — a form added later must fail this test, not slip
  through it.
- The union-injection path that already uses `isContextualExpression` keeps its
  existing behaviour: extending the predicate must not change which contextual
  unions are accepted today.
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
- The predicate must stay in step with every form that threads the expected type
  inward, and nothing enforces that mechanically. An omission is **not**
  symmetric: a missing contextual case is accepted and silently defaulted, which
  is how `match` was nearly shipped. An earlier revision of this section claimed
  the predicate could only over-reject; that was wrong. Extending the existing
  `isContextualExpression` rather than adding a second predicate is the
  mitigation, since the union path exercises the same function.
- It reverses a documented instruction in `AGENTS.md`, which is a cost to the
  credibility of such instructions. Reversing it explicitly and narrowly, rather
  than quietly, is the mitigation.
