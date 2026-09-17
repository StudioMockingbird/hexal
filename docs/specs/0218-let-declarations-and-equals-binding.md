# RFC 0218: `let` Declarations and `=` Binding

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; not scheduled
- Created: 2026-09-18
- Updated: 2026-09-18
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
let name = "hexal"
let mut total = 0
static let limit = 10
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
declaration = "let" , [ "mut" ] , identifier
              , ( ":" , type-expression , "=" | "=" ) , expression ;

static-module-value = "static" , "let" , [ "mut" ] , identifier
                      , ( ":" , type-expression , "=" | "=" ) , expression ;

assignment = assignment-target , "=" , expression ;   (* unchanged *)

statement = non-control-statement | return-statement
            | if-statement | while-statement | for-statement ;
non-control-statement = declaration | assignment | call-statement
                        | try-statement | "break" | "continue"
                        | defer-statement | errdefer-statement ;
```

- `let` is added to `reserved-word`.
- `:` and `=` are independent tokens; no adjacency rule remains.
- The former `ColonEqual` (`:=`) token is deleted from the lexer.
- Parameter, member, ADT-payload, result, `for`-binder, and `extern` grammars
  are unchanged: none uses `let` and none uses `=` for introduction.

Examples:

```hexal
let typed: Int32 = 13
let inferred = 13
let mut counter: Int32 = 0
static let port: Int32 = 8080
static let mut requests = 0
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
- Visibility, shadowing, protected-name rules, source-order type visibility,
  and module-value restrictions are unchanged.
- A `static let` keeps the module-value contract: program-lifetime storage
  private to its module unless exported, with the closed static-initializer set.

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
| `mut` outside `let mut` in a declaration | Syntax Error: `'mut' appears only immediately after 'let' in a declaration` |
| Assignment to a fixed binding or undeclared name | existing diagnostics, unchanged |

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
2. Parse `declaration = "let" , [ "mut" ] , identifier , ( ":" T "=" | "=" ) , expression`.
3. Parse `static-module-value` with the same `let ... =` shape.
4. Replace the recovery and rejection diagnostics above; remove every message
   that names `:=`.

### Phase 3: checker

1. Reword the contextual-inference diagnostic from `` `:=` `` to `` `let` ``;
   the rule itself is unchanged.
2. Confirm no semantic path inspected the `:=` token; none should.

### Phase 4: sources, catalog, and tests

1. Rewrite every `.hex` source that uses `:=`: workbench snippets, integration
   fixtures, and any doctest-like source in specs still considered active.
2. Update every diagnostic assertion that names `:=`.
3. Recompute the snippet SHA manifest only if generated C changed; it must not,
   so a moved hash is a defect to investigate before rebuilding.

### Phase 5: documentation

1. Update `docs/reference.md`: the grammar productions above, the
   declaration-operator prose, the `static` module-value spelling, and every
   `:=` example.
2. Record the change on `docs/status.md` while the spec is active; archive the
   spec when every Validation item passes.

## Required implementation sweep

- lexer token kinds, names, scanning, and tests;
- parser declaration, module-value, recovery, and every `:=` diagnostic;
- checker contextual-inference diagnostic and any `:=` mention;
- all `.hex` sources in `workbench/snippets`, `compiler/tests/integration`,
  `compiler/tests/c23validation`, `internal/driver`, `packages/`, and `stdlib/`
  that use `:=` as a declaration;
- generated-C text assertions that embed a source string;
- `docs/reference.md` grammar and prose.

## Reference synchronization

`docs/reference.md` is the sole normative source. This spec changes the
`declaration` and `static-module-value` productions, adds `let` to
`reserved-word`, restates the declaration-operator rule, and replaces every
`:=` example. No semantic rule changes.

## Validation (exhaustive)

1. `let name: T = expr`, `let name = expr`, `let mut name = expr`, and
   `static let [mut] name [: T] = expr` each parse and check.
2. `name := expr` reports `':=' is not a declaration operator; use 'let name = value'`.
3. `name: T = expr` with no `let` reports `declarations require 'let'`.
4. `let name: T` with no initializer reports `expected '=' in a 'let' declaration`.
5. `let name = <contextual>` is rejected with the `` `let` `` inference
   diagnostic; the equivalent annotated form is accepted.
6. `name = expr` with no prior binding is rejected as an unknown name and never
   declares; `let name = expr` followed by `name = expr` is accepted when the
   binding is `mut` and rejected when it is fixed.
7. `mut` before `let` (`mut let x = 1`) is rejected; `let mut x = 1` is accepted.
8. `let` cannot be used as an identifier; a program that used `let` as a name now
   fails as a reserved-word use.
9. Parameters, members, ADT payloads, results, `for` binders, `self`, named
   constructor arguments, and `extern` declarations are unchanged and still
   parse.
10. Generated C is byte-identical for every program that compiles before and
    after the change; the snippet SHA manifest does not move. If it moves, the
    change altered semantics and is wrong.
11. `go test ./...`, `go vet ./...`, `go vet -tags c23 ./...`, and the complete
    tagged C23 suite pass with only Clang installed.
12. The workbench snippet catalog, integration fixtures, and tagged fixtures
    that use declarations all still compile, run, and produce their exact
    expected output after the rewrite.
13. `gofmt -l` is clean on every changed Go file, and no test asserts `:=`.

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
2. Module values are `static let x`, not `let static x`. `static` keeps its
   role as the module-scope marker; `let` remains the introducer.
3. `:=` is a hard error, not a deprecated alias. Its one diagnostic names the
   replacement directly, so the language has exactly one declaration spelling.

## Consequences

- Every existing `.hex` source and the snippet catalog must be rewritten; this
  is an intentional breaking surface change.
- `let` becomes reserved, so a program using `let` as an identifier breaks.
- Declarations and assignments become distinguishable by keyword, and `=`
  regains a single meaning in both directions.
