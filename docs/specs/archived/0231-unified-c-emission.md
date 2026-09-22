# RFC 0231: Unified C Emission

- Kind: Architecture Decision Record (ADR)
- Status: Implemented. All nine Validation bullets were verified on the tree:
  zero `WriteString`/`Fprintf`/`Fprintln` sites remain under
  `compiler/generator`; the module-body write sequence in `emission.go` is the
  same twenty calls in the same order as before the migration; module-header
  fragments render through `{{define}}` blocks in `packages/module.c`; `go test
  ./...` and `go vet ./...` pass with the snippet manifest unmoved, so
  generated C is byte-identical; `missingkey=error` is still set; the eight
  plain-C files named below are byte-unchanged; and no `hex_string_128` or
  `hex_string_256` literal remains in Go source
- Created: 2026-09-22
- Scope: make generated C come from one mechanism instead of two
- Origin: a request to "use Go's `text/template` for the C code". Verified
  against the tree first: **`text/template` is already in use** for the
  component C. The genuine problem is not the engine but that a second,
  unrelated mechanism emits the rest of the C from Go string literals
- Depends on: nothing. Every figure below was measured on 2026-09-22
- Coordinates with: RFC 0230 (arc foundations), RFC 0229 (whose domain 8 asks
  a related but narrower question), RFC 0228 (whose `hex_string_128` drift
  evidence lives in the concatenation sites this RFC would remove)
- Does not change: generated C output, Hexal semantics, the compiler
  boundary, or any artifact hash

## The current state, measured

Generated C comes from **two** mechanisms.

### Mechanism 1: embedded templates — 7042 lines

`compiler/generator/components.go` embeds the package tree and parses it with
`text/template`, once per process, fail-closed:

```go
//go:embed packages/*.h packages/*.c
var packageTemplates embed.FS

instance, err := template.New(name).Option("missingkey=error").Parse(body)
```

Forty files, 7042 lines. Thirty-two contain `{{...}}` actions; eight are plain
C carried through the same machinery with no substitutions (`handle.c`,
`stash.c`, `file.h`, `handle.h`, `io.h`, `print.h`, `seek.h`, `stash.h`).

This mechanism is sound. `missingkey=error` makes a model/template mismatch a
build-time panic rather than an empty string in generated C, and the C is
readable as C.

### Mechanism 2: string concatenation in Go — 255 sites

Twenty-five Go files build C with `WriteString` and `Fprintf`:

| File | Sites |
|---|---:|
| `print.go` | 64 |
| `concurrency.go` | 39 |
| `emission.go` | 32 |
| `unions.go` | 16 |
| `adt.go` | 14 |
| `bitwise.go` | 11 |
| `heap.go` | 10 |
| `declarations.go` | 8 |
| 17 more files | 61 |

A representative line, from `print.go:216`:

```go
fmt.Fprintf(result, "static void hex_print_nested_%s(hex_print_buffer *out, const void *value) {\n    hex_string_128 text = hex_error_kind_header(*(const hex_t_ErrorKind *)value);\n    hex_print_text(out, text.data, text.byte_length);\n}\n", typ.CName)
```

That is four lines of C written as one Go string with escaped newlines. It has
no syntax highlighting, no structure a reader can scan, and no way to diff a C
change as C.

## The problem

Not the engine — the split.

Because half the C lives in Go strings, the `packages/` tree does not deliver
what separating C into files was supposed to deliver. A maintainer changing
generated C still has to know which of two places owns the fragment, and the
half that lives in Go is the half that is hardest to read.

The split also has a measurable cost already recorded elsewhere: the fourteen
hard-coded `hex_string_128` and `hex_string_256` spellings that RFC 0228 cites
as its strongest drift evidence are **all** in concatenation sites. A template
with a model field could not have drifted from the constant; a Go string
literal did.

## Where the 255 sites actually emit

This determines the migration shape, and it is not what "move the C into
`packages/`" first suggests.

The `packages/` tree holds **program-wide component** artifacts. The 255
concatenation sites almost all emit **per-module** C instead: helpers and
specializations that depend on one module's own types, assembled into that
module's header by a single sequence in `emission.go`:

```go
writeUnionForwardDeclarations(&result, input.unions)
writeNominalBodies(&result, input.objects, input.adts, input.unions, ...)
writeUnionDefinitions(&result, input.unions, input.tags)
writeModuleCollectionSpecializations(&result, &input)
writeHeapAllocateHelpers(&result, input.heaps)
writeStashHelpers(&result, input.stash)
writePrintDefinitions(&result, input.printState, input.tags)
writeEqualityDefinitions(&result, input.equality, input.tags)
...
```

Twenty-two such calls, in one `strings.Builder`, and **the order is
load-bearing**. The code says so: collection specializations "follow their
element definitions", and the heap helpers "reference module-owned element
types, so they follow the object definitions". C requires a declaration before
its use, so this sequence is a correctness constraint, not a style.

Two consequences:

- The destination is a **module-header template category**, which does not
  exist today; `packages/` cannot hold it, because a component artifact is
  program-wide and cannot declare per-module types.
- Any migration must preserve that ordering exactly.

`componentArtifact` already carries a `block` field for rendering a
`{{define}}` sub-template, but **no template in the tree uses `{{define}}`
today**, so this RFC is the first consumer of that mechanism.

## Decision: Direction A, migrated per writer function

Consolidate onto templates. The alternative — moving 7042 template lines into
Go string literals — was considered and rejected: it would degrade the
readable half to make it consistent with the unreadable half.

**The migration unit is one `write*` function, not one file.** Each keeps its
exact position in `emission.go`'s sequence; only its body changes, from
concatenation to building a model and executing a template:

```go
// before
func writePrintNestedHelper(result *strings.Builder, typ compilerTypes.Type, tags *tagRegistry) {
	switch {
	case compilerTypes.IsErrorKind(typ):
		fmt.Fprintf(result, "static void hex_print_nested_%s(...) {\n    hex_string_128 text = ...;\n}\n", typ.CName)
	// ...
	}
}

// after: the switch stays in Go and decides; the template only substitutes
func writePrintNestedHelper(result *strings.Builder, typ compilerTypes.Type, tags *tagRegistry) error {
	model := nestedPrinter{CName: typ.CName}
	switch {
	case compilerTypes.IsErrorKind(typ):
		model.Body = printErrorKindBody
	// ...
	}
	return renderInto(result, "print.h", "nested_printer", model)
}
```

This shape is what keeps the ordering invariant safe: the sequence in
`emission.go` is untouched, so a converted function emits in the same place
with the same neighbours. It also makes each function independently
verifiable, which the whole-file alternative would not.

### Two boundaries this must respect

Or it makes things worse rather than better.

- **Branching stays in Go.** A `switch` over type predicates is clearer than
  nested `{{if}}` actions. The model arrives at the template already decided —
  a `Body` string or a chosen variant — so templates stay substitution, not
  logic. Same line RFC 0230 Decision 6 draws for component demand.
- **Per-specialization output uses `{{range}}` over a model slice**, never a
  template per specialization.

The honest cost: the 255 sites are mostly specialization-driven, one output
per concrete type chosen by a type-predicate `switch`. Converting them means
building a model that carries what the switch decides inline. That is real
work, and it is the reason this is a per-function migration with a
byte-identical proof rather than a bulk rewrite.

## Migration shape

One `write*` function per change, in descending site density so the largest
readability win lands first. The `emission.go` sequence is never reordered.

1. `print.go` (64 sites, chiefly `writePrintDefinitions` and its helpers)
2. `concurrency.go` (39)
3. `emission.go`'s own 32, which are the module-header shell itself
4. `unions.go`, `adt.go`, `bitwise.go`, `heap.go`, `declarations.go` (59)
5. the remaining 17 files (61)

Step 3 is the one to sequence carefully: `emission.go` both *assembles* the
module header and *emits* part of it, so converting its own sites changes the
file that orders every other converted function. Do it after steps 1-2 have
proven the pattern and before step 4 spreads it widely.

Each function is its own change. **The proof for every one is the same: generated
C is byte-identical and the snippet manifest does not move.** That is an
unusually strong verification condition — this refactor has no legitimate
reason to alter a single byte of output — and it means a wrong step is caught
immediately rather than at review.

The eight plain-C files stay as they are. They contain no actions, they are
already readable, and converting them would change nothing.

## Relationship to the rest of the arc

This RFC is independent of RFC 0228 and RFC 0229 and can land in any order
against them, with one interaction worth noting: it **removes the fourteen
hard-coded C spellings** that RFC 0228 cites. If this lands first, RFC 0228's
generated-C drift evidence is already fixed and its scope narrows to the
eleven tunable declarations in the ledger.

It also makes RFC 0229's domain 8 question easier to answer, without
answering it. Once all C is in templates, "render a layout from a record"
means passing a field list to an existing template rather than replacing a
mechanism — so the comparison that question calls for becomes a small
experiment instead of an architectural bet.

## Non-goals

- Changing generated C output in any way.
- A template engine other than `text/template`.
- Moving logic into templates.
- Converting the eight plain-C files.
- Touching the runtime C that is not generated.

## Validation

This ADR is implemented when:

- every generated C fragment comes from a template, and no non-test Go file in
  `compiler/generator` writes C text with `WriteString` or `Fprintf`;
- the `emission.go` call sequence that orders module-header sections is
  unchanged in order, so every declaration still precedes its use;
- module-header fragments render through `{{define}}` blocks, this RFC being
  that mechanism's first consumer;
- generated C is **byte-identical** for every catalog snippet, and the snippet
  manifest is unchanged, at every step of the migration;
- template models carry decided values; no `{{if}}` chain replaces a Go
  `switch` over type predicates;
- `missingkey=error` remains set, so a model/template mismatch stays a
  build-time failure;
- no `hex_string_128` or `hex_string_256` literal remains in Go source;
- the eight plain-C files are unchanged;
- `go test ./...` and `go vet ./...` pass at every step.

## Migration record

The last seventeen literals were the five test files this RFC waived:
`compiler/tests/integration/error_test.go` and `dict_test.go`,
`compiler/generator/error_component_test.go`, `dict_component_test.go`, and
`inline_string_test.go`. Each expected-`hex_string_128`/`hex_string_256`
spelling is now built from `compilerTypes.ErrorHeaderText.CName` and
`compilerTypes.ErrorMessageText.CName`; no assertion was weakened, and the five
files' tests pass against the same expected bytes as before.

Two figures in this RFC did not survive contact with the migration:

- **Site count.** This RFC counted 255 sites across 25 Go files. The migration
  converted 334 sites across 29 non-test files. The difference is class, not
  drift: the original count excluded `fmt.Sprintf` fragments and Go-side
  fragment builders, which are still C text built in Go, and which the
  migration's own zero-write-site gate therefore had to include.
- **Plain-C files.** Still exactly eight, and still byte-unchanged
  (`handle.c`, `stash.c`, `file.h`, `handle.h`, `io.h`, `print.h`, `seek.h`,
  `stash.h`).

Incidents the migration produced and the gates that caught them, recorded
because each is a way this mechanism can fail quietly:

- A fragment rendered from the wrong template file truncated output silently.
  Only the snippet manifest caught it; no Go test and no C compiler did. This
  is the strongest available argument for keeping the manifest as the
  generator's regression net.
- Two `buildNumericModel` error returns were dropped instead of propagated.
- A template model field name did not match its template's reference, which
  `missingkey=error` turned into a build-time failure rather than an empty
  string in generated C.
- Two byte-level incidents, one around a closing helper brace and one around a
  blank line, both caught by the manifest.
