# Seawitch Lang

Seawitch is a high-level "syntax sugar" language with Lua-like syntax and a C23 compilation target. It aims to be a "better c", with close mapping of c concepts but with some modern niceties like;

- Sum types
- Match expressions
- No function closures
- Methods on objects
- Generics
- in built datastructures like `List<T>`, `Dict<T, U>`
- in built memory allocation primitives like general purpose alocator (sack!), Arena and Pool
- Zig's allocator passing model

## Critical Directives

- **Execution permission.** A request to implement, fix, or update grants
  permission to edit files and canonical docs; do not ask again. Ask only when
  the request was analysis, exploration, a spec, or a plan, or when an
  unresolved design decision would change semantics.
- **Crisp communication.** Terse, token-minimal, losing no key information. No
  fluff; code samples where they explain better than prose.
- **Clarify ambiguity.** When a request is ambiguous about user-visible
  behavior, ask one focused question before implementing.
- **Workbench validation.** Once code implementation is complete, rebuild the workbench
  binary into `bin/` and restart the running workbench before handoff.
- **Pushback.** When a request is wrong or a poor fit for Seawitch, push back with
  evidence and rationale.
- **Architecture and style.** Align the compiler's architecture and coding
  style with the 'Clox' compiler tutorials in *Crafting Interpreters* by Robert
  Nystrom.
- **Code is documentation.** Keep code well-commented, clearly stating what it
  does and why. Add detailed comments in complex areas.
- **C23 is also the refernce** When in doubt, always try to align to how C does things and keep it simple.

- **Simplify.** Before adding an implementation, ask:
  1. Does this need to exist? If not, skip it (YAGNI).
  2. Does this already exist in the codebase? Reuse it; do not rewrite it.
  3. Does the standard library provide it? Use that.
  4. Does the native platform provide it? Use that.
  5. Is there an installed dependency that provides it? Use that.
  6. Can it be one line? Keep it to one line.
  7. Only then, implement the minimum that works.

## Language Goals & Philosophy

1. Seawitch should feel intuitive.
2. There should only be one obvious way to do things.
3. The language surface must remain small and clean.
4. Prioritize simplicity, elegance, expressiveness, and composability.
5. Seawitch is a high-level systems programming language. Users should be able to
   do everything with Seawitch that they can do with C.
6. Static typing.
7. Compiles to human-readable, formatted C23 with `#line` source mapping back
   to Seawitch.
8. Can import C code and headers.
9. Trivial import of C libraries and tools.
10. Incremental compiler. Only recompile the module that changed.
11. Fast compilation.
12. Trivial concurrency and multithreading.
13. Low ceremony: minimize boilerplate and syntactic noise.
14. Promote readability over compactness and complicated instructions.
15. No runtime overhead.
16. No undefined behavior.
17. If it compiles, it runs.
18. If it compiles, it has no memory issues.
19. Like Crystal, everything here is also an object.

## Architecture

- Forward-only pipeline with clean separation of concerns.
- **Fail-closed architecture.** Any invalid or unsupported program must fail
  with a diagnostic. If the compiler cannot classify the failure, report an
  `Unknown Error` and make clear that the problem is in the compiler, not the
  user's program.
- Analyzer and code-generation dispatch must handle every supported syntax
  node explicitly.
- The current literal-only compiler may pass checked statements directly to
  code generation. Do not add an empty analyzer pass; introduce the analyzer
  when operators, conversion lowering, or ownership analysis first requires an
  analyzed representation distinct from checked syntax.
- **Earliest diagnostic ownership.** Define each compilation error at the
  earliest phase that can prove it. If the same source construct could produce
  errors in multiple phases, keep the earliest diagnostic and do not let later
  phases reinterpret or duplicate it. A later phase may report independent
  errors only after it receives valid output from the preceding phase.
- Unsupported syntax must produce a structured diagnostic; never return an
  empty result, emit a placeholder comment, or silently omit output.
- New features mirror the closest existing implementation—structure, naming,
  diagnostics, and test kinds—before inventing new ones.
- Prefer plain loops, switches, and direct data over frameworks and layers of
  indirection. When two solutions differ in complexity, take the simpler one.
- The same standard applies to generated C23: humans maintain and debug it, so
  it should remain as plain as the compiler source.
- The generated C23 must always try to use the latest features released for C, as long as the latest gcc & clang versions support that.

## Documentation

Keep these three canonical documents updated once per feature, after behavior
stabilizes—not repeatedly while it is still changing:

1. `docs/grammar.md` — formal grammar in EBNF format.
2. `docs/language.md` — cross-cutting rationale, semantic rules, and key
   language decisions that cannot be inferred easily from the code.
3. `docs/status.md` — project checklist and known follow-ups.

## Workflow

Choose the lightest workflow that fits the task:

- **Small (<50 LOC):** Implement directly.
- **Medium:** Write a brief implementation plan.
- **Large** (new feature, parser, type checker, or runtime):
  1. Write a full specification.
  2. Review the design.
  3. Write an implementation plan.
  4. Get explicit user sign-off.
  5. Follow test-driven development.
  6. Implement the code.
  7. Verify the result.
  8. Set the spec's `Status:` to `Implemented`, and delete its execution plan.
     A finished plan left in place is indistinguishable from unstarted work.

## Testing

- Keep unit tests light. Add only the focused coverage fundamentally required
  to validate an individual compiler stage or helper.
- Integration tests live in `compiler/`, one file per language facet, named for
  the facet (`pointers_test.go`, `operators_test.go`). Never name a test file
  after a spec, and never put a spec number in a test function name — cite the
  spec in a header comment instead. Together these files must verify the public
  compiler behavior end to end.
- `go test ./...` must pass with no external toolchain installed. Tests that
  need gcc or clang belong behind the `c23` build tag; see spec 0013.
- Intentional overlap between unit and integration tests is expected: unit
  tests isolate stage behavior, while integration tests confirm that the same
  behavior survives the complete compilation pipeline.

## Spec format

All specs live in `docs/specs/`, one flat number sequence, `NNNN-kebab-name.md`.
Numbers are permanent identifiers: never renumber, never reuse. Each spec names
its kind in the header:

- Feature Specifications: Rust-Style RFC
- Language Semantics: ISO/IEC Language Standard Format
- Architecture Decisions: ADR (Architecture Decision Record)
- Execution Plans: named `...-plan.md`, header links the spec they implement

- Note: a Spec once closed is immutable. Do not make any changes to it, even if the feature it refers to is updated.
