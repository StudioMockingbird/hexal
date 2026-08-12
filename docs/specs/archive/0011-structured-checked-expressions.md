# Spec 0011: Keep Checked Expressions Structured Until C Generation

- Kind: architecture decision
- Formerly: ADR 0001. Specs 0004, 0006, and 0009 cite it under that name; they
  are closed and were not edited.
- Status: Implemented
- Date: 2026-08-07
- Related: RFC 0001 (raw pointers), RFC 0002 (mutability and access), RFC 0004
  (source and generated identifiers), RFC 0006 (core object values)

## Context

The checker currently stores generator-ready C fragments in checked operands.
Examples include:

```text
"x"
"&x"
"*p"
"*(*pp)"
```

This mixes two responsibilities:

- the checker resolves names, types, addressability, mutability, and pointer
  capabilities; and
- the generator chooses C identifiers and renders C syntax.

RFC 0004 prefixes a value named `x` as `sw_v_x`. If the generator renames the
declaration while the checker still supplies `"&x"`, the declaration and its
reference disagree. Fixing the string in the checker would create two C-name
rendering sites. Parsing the string back apart in the generator would make the
checked representation unreliable.

RFC 0006 will add resolved member paths. Keeping opaque C strings would make
that coupling deeper before it becomes easier to remove.

## Decision

Checked expressions retain resolved declaration identities and structured
operations. They do not retain source-derived C identifiers or composite C
expression strings.

The current pointer operations must be representable conceptually as:

```text
Variable(binding-identity)
AddressOf(Variable(binding-identity))
Dereference(Variable(binding-identity))
Dereference(Dereference(Variable(binding-identity)))
```

The concrete Go representation may use expression nodes or a base binding plus
an ordered access path. It must preserve operation order and the stable
identity of every referenced declaration.

The checker remains responsible for proving that each operation is legal. The
generator is responsible for:

1. mapping each resolved identity to its generated C identifier;
2. rendering address-of and dereference syntax in the recorded order; and
3. failing closed on an unknown checked operation.

RFC 0006 extends the same representation with a resolved
`Member(member-identity)` operation. A checked member operation stores member
identity, not `.sw_m_name` text.

Source spellings remain in declaration metadata for diagnostics and generated
name construction. Canonical outgoing C type spellings such as `int32_t` may
remain in resolved type metadata; they are target type mappings, not
source-derived expressions.

This decision does not introduce an analyzer. The checked representation may
continue directly to generation until operators, conversion lowering,
ownership analysis, or another implemented feature requires a distinct
analyzed form.

## Consequences

### Positive

- Declaration and reference naming have one implementation site.
- The generator never has to parse C text produced by the checker.
- Pointer nesting and future member paths remain explicit and testable.
- Generated-name changes do not require checker string rewrites.
- Unsupported checked operations can fail closed before invalid C is emitted.

### Negative

- Existing checked operands and their tests must be migrated.
- Checker and generator changes must land together.
- The generator receives a small structured expression model rather than a
  printable string.

## Rejected alternatives

### Apply generated names inside the checker

Rejected. It makes the checker a second C-rendering stage and couples semantic
checking to one backend spelling.

### Keep C strings and rewrite them in the generator

Rejected. String parsing cannot reliably recover binding and member identity
and directly conflicts with the forward-only compiler pipeline.

### Add an analyzer solely for this migration

Rejected. Structured checked operands are sufficient. An empty pass would add
architecture without adding a semantic responsibility.

## Implementation constraints

Implementation must:

1. remove generator-ready variable, address-of, and dereference strings from
   checked operands;
2. retain stable binding identities and structured operation order;
3. render all source-derived C identifier references in the generator;
4. cover plain reads, `ref`, `.value`, assignment through `.value`, and nested
   `.value` in focused and end-to-end tests; and
5. fail closed for every unrecognized checked expression operation.

Code review or an architecture check—not only an end-to-end output test—must
verify that no source-derived composite C expression is constructed outside
generation.
