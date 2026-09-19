# RFC 0218: `let` Declarations and `=` Binding

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented
- Created: 2026-09-18
- Updated: 2026-09-19
- Scope: replace the `:=` value-binding operator with a mandatory `let`
  declaration introducer plus `=`, so a declaration and an assignment are
  distinguished by a keyword rather than by an operator
- Coordinates with: the lexer `:`/`=` tokens, the parser declaration grammar,
  declaration-inference diagnostics, `docs/reference.md` grammar and prose, the
  workbench snippet catalog, and every fixture source
- Does not change: type syntax, parameters, record members, ADT payloads,
  results, `for` binders, `self`, named constructor arguments, `extern`
  declarations, language semantics, generated C, or the string-in/string-out
  compiler boundary

## Summary

A value binding is introduced by `let`:

```hexal
let count: Int32 = 0
let name: String = "hexal"
let mut total: Int32 = 0
```

`=` alone assigns to an existing writable place, exactly as today:

```hexal
total = total + 1
```

The `:=` token is removed. `:` and `=` are independent tokens, and `let` is the
only way to introduce a value binding.

## Motivation

- `:=` exists for one reason: to distinguish `x := e` (a declaration) from
  `x = e` (an assignment). A mandatory `let` carries that distinction
  unambiguously and lets the ordinary `=` return for both reading and writing.
- `let` reads consistently with `mut` and states intent in words:
  `let mut total = 0` introduces a mutable binding; `total = ...` mutates it.
- It removes a non-alphabetic two-character operator whose only job is
  declaration disambiguation, advancing the language goals of one obvious way
  and a small, clean surface.
- It restores the pre-`:=` spelling (`=`) while keeping the declaration/assignment
  distinction that motivated `:=`, without reintroducing the ambiguity that
  caused `=` to be replaced.

## Syntax

Grammar productions that change:

```ebnf
Declaration = "let" [ "mut" ] identifier
              ( ":" TypeExpression "=" | "=" ) Expression .

Assignment = AssignmentTarget "=" Expression .
```

- `let` is added to `reserved-word`.
- `:` and `=` are independent tokens; no adjacency rule remains.
- The former `ColonEqual` (`:=`) token is deleted from the lexer.
- Parameter, member, ADT-payload, result, `for`-binder, and `extern` grammars
  are unchanged: none uses `let` and none uses `=` for introduction.

Examples:

```hexal
let typed: Int32 = 13
let inferred = Heap()
let mut counter: Int32 = 0
```

## Declaration semantics

- A declaration states its type exactly once, on one side or the other.
  `let name: T = initializer` states it on the left; `let name = initializer`
  takes it from the initializer and is rejected when the initializer is
  contextual (an integer, float, or string literal, `nil`, an array literal, or
  a `match` whose every arm is contextual), exactly as `:=` is today.
- A declaration without an initializer is rejected: `let name: T` is an error,
  just as `name: T :=` is today.
- `mut` appears only immediately after `let`: `let mut name = ...` declares a
  replaceable binding; `let name = ...` declares a fixed binding.
- Visibility, shadowing, protected-name rules, source-order type visibility, and
  storage semantics are unchanged by this RFC. RFC 0219 separately classifies
  ordinary top-level `let` declarations by entry/imported module role.

## Assignment semantics

- `place = expression` assigns and never declares. There is no bare `=`
  declaration form.
- An assignment whose target has no binding is the ordinary
  unknown-name/assignment diagnostic, never an implicit declaration:

```hexal
x = 5        (* rejected: unknown variable x; use 'let x = 5' to declare *)
```

- Named constructor arguments (`name = value`), module aliases, and the `=`
  inside a struct or ADT-variant call are unchanged.

## Diagnostics

| Condition | Required diagnostic |
| --- | --- |
| A statement introduces a binding without `let` | Syntax Error: `declarations require 'let'` |
| `let` binding has no initializer | Syntax Error: `expected '=' in a 'let' declaration` |
| `let name = initializer` with a contextual initializer | Type Error: `` `let` requires an initializer whose type does not depend on context; annotate the binding instead `` |
| `:=` used as a declaration operator | Syntax Error: `':=' is not a declaration operator; use 'let name = value'` |
| `let name := initializer` | Syntax Error: `expected a type after ':' in a 'let' declaration` |
| `mut` outside `let mut` in a declaration | Syntax Error: `'mut' appears only immediately after 'let' in a declaration` |
| Assignment to a fixed binding or undeclared name | existing diagnostics, unchanged |

The `:=` diagnostic is token-sequence based. Source adjacency is irrelevant:
`name:=value` and `name : = value` receive the same diagnostic. The same
diagnostic applies to the typed former declaration `name: T := value`. In a
declaration already introduced by `let`, `:` begins a type annotation, so
`let name:=value` and `let name : = value` instead receive the missing-type
diagnostic above. `let name mut = value` receives the malformed-`mut`
diagnostic above.

## C23 lowering

None. This is a surface-syntax change: the parsed declaration and assignment
trees are unchanged, so generated C is byte-identical for every program that
compiles before and after. No runtime, ABI, or component change results.

## Implementation plan

### Phase 1: lexer

1. Delete the `ColonEqual` token kind, its name mapping, and its scanning case;
   `:` and `=` scan independently.
2. Update the lexer tests that assert `:=` is one token; add a test that `:=`
   now lexes as `:` then `=`.

### Phase 2: parser

1. Add `let` as a reserved word and a keyword token.
2. Parse `Declaration = "let" [ "mut" ] identifier ( ":" TypeExpression "=" | "=" ) Expression`.
3. Replace the recovery and rejection diagnostics above; retain `:=` only in
   the required rejection diagnostic and its negative tests.
4. Replace `parser.Declaration.Operator` with `Keyword`, storing the source
   `let` token. The checker uses that token as the diagnostic anchor for
   contextual inferred initializers; the `=` token has no semantic role.

### Phase 3: checker

1. Reword the contextual-inference diagnostic from `` `:=` `` to `` `let` ``;
   the rule itself is unchanged.
2. Confirm no semantic path inspects `=` or the former `:=` token; none should.

### Phase 4: sources, catalog, and tests

1. Rewrite every positive `.hex` source that uses `:=`: workbench snippets,
   integration fixtures, packages, standard-library sources, driver fixtures,
   and doctest-like source in active specs. Archived specs are immutable and are
   not rewritten.
2. Update diagnostic assertions that treat `:=` as valid. Retain only the
   negative sources and assertions required by the diagnostics above.
3. Compile the positive fixture corpus immediately before and after its
   syntax-only rewrite and compare every `CompilationResult.Files` entry. The
   snippet SHA manifest must not move. Any difference is a defect to investigate;
   do not rebuild the manifest for RFC 0218.

### Phase 5: documentation

1. Update `docs/reference.md`: the grammar productions above, the
   declaration-operator prose and every `:=` example. RFC 0219 owns removal of
   the separate `static` production and keyword.
2. Apply the same productions to the checked root `GRAMMAR.ebnf`. The reference
   remains the sole normative contract; the root grammar is its checked mirror.
3. Record the change on `docs/status.md` while the spec is active; archive the
   spec when every Validation item passes.

## Required implementation sweep

- lexer token kinds, names, scanning, and tests;
- parser declaration, recovery, and every `:=` diagnostic;
- checker contextual-inference diagnostic and any `:=` mention;
- all `.hex` sources in `workbench/snippets`, `compiler/tests/integration`,
  `compiler/tests/c23validation`, `internal/driver`, `packages/`, and `stdlib/`
  that use `:=` as a declaration;
- generated-C text assertions that embed a source string;
- `docs/reference.md` grammar and prose, plus the checked root `GRAMMAR.ebnf`
  mirror.

## Reference synchronization

`docs/reference.md` is the sole normative source. This spec changes the
`declaration` production, adds `let` to `reserved-word`, restates the
declaration-operator rule, and replaces every `:=` example. RFC 0219 separately
removes the `static-module-value` production. No semantic rule changes here.

## Validation (exhaustive)

1. `let name: T = expr`, `let name = expr`, and `let mut name = expr` each parse
   and check in every position where the corresponding ordinary pre-RFC
   declaration was valid. Former `static` declarations remain RFC 0219's scope.
2. `name := expr`, `name : = expr`, and `name: T := expr` report `':=' is not a
   declaration operator; use 'let name = value'`. `let name := expr` and its
   spaced form report `expected a type after ':' in a 'let' declaration`.
3. `name: T = expr` with no `let` reports `declarations require 'let'`.
4. `let name: T` with no initializer reports `expected '=' in a 'let' declaration`.
5. `let name = <contextual>` is rejected with the `` `let` `` inference
   diagnostic; the equivalent annotated form is accepted.
6. `name = expr` with no prior binding is rejected as an unknown name and never
   declares; `let name = expr` followed by `name = expr` is accepted when the
   binding is `mut` and rejected when it is fixed.
7. `mut` before `let` (`mut let x = 1`) is rejected; `let mut x = 1` is accepted.
   `let x mut = 1` reports `'mut' appears only immediately after 'let' in a
   declaration`.
8. `let` cannot be used as an identifier; a program that used `let` as a name now
   fails as a reserved-word use.
9. Parameters, members, ADT payloads, results, `for` binders, `self`, named
   constructor arguments, and `extern` declarations are unchanged and still
   parse.
10. Every rewritten positive regression program has byte-identical
    `CompilationResult.Files` before and after the source rewrite; the snippet
    SHA manifest does not move. If it moves, the change altered output and is
    wrong.
11. `go test ./...`, `go vet ./...`, `go vet -tags c23 ./...`, and the complete
    tagged C23 suite pass with only Clang installed.
12. The workbench snippet catalog, integration fixtures, and tagged fixtures
    that use declarations all still compile, run, and produce their exact
    expected output after the rewrite.
13. `gofmt -l` is clean on every changed Go file. No positive source or
    assertion accepts `:=`; negative diagnostic coverage remains.

## Non-goals

- Introducing `var`, `const`, `let ... else`, destructuring, or multiple
  binders in one declaration.
- Optional initializers, delayed initialization, or default values.
- Changing parameters, members, ADT payloads, results, `for` binders, `self`,
  named constructor arguments, or `extern` declarations.
- Any change to semantics, generated C, or the runtime ABI.
- Keeping `:=` as a deprecated alias.

## Decisions

The three surface choices are settled and are the ones the grammar and
diagnostics above specify:

1. `mut` placement is `let mut x`, not `mut let x`. `mut` modifies the binding,
   so it follows the introducer that creates the binding.
2. RFC 0218 adds no separate module-value spelling. RFC 0219 owns top-level
   binding classification and removal of `static`.
3. `:=` is a hard error, not a deprecated alias. Its one diagnostic names the
   replacement directly, so the language has exactly one declaration spelling.

## Consequences

- Every existing `.hex` source and the snippet catalog must be rewritten; this
  is an intentional breaking surface change.
- `let` becomes reserved, so a program using `let` as an identifier breaks.
- Declarations and assignments become distinguishable by keyword, and `=`
  regains a single meaning in both directions.
