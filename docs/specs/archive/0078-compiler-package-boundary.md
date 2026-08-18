# RFC 0078: Compiler Package Boundary

- Kind: Architecture Decision Record (ADR)
- Status: Closed — rejected, not needed
- Created: 2026-08-16
- Closed: 2026-08-17
- Scope: Go package visibility for the compiler's stage packages

## Decision

**Rejected.** `lexer`, `parser`, `checker`, `generator`, and `types` stay where
they are. They are not moved under `compiler/internal/`.

This record exists so the proposal is not made again without the trigger below.

## What was proposed

Move the five stage packages under `compiler/internal/` so that Go, rather than
convention, enforces `compiler` as the only importable package. 858 exported
symbols across those packages would stop being public API without any of them
being renamed.

## Why it was rejected

**`internal/` enforces one rule: code outside the module subtree cannot import
this package. The module is `hexal` — an unqualified, unpublished path.** Nothing
outside this repository can import any of it already, `internal/` or not. The
restriction is total today, provided by the module not being published, and the
move would add a fence inside a building with no doors.

The two stated motivations did not survive inspection:

- *"Unexporting decisions are advisory."* The proposal explicitly unexported
  nothing — RFC 0074's R16 does that work. `internal/` restricts **importing**,
  not **exporting**: a new exported symbol is just as easy to add inside
  `internal/` as outside it. It would not make R16's result stick.
- *"A rename inside `checker` looks like a breaking change."* Breaking for whom?
  One repository, one consumer (`workbench`, which imports `hexal/compiler`
  only), and `go build ./...` locates every call site immediately. The
  constraint was imagined.

Against that: 101 files' import blocks rewritten, `git blame` polluted across all
of them, longer import paths throughout, and the reversal of a deliberate earlier
simplification that flattened the tree out of `internal/`.

`AGENTS.md`'s first simplification question — does this need to exist? — answers
itself.

## What delivers the actual value

RFC 0074's **R16** deletes exported symbols that have no consumer. That is a
real reduction in surface area and needs no file moves. R16 never depended on
this ADR: "unexporting is a breaking change" was never true for an unpublished
module, so nothing was blocking it.

## The trigger that would reopen this

If `hexal`'s module path becomes a fetchable one — `github.com/<owner>/hexal` or
similar — and the module is published for others to import, then `internal/`
stops being decorative and this ADR should be reconsidered on its original
argument. Until the module is externally importable, it has nothing to enforce.

## Record of the symbol census

The census taken while evaluating this proposal is worth keeping, since it
answers "how large is the stage surface" and two separate audits got it wrong.

Method: `go/ast` over every `.go` file excluding `*_test.go`, counting exported
`FuncDecl` (functions and methods), `TypeSpec`, and `ValueSpec` names as
declarations, and exported struct fields separately.

| Package | Declarations | Struct fields | Exported |
|---|---|---|---|
| `checker` | 127 | 161 | 288 |
| `types` | 174 | 68 | 242 |
| `parser` | 70 | 164 | 234 |
| `lexer` | 83 | 4 | 87 |
| `generator` | 7 | 0 | 7 |
| **Total** | **461** | **397** | **858** |

Two results worth carrying into R16:

- **Struct fields are the larger half of the surface** for `parser` (164) and
  `checker` (161). A census counting only functions and types understates
  exposure by roughly half.
- **`generator` exports 7 symbols.** It is already effectively encapsulated.

Beware `go doc -all` here — it under-enumerates in this tree — and a naive
`grep '^func [A-Z]'`, which counts `Test*` functions unless `*_test.go` is
excluded. The two fail in opposite directions.
