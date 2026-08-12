# RFC 0004: Source and Generated Identifiers

- Status: Implemented
- Created: 2026-08-07
- Revised: 2026-08-07
- Applies to: source declarations and private generated C names
- Related: RFC 0005 (type declarations and transparent aliases), ADR 0001
  (structured checked expressions)

## Summary

Seawitch has two source-name rules:

1. an identifier begins with an ASCII letter and continues with ASCII letters,
   decimal digits, or `_`; and
2. a visible type and value cannot have the same source spelling, as specified
   by RFC 0005.

`main` is an ordinary identifier.

Private C identifiers derived from Seawitch source receive a fixed kind
prefix:

| Seawitch declaration | Generated C |
|---|---|
| value `score` | `sw_v_score` |
| nominal type `Point` | `sw_t_Point` |
| object member `x` | `sw_m_x` |

The mapping is always `prefix + complete source spelling`. It is stateless and
unconditional. It has no C keyword list, hash, truncation, length rule,
collision retry, or name registry.

## Motivation

The current compiler copies value names directly into C:

```seawitch
int: Int32 = 10
```

```c
const int32_t int = INT32_C(10); // Invalid C.
```

Conditionally escaping C keywords adds a special-case list and makes generated
names depend on their spelling. One unconditional rule is simpler:

```c
const int32_t sw_v_int = INT32_C(10);
```

The kind prefix also keeps generated C readable. A reader can distinguish a
Seawitch value, type, and member without compiler metadata.

Letter-led source names are a language-surface decision, not a requirement of
the generated prefix. They keep application names visually ordinary and leave
leading-underscore forms out of Seawitch.

Likewise, the type/value rule is a source-language clarity decision. The C
prefixes could distinguish the two declarations, but Seawitch deliberately
gives one visible spelling one meaning.

## Detailed design

### Source identifiers

```ebnf
identifier = ASCII-letter , { ASCII-letter | decimal-digit | "_" } ;
```

Identifiers are case-sensitive:

```seawitch
player: Int32 = 1
Player: Int32 = 2
player_2: Int32 = 3
```

They cannot begin with `_` or a digit:

```seawitch
_player: Int32 = 1 // Syntax Error: identifiers must begin with a letter.
2player: Int32 = 2 // Invalid declaration name.
```

Keywords are excluded after the lexer scans the character form. Unicode
identifiers are outside this RFC.

`main` is not a keyword. A value named `main` is safe because it does not emit
the C entry-point spelling:

```seawitch
main: Int32 = 1
```

```c
const int32_t sw_v_main = INT32_C(1);
```

C keywords are not automatically Seawitch keywords. For example, `int` and
`restrict` remain valid Seawitch identifiers.

### Type and value names

RFC 0005 owns the shared visible-name rule:

```seawitch
type Point = Int32
Point: Int32 = 10 // Type Error: Point is already declared as a type.
```

The reverse declaration order is also illegal. Built-in type names count as
visible type names. Scope and shadowing details remain in RFC 0005.

### Private generated C names

```text
private-c-name(kind, source-name) = prefix(kind) + source-name
```

| Current kind | Prefix |
|---|---|
| value binding | `sw_v_` |
| nominal type and its structure tag | `sw_t_` |
| object member | `sw_m_` |

Examples:

```text
score       -> sw_v_score
int         -> sw_v_int
INT32_MAX   -> sw_v_INT32_MAX
Point       -> sw_t_Point
x           -> sw_m_x
sw_v_score  -> sw_v_sw_v_score
```

The generator never guesses that source text is already lowered. It applies
the mapping once to every declaration and reference.

The mapping is total for every checked source identifier and injective within
each kind. Members may repeat across object types because they are resolved
within their containing structure.

Long names are emitted in full. No supported target currently justifies a
source limit or a shortened generated spelling.

ADR 0001 requires references to reach the generator as resolved identities and
structured operations rather than pre-rendered C strings. That architectural
decision ensures declarations and references use this one mapping site.

### Phase ownership and diagnostics

The lexer owns invalid identifier shape. The checker owns declaration
conflicts, including the RFC 0005 type/value rule. The generator owns private
C-name construction.

Representative diagnostics are:

```text
[Syntax Error] identifiers must begin with a letter
[Type Error] Point is already declared as a type
```

An unsupported checked declaration kind reaching generation is an `Unknown
Error`. The generator must not fall back to a raw source name.

### Foreign names

These prefixes apply only to Seawitch-owned private declarations. Future C
imports retain their declared foreign names; for example, raylib calls the
exact foreign name `DrawCircle`.

An imported macro or symbol could deliberately use an exact `sw_...` spelling.
Detecting or aliasing that conflict belongs to the future C-interchange
specification. This RFC does not scan headers or claim a public ABI namespace.

## Drawbacks

Every private source-derived name becomes slightly longer. The added text is
fixed and identifies the declaration kind.

Leading-underscore source names are rejected even though prefixing would make
them safe in C. This is an intentional source-language choice.

The private prefix alone cannot guarantee absence of a deliberately matching
foreign macro. C importing must still handle known foreign collisions.

## Alternatives considered

### Escape only C23 keywords

Rejected. It adds a conditional rule and does not address non-keyword macros.

### Emit raw names

Rejected. Valid Seawitch names could produce invalid C.

### Use leading `_` or `__` prefixes

Rejected. Those forms intersect C implementation-reserved naming rules and
make generated code look implementation-owned.

### Hash, truncate, or shorten names

Rejected. No supported target requires it. It adds machinery and obscures
generated C.

### Let different prefixes legalize matching type and value names

Rejected by RFC 0005. Generated naming must not broaden source-language name
semantics.

## Outside this RFC

- checked-expression structure, covered by ADR 0001;
- function and method naming, covered by RFC 0008;
- compiler-created helper names, owned by the feature that first needs them;
- exported names, C imports, and foreign collision handling;
- module qualification and cross-translation-unit linkage;
- Unicode identifiers; and
- demonstrated target-specific identifier limits.

## Implementation acceptance criteria

Implementation is complete when tests prove that:

1. identifiers follow the letter-led grammar;
2. a leading `_` and other invalid declaration names fail with structured
   diagnostics;
3. `main` remains an ordinary identifier and a value named `main` emits as
   `sw_v_main`;
4. every current value declaration and reference uses
   `sw_v_<complete-source-name>`;
5. `int`, `restrict`, and `INT32_MAX` generate valid C without a C-name list;
6. a source name beginning with `sw_v_` is prefixed normally;
7. long names are emitted without hashing, truncation, or length diagnostics;
8. RFC 0006 object types and members use `sw_t_` and `sw_m_`; and
9. generated C compiles and preserves existing runtime behavior.

The RFC 0005 acceptance criteria separately prove the type/value name rule.

## Implementation handoff

The implementation plan must cover:

1. the lexer change for letter-led identifiers;
2. one stateless generator helper for prefix plus source spelling;
3. every current value declaration and reference site;
4. fail-closed handling for unsupported declaration kinds;
5. focused lexer and generator tests; and
6. end-to-end C23 compilation cases for C keywords, standard macro names,
   prefix-like source names, `main`, and long names.

ADR 0001 is implemented with this RFC so declarations and references cannot
acquire different C spellings. Canonical grammar, language, and status
documents are updated only after implementation stabilizes. No analyzer pass
is required.
