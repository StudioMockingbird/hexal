# RFC 0061: Explicit Block Openers

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-15
- Features: mandatory `do` for function and method bodies; mandatory `then`
  for `if` and `elseif` bodies
- Created: 2026-08-15
- Depends on: RFC 0008 (functions and methods), RFC 0015 (structured control
  flow), RFC 0022 (match), and RFC 0028 (`for`)
- Coordinates with: `docs/reference.md`, compiler parser tests, integration
  tests, dormant C23 canaries, and workbench snippets

## Summary

Every structured body has an explicit opening delimiter and one closing
`end`:

```hexal
fun add(left: Int32, right: Int32): Int32 do
    return left + right
end

impl Point.x(): Int32 do
    return self.x
end

if value > 0 then
    print("positive")
elseif value < 0 then
    print("negative")
else
    print("zero")
end

while ready do
    process()
end

for item in items do
    print(item)
end
```

`do` opens an executable function, method, or loop body. `then` opens the
consequence of a condition or match pattern. `else` is itself the opener for
its body.

## Motivation

Current function, method, `if`, and `elseif` headers end without a delimiter,
while loops already require `do` and match arms require `then`. The split is
arbitrary and makes body boundaries less explicit.

The revised forms align with the existing Lua-like surface:

- `if condition then ... end`;
- `while condition do ... end`;
- `for value in source do ... end`; and
- `match` arms using `then`.

Functions and methods are non-capturing declaration boundaries; control-flow
bodies can access their enclosing bindings. The new keywords expose body
boundaries but do not create or alter scope semantics.

## Normative syntax

Replace the affected grammar productions with:

```ebnf
function-declaration = "fun" , identifier , [ generic-parameter-list ]
                       , signature , "do" , block , "end" ;
implementation-declaration = "impl" , type-expression , "." , identifier
                             , [ generic-parameter-list ] , signature
                             , "do" , block , "end" ;
if-statement = "if" , expression , "then" , block
               , { "elseif" , expression , "then" , block }
               , [ "else" , block ] , "end" ;
while-statement = "while" , expression , "do" , block , "end" ;
for-statement = "for" , for-binders , "in" , expression
                , "do" , block , "end" ;
match-arm = "|" , match-pattern , "then" , match-arm-expression ;
```

Rules:

- `do` is mandatory after every function and implementation signature.
- `then` is mandatory after every `if` and `elseif` condition.
- `do` remains mandatory after every `while` condition and `for` source.
- `then` remains mandatory after every match pattern.
- `else` takes no additional delimiter.
- The opener may be separated from its header by ordinary lexical separation;
  it need not occur on the same line.
- The former delimiter-free function, method, `if`, and `elseif` forms are
  Syntax Errors; there is no compatibility mode or optional spelling.
- `do` and `then` are already reserved words; the lexer and reserved-word set
  do not change.

## Semantics

This RFC changes syntax only:

- function, method, branch, and loop scope rules are unchanged;
- declaration visibility and capture restrictions are unchanged;
- evaluation order and control flow are unchanged;
- AST meaning, checking, and C23 lowering are unchanged; and
- adding an opener on an existing header line does not change generated
  `#line` mappings.

## Diagnostics

- A function or implementation signature not followed by `do` reports a
  Syntax Error at the first token after the signature.
- An `if` or `elseif` condition not followed by `then` reports a Syntax Error
  at the first token after the condition.
- Diagnostics name the missing delimiter and owning construct.
- Recovery must not consume the matching `elseif`, `else`, or `end` as part of
  a malformed condition or body.

Representative required messages:

```text
expected 'do' after function signature
expected 'do' after method signature
expected 'then' after if condition
expected 'then' after elseif condition
```

Exact punctuation may follow existing parser conventions; the construct and
missing keyword must remain explicit.

## Implementation

### Parser

- After parsing a function signature, consume `lexer.Do` before its body.
- Apply the same rule to both local and qualified implementation receiver
  paths.
- After parsing each `if` or `elseif` condition, consume `lexer.Then` before
  its body.
- Keep the existing mandatory `do` consumption for `while` and `for`.
- Keep the existing mandatory `then` consumption for match arms.
- Update parser recovery and construct-specific diagnostics without changing
  the checked AST unless retaining delimiter tokens becomes necessary for
  diagnostics.

### Source migration

Migrate every compiler-owned Hexal source string:

- lexer/parser/checker/generator unit fixtures;
- `compiler/tests/integration/` programs;
- dormant `compiler/tests/c23validation/` programs;
- workbench snippet sources; and
- any smoke or testdata source strings.

Add `do` or `then` to existing header lines where practical so source line
numbers and generated C remain stable. Workbench snippet `reservedWords`
metadata must include every newly used delimiter.

### Reference synchronization

After implementation stabilizes, update only the affected EBNF productions in
`docs/reference.md`. Semantic sections require no new contract beyond the
mandatory delimiters. Verify the reserved-word list remains unchanged.

## Required tests

- Accept functions and methods with mandatory `do`.
- Accept `if` and every `elseif` with mandatory `then`.
- Retain acceptance of `while ... do`, `for ... do`, match-arm `then`, and
  delimiter-free `else`.
- Accept a delimiter on the next line or after a comment.
- Reject every former delimiter-free form.
- Reject `then` where `do` is required and `do` where `then` is required.
- Verify missing-delimiter recovery preserves following branches and sibling
  statements.
- Verify exported, generic, no-result, recursive, and method declarations use
  the same rule.
- Verify nested combinations of functions, branches, loops, and match parse
  with the expected ownership of each `end`.
- Verify representative migrated programs generate byte-identical C except
  where source-line fixtures intentionally change.
- Run `go test ./...`, `go vet ./...`, `go test -tags c23 ./...`, and
  `go vet -tags c23 ./...`.

## Acceptance criteria

1. Every function and implementation body requires `do ... end`.
2. Every `if` and `elseif` body requires `then ... end` through the enclosing
   conditional.
3. Loop and match delimiters retain their existing spellings.
4. Old delimiter-free forms fail with construct-specific Syntax Errors.
5. No checker, generator, ABI, or runtime behavior changes.
6. All compiler-owned sources, workbench snippets, and metadata use the new
   grammar.
7. `docs/reference.md` matches the implemented grammar before closure.
8. The ordinary and tagged test suites remain green.

## Adjacent Lua/Luau/Crystal-style syntax candidates

These candidates are not accepted by this RFC and do not block it.

### Recommended for a later syntax-cleanup RFC

#### Uniform trailing commas

Permit one trailing comma in every comma-delimited multiline form:

```hexal
fun transform(
    value: Int32,
    scale: Int32,
): Int32 do
    return combine(
        value,
        scale,
    )
end
```

Candidate positions:

- function and method parameters;
- call arguments;
- generic parameters and type arguments;
- function-type parameter lists; and
- existing object, ADT, Array, and initializer lists.

Hexal already permits trailing commas in several aggregate forms. Extending
the same rule removes an arbitrary formatting distinction without changing
semantics. Recommendation: adopt as one uniform rule, not per-form options.

### Worth discussing, but not recommended yet

#### Word-form logical negation

Replacing `!value` with `not value` would make the existing `and`/`or` family
lexically uniform and align with Lua/Luau:

```hexal
if not ready then
    return
end
```

This is a spelling migration rather than a capability. `!` remains familiar
to C-family systems programmers and pairs with `!=`; supporting both would
violate the one-obvious-way rule. Recommendation: decide separately and, if
accepted, replace `!` rather than alias it.

### Explicitly not recommended

- Rename `fun` to Lua/Luau `function` or Crystal `def`: neither alternative is
  shared, and `fun` is already compact and unambiguous.
- Rename `elseif` to Crystal `elsif`: `elseif` already matches Lua/Luau and the
  lexer.
- Add `unless`, postfix conditionals, `repeat ... until`, or standalone
  `do ... end`: each duplicates existing control flow or adds semantics rather
  than merely regularizing syntax.
- Add Lua's `local`, implicit declarations, or untyped inference: explicit
  typed bindings are a core Hexal rule.
- Add Lua method-call `:` syntax: `.` is already the single method-access form.
- Add Lua operators `~=`, `..`, or `#`: `!=`, typed operations, and named
  length APIs are clearer for a C-oriented systems language.
- Make call parentheses optional: mandatory parentheses keep parsing and calls
  uniform.
- Add Crystal-style blocks, closures, splats, named arguments, or macros:
  these are semantic features, not syntax cleanup, and conflict with the
  current language boundary.

## Readiness

The accepted block-opener change is implementation-ready. The adjacent
candidates are explicitly non-normative and require separate approval.
