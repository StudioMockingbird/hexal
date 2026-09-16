# RFC 0143: Raw Strings and Explicit-Heap Interpolation

- Kind: Language Semantics
- Status: Closed; implemented 2026-09-07. `compiler/lexer/lexer.go` gained
  `r"..."`/`r#"..."#` raw-string scanning (`scanRawString`, delimiter counted
  via `rawStringOpens`, closing on the first `"` followed by at least as many
  `#`) and a non-recursive-in-spirit but Go-call-bounded (128-deep,
  `maxInterpolationDepth`, same limit and message as the parser's
  `maxSyntaxDepth`) interpolation scanner: the `Lex` loop was refactored into
  a reusable `scanToken` primitive so `lexInterpretedString` can drive the
  same tokenizer recursively for embedded expressions and nested strings,
  emitting the existing single `StringLiteral` token unchanged for any
  literal with no unescaped `{{`, or the new
  `InterpStringStart`/`InterpText`/`InterpOpen`/`InterpClose`/`InterpStringEnd`
  sequence otherwise; `\{`/`\}` were added to `DecodeLiteralBody`'s escape
  switch. The parser's `interpolationTemplate()` builds one
  `InterpolationTemplateExpression` from that token sequence (only genuine
  syntax error: an empty `{{}}`, since a lexer diagnostic already
  short-circuits parsing on every other malformed case). The checker
  (`compiler/checker/strings.go`) added `checkRawStringLiteral` (sharing
  `checkedStringLiteralValue` with the ordinary literal path) and
  `checkStringInterpolate`, restructuring `checkStringTypeCall`'s dispatch
  into a switch; the general expression dispatch rejects an
  `InterpolationTemplateExpression` reached outside that one call-argument
  position with the spec's exact contextual diagnostic. The generator
  (`compiler/generator/interpolation.go`, new) hoists each
  `String.interpolate` call's Heap-first, left-to-right, exactly-once
  evaluation into a prologue producing a `hex_string_storage` allocated
  once from checked-summed segment lengths, reusing the interned literal
  pool for text segments and new demand-driven `hex_string_format_*`
  helpers (`compiler/generator/packages/string.{h,c}`, gated behind a new
  `NeedInterpolation` flag) for scalar segments; String/Strand segments read
  their own `.data`/`.byte_length`/`.rune_length` directly. Deliberate scope
  decision, not shared with `print`: the new format helpers duplicate
  print's exact snprintf/NaN/Inf spelling rather than factoring print.go's
  Go-side emission into shared metadata, to avoid touching already-shipped
  print output this late in the change; behaviorally identical, verified by
  direct GCC-compiled-and-run comparison. Verified end-to-end (scalar,
  String/Strand/Rune, raw-string-into-interpolation, and all six rejection
  diagnostics) by compiling and running real programs under
  `-std=c23 -Wall -Wextra -Werror`. Added six workbench snippets
  (`text-raw-path`, `text-raw-quoted`, `text-raw-multiline`,
  `text-interpolation-scalar`, `text-interpolation-unicode`,
  `text-interpolation-order`) and updated `docs/reference.md`'s lexical
  rules, grammar, and Text section. A template-file whitespace regression the
  new `{{if .NeedInterpolation}}` block introduced into every
  String-using program's generated `hexal/string.{h,c}` (one stray blank
  line, via Go template `{{end}}` leaving its trailing newline) was caught by
  the manifest rebuild and fixed with `{{end -}}` trim markers before any
  snippet's baseline was touched; the rebuilt manifest is additive only: 7
  new entries, 0 pre-existing entries changed. Full gate green: `gofmt`,
  `go build`, `go vet` (plain and `-tags c23`), `go test -count=1 ./...`, and
  `go test -tags c23 -count=1 ./...` (real GCC/Clang/Zig compilation of the
  whole tagged snippet catalog, ~5 minutes, zero warnings).
- Created: 2026-09-07
- Updated: 2026-09-07
- Coordinates with: RFC 0142, `docs/reference.md`
- Does not change: UTF-8 validity, immutable `String` representation, explicit
  allocator passing, ordinary string escapes, `print`, or general call syntax

## Summary

Add:

1. Raw UTF-8 string literals with hash-delimited terminators.
2. String interpolation through one explicit-Heap associated operation.

```hexal
path := r#"C:\data\input "quoted".txt"#

message := String.interpolate(heap, "{{name}} has {{count}} items")
defer message.free(heap)
```

A raw literal is static storage and performs no escape processing or
interpolation. Interpolation produces one heap-owned `String`; its explicit
`Heap` argument prevents hidden allocation.

## Goals

- Express backslashes, quotes, generated text, and multiline text without
  escape noise.
- Embed typed Hexal expressions in text without manual conversion and
  concatenation.
- Preserve Hexal's explicit allocator-passing and ownership contracts.
- Evaluate every interpolation expression exactly once in source order.
- Lower to direct, readable C23 without a formatting language, runtime parser,
  variadic ABI, or compiler-owned general-purpose formatter.

## Grammar

Add these lexical and expression productions:

```ebnf
raw-string-literal = "r" , raw-hashes , '"' , raw-contents
                     , '"' , raw-hashes ;
raw-hashes = { "#" } ;

interpolation-call = "String" , "." , "interpolate" , "("
                     , expression , "," , interpolation-template
                     , [ "," ] , ")" ;
interpolation-template = interpreted-string-start
                         , { template-text | interpolation }
                         , interpreted-string-end ;
interpolation = "{{" , expression , "}}" ;
```

`raw-contents` ends at a quote followed by at least the opening delimiter's
number of `#` characters. A shorter quote/hash sequence is content. The closing
delimiter consumes only the required number; any additional `#` characters
begin following tokens. The scanner processes the input linearly; delimiter
count does not introduce recursion.

`interpolation-template` is contextual syntax valid only as the second
argument of `String.interpolate`. It is not a separately typed expression.
Nevertheless, the lexer recognizes interpolation in every interpreted string
so it can tokenize nested expressions and nested string literals correctly in
one forward pass. Contextual validity is decided after tokenization.

This remains lexical delimiter recognition, not expression parsing. The lexer
maintains a mode stack and delimiter depths only to find the matching `}}`;
the parser alone assigns syntax and meaning to the emitted expression tokens.
This first-pass recognition is necessary because the existing pipeline lexes
the complete source before parsing, and an ordinary whole-string token would
terminate at a nested expression's quote before the parser could recover it.

## Raw string literals

```hexal
plain := r"C:\data\input.txt"
quoted := r#"He said "hello"."#
marked := r##"The text contains "# and {{braces}}."##
multiline := r"first
second"
```

- Any nonnegative number of delimiter hashes is accepted.
- Contents are copied byte-for-byte from source after removing the delimiters.
- Backslashes, quotes not matching the closing delimiter, `{`, `}`, and line
  breaks have no special meaning.
- Raw literals never interpolate. `{{name}}` is literal text.
- Contents must be valid UTF-8 under the existing source and `String` rules.
- The resulting value is an immutable literal-backed `String` in static
  storage. It allocates nothing and cannot be passed to `String.free`.
- Byte length, rune length, indexing, slicing, equality, ordering, and printing
  are identical to an interpreted literal containing the same UTF-8 bytes.
- The `r` prefix is recognized as raw-string syntax only when immediately
  followed by `"` or one or more `#` followed by `"`. Otherwise `r` is an
  ordinary identifier.

An unterminated raw literal reports `Lexical Error` at its opening `r`:
`unterminated raw string literal`.

## Interpolation

### Surface

```hexal
message := String.interpolate(
    heap,
    "user {{user_id}}: {{enabled}}",
)
defer message.free(heap)
```

- `String.interpolate` is a compiler-known associated operation, not a
  user-overloadable function.
- It accepts exactly one `Heap` expression and one interpreted template.
- The Heap expression is evaluated first and exactly once.
- Embedded expressions are evaluated exactly once, left to right.
- The returned `String` is allocated from that Heap and must follow the normal
  `String.free(heap)` ownership contract.
- A template must contain at least one interpolation. Use a normal string
  literal when no interpolation is needed.
- A raw literal cannot be an interpolation template. Raw text is deliberately
  inert.

### Template text and braces

Every interpreted string is scanned for `{{`, including strings outside
`String.interpolate`. Ordinary interpreted-string escapes retain their current
meanings. Add `\{` and `\}` as interpreted-string escapes:

| Source | Template text |
| --- | --- |
| `\{` | literal `{` |
| `\}` | literal `}` |
| `{{ expression }}` | formatted expression value |

A single unescaped `{`, any `}` in template-text mode, and `}}` in
template-text mode are literal text. Only `{{` enters expression mode. Escape
one or both opening braces to write a literal `{{`. `{{}}` is rejected because
an interpolation requires an expression. The closing `}}` belongs to the
template; braces inside a nested expression are balanced by the lexer and the
ordinary expression parser and do not close it early.

An interpreted string containing `{{ expression }}` outside the exact template
position is rejected rather than implicitly allocating:

```hexal
text := "Hello {{name}}"  -- rejected
text := "Hello \{{name}}" -- literal "Hello {{name}}"
```

Diagnostics:

- unmatched opening delimiter: `unterminated string interpolation`
- empty interpolation: `string interpolation requires an expression`
- interpolation outside the protected operation:
  `string interpolation requires String.interpolate(heap, template)`
- unsupported embedded type:
  `string interpolation does not support <Type>`
- non-template second argument:
  `String.interpolate requires an interpreted interpolation template`
- template without an interpolation:
  `String.interpolate requires at least one interpolation`

The earliest scanner or parser able to prove the error owns the diagnostic.

### Supported values

Interpolation supports exactly these value types:

- `Bool` and `Rune`;
- every fixed-width signed and unsigned integer, `Size`, and `Byte`;
- `Float32` and `Float64`;
- `String` and `Strand`.

Formatting is the same value spelling as direct `print`, without quotes,
separators, or a trailing line break. No implicit user conversion, method
lookup, or aggregate traversal occurs.

`Nil`, pointers, unions, structs, ADTs, arrays, views, lists, dictionaries,
allocators, concurrency values, `Error`, and `Fun` are unsupported. Supporting
a new type requires an explicit language contract; it does not follow merely
because `print` later learns that type.

### Allocation and failure

- One successful interpolation performs exactly one payload allocation for
  the returned String under the existing String representation.
- The implementation first computes the exact checked UTF-8 byte length, then
  allocates once, then writes each literal and formatted segment.
- Length addition and allocation-size arithmetic use the existing checked C23
  path. Overflow traps before allocation.
- Allocation failure follows the existing String-allocation trap contract.
- No intermediate Hexal `String`, `List`, growable buffer, or C heap object is
  created.
- Expression evaluation is not repeated between length measurement and
  writing; each value is captured once in a typed C temporary.

## C23 lowering

Raw literals use the existing static UTF-8 literal representation and literal
pool. Equal raw and interpreted contents may share storage under the existing
pooling rules.

For interpolation, generated C:

1. Evaluates and stores the Heap.
2. Evaluates each embedded expression into one typed temporary.
3. Computes each formatted segment's byte length with the same C23 formatting
   contract used to write it.
4. Checked-adds literal and formatted lengths.
5. Allocates the final String once.
6. Copies literal bytes and writes formatted values directly into the final
   buffer in source order.
7. Writes the one terminating NUL required by the String representation.

Use standard C23 facilities directly when they exactly implement a step.
Reuse existing formatter fragments where they carry Hexal semantics; do not
introduce a wrapper that merely delegates to a standard function. Generated
helpers are demand-driven and live in the String component, not `hexal.h` or a
module header. A program with raw strings but no interpolation selects no
interpolation helper.

## Ownership and placement

- A raw literal is static-backed and non-owning exactly like an interpreted
  literal.
- An interpolated result is heap-backed and owning exactly like other runtime
  String constructors.
- Copying the returned handle does not transfer or duplicate allocation
  ownership; existing String alias and free rules apply.
- Interpolation accepts borrowed `String` and `Strand` operands for the duration
  of the call. It stores only their bytes in the new result.
- The result contains no View and introduces no new lifetime relation to an
  embedded operand.

## Implementation plan

### Phase 1: baseline and inventory

1. Run the ordinary and tagged suites and record the snippet manifest.
2. Inventory the interpreted-string scanner, string token payload, parser
   primary-expression dispatch, literal pool, String dependency discovery,
   print formatting fragments, String allocation path, and every exhaustive
   token/expression switch.
3. Record generated-C baselines for interpreted literals, escaped literals,
   String construction/free, print formatting of every interpolation-supported
   scalar, and a module containing no String.

### Phase 2: raw-string scanning

4. Add one raw-string token kind carrying decoded UTF-8 bytes and the opening
   delimiter position.
5. Recognize `r` plus the complete quote/hash delimiter before identifier
   scanning, without reserving `r`.
6. Scan until the exact closing delimiter with one forward pass. Preserve
   source line accounting across multiline contents and diagnose EOF at the
   opening delimiter.
7. Validate UTF-8 through the same literal path as interpreted strings; do not
   create a second String literal representation.
8. Parse the token as the existing checked String-literal expression and send
   it through the existing literal pool and dependency discovery.

### Phase 3: interpolation syntax

9. Add explicit interpolated-string start, text, interpolation-open,
   interpolation-close, and string-end token shapes. A quoted string containing
   no unescaped `{{` retains the existing single `StringLiteral` token and
   downstream path.
10. Make the lexer inspect every interpreted string. On unescaped `{{`, emit
    the preceding decoded text and enter ordinary expression-token mode; close
    only on `}}` at that interpolation's outer delimiter depth, then resume
    string-text mode.
11. Reuse ordinary tokenization inside the embedded expression, including
    nested interpreted/raw strings and comments. Track parentheses, brackets,
    and braces so their closing tokens cannot terminate interpolation early.
    Preserve absolute source line/column positions on every emitted token.
12. Add `\{` and `\}` to interpreted-string escape decoding everywhere. Raw
    strings remain escape-free. A single brace and closing braces in text mode
    remain literal.
13. Parse an interpolated token sequence into one ordered template node holding
    decoded literal-byte segments and ordinary expression nodes with precise
    source ranges. Enter the ordinary shared syntax-depth budget for the
    template and every embedded or nested interpolation production; the
    implemented maximum remains 128 and transitively bounds checker and
    generator recursion over the resulting tree.
14. Recognize the exact `String.interpolate(` form through the existing
    compiler-owned type-qualified operation path. Accept the template node only
    as its second argument; reject it in every other expression or call
    position with the exact contextual diagnostic. Coordinate the shared call
    argument parser with RFC 0142; neither RFC semantically depends on the
    other.
15. Emit the exact malformed-template diagnostics before ordinary call
    checking can reinterpret the form.

### Phase 4: checking and metadata

16. Resolve the first argument as `Heap` and reject every other type through
    ordinary argument checking.
17. Check embedded expressions left to right in the surrounding lexical scope.
    Require a value and one supported concrete type for each expression.
18. Record each segment's checked type and formatting kind. Checked metadata
    validation fails closed on a missing, unknown, or unsupported kind.
19. Mark the result as an owning heap-backed `String` and reuse existing String
    placement, escape, cleanup, and free analysis.
20. Add interpolation demand to component discovery only for a successfully
    checked interpolation expression.

### Phase 5: generation

21. Add typed capture emission that preserves Heap-first and expression
    left-to-right evaluation exactly once.
22. Factor the existing scalar/text formatting knowledge into shared internal
    generator metadata used by print and interpolation without changing
    print's output or generated symbols.
23. Emit exact-length measurement, checked accumulation, one String allocation,
    ordered writes, and one final NUL as specified.
24. Keep helper selection demand-driven and String-component-owned. Verify no
    helper, include, or generated artifact appears for raw-only programs.
25. Extend every expression walker, validation switch, source-mapping path,
    and dependency walker for interpolation and fail closed on malformed
    checked metadata.

### Phase 6: migration, documentation, and gates

26. Add compact workbench snippets for zero-hash raw text, hash-delimited
    quotes, multiline raw text, scalar interpolation, Unicode text
    interpolation, and evaluation order. Reuse snippets where meaningful.
27. Update active specs whose executable examples need the new syntax; never
    edit archived specs.
28. After behavior stabilizes, update `docs/reference.md` grammar, lexical
    rules, interpolation type set, evaluation order, allocation, ownership,
    diagnostics, and C23 contracts.
29. Add manifest entries only for genuinely new snippets. No existing entry
    may change hash.
30. Run every Validation item below, remove the status row, close the RFC, and
    archive it only when code, tests, snippets, and reference agree.

## Required sweep

- Every token-kind and primary-expression dispatch for the raw-string token.
- Every token-kind, recovery path, source-position calculation, and parser
  dispatch for the interpolated-string token sequence.
- Every byte/rune length, literal pooling, static-storage, print, comparison,
  slicing, and String dependency path that distinguishes literal syntax.
- Every expression walker and checked-metadata validator for interpolation.
- Print formatting knowledge duplicated rather than shared internally with
  interpolation.
- String-component selection, include ordering, helper naming, and demand
  predicates.
- Interpreted-string escape decoding and diagnostics that assume `{` and `}`
  have no string-level meaning.
- Diagnostics or tests that assume interpreted quotes are the only String
  literal form.
- Reserved-word inventories: `r` must not be added.
- Snippet catalog feature tags and generated-artifact manifest.
- `docs/reference.md` and active specifications; archived specs are immutable.

The sweep must not add hidden allocation, a general formatting protocol,
named function arguments, implicit conversion to String, or interpolation to
raw literals.

## Validation

This section is exhaustive.

### Raw strings

- `r"text"`, an empty raw literal, and literals with one and multiple hash
  delimiters produce the exact expected UTF-8 bytes.
- Backslashes, interpreted escape spellings, braces, non-closing quotes, and
  shorter quote/hash sequences remain literal content.
- A multiline raw literal preserves line breaks and maintains correct source
  lines for the following token and diagnostic.
- A raw literal containing Unicode has the same byte length, rune length,
  equality, ordering, slicing, and print behavior as an equivalent interpreted
  literal.
- Equal raw and interpreted contents use the existing literal-pool identity
  policy and produce no heap allocation.
- The first quote followed by at least the opening hash count closes the
  literal; missing quote or hash suffix reports the exact unterminated
  diagnostic at the opening `r`.
- A closing delimiter followed by additional hashes consumes only its required
  hashes; the remainder is tokenized after the literal.
- `r`, `raw`, and identifiers beginning with `r` remain legal identifiers.
- Raw literals reject invalid UTF-8 through the existing literal diagnostic.

### Interpolation syntax and checking

- A template embeds every supported type and formats it exactly as direct
  print without separators, quotes, or a newline.
- Literal text and expressions alternate correctly, including interpolation
  at the beginning or end and adjacent interpolations.
- `\{` and `\}` produce literal braces in every interpreted string. Single
  unescaped braces and closing-brace runs in text mode remain literal.
- `{{}}` and an unterminated interpolation receive the exact specified
  diagnostics.
- An ordinary interpreted string containing interpolation syntax outside
  `String.interpolate` receives the exact contextual diagnostic; escaping an
  opening brace produces the intended literal text without allocation.
- Nested calls, indexing, parenthesized binary expressions, and literals inside
  an interpolation parse without prematurely consuming `}}`.
- A nested interpreted string containing braces and quotes is tokenized as part
  of the embedded expression rather than terminating the outer template.
- Interpolation nesting enters the same shared syntax-depth budget as every
  other recursive production: depth 127 remains accepted where otherwise
  valid, while crossing the implemented maximum of 128 reports the existing
  nesting diagnostic rather than exhausting the Go stack.
- The Heap expression and embedded expressions evaluate exactly once in strict
  left-to-right order.
- A non-Heap first argument, raw or ordinary expression as the second argument,
  template with no interpolation, no-result embedded call, and every excluded
  type are rejected at the earliest specified phase.
- Ordinary calls, methods, function values, and unrelated built-ins do not gain
  interpolation-template syntax.

### Allocation and generated C

- One successful interpolation performs one String payload allocation, writes
  the exact UTF-8 byte length, and appends exactly one NUL.
- Empty formatted values and empty literal segments do not cause extra
  allocation or incorrect offsets.
- Length and allocation overflow use the existing checked trap path before any
  undersized allocation or write.
- Captured expression values are reused for measurement and writing; generated
  C contains no repeated operand expression.
- Raw-only output uses the existing static literal representation and selects
  no interpolation helper or additional component.
- Interpolation helpers, if demanded, occur once in the String component and
  never in `hexal.h` or module headers.
- Programs not using either feature produce byte-identical artifacts. Existing
  snippet-manifest hashes do not change; only new snippet entries are added.
- Generated source retains correct `#line` mapping around raw multiline content
  and every embedded expression.
- The complete tagged snippet catalog compiles under every available supported
  C23 toolchain.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Handoff

After implementation and validation, rebuild the workbench binary into `bin/`
and restart the running workbench. This is an operational handoff requirement,
not a language acceptance test.

## Consequences

- Raw literals add lexical syntax but no runtime representation or allocation.
- Interpolation adds one explicit-Heap String operation and no general
  formatting subsystem.
- Interpolated results visibly own memory and use existing String cleanup.
- Supported interpolation values form a deliberately closed set.
- Ordinary strings remain static literals; ordinary calls remain positional.

## Non-goals

- Implicitly allocating interpolated literals.
- Interpolation inside raw strings.
- Format specifiers, width, precision, alignment, locale, reflection, or a
  user-extensible formatting protocol.
- General conversion of arbitrary values to String.
- Aggregate, collection, pointer, Error, allocator, concurrency, or function
  formatting.
- Compile-time interpolation or constant folding.
- Changing String representation, ownership, or UTF-8 validation.
- Replacing `print`, adding streams/builders, or changing String concatenation.
