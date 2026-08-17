Do a full audit of the codebase to find opportunities for improvements/refactoring. use parallel subagents where possible.
These are the areas of focus

| # | Refactoring pass | Focus | Outcome | Risk | RoI |
|---:|---|---|---|---|---|
| 1 | Line-ending and `gofmt` hygiene | Normalize formatting and line endings. | Clean, reviewable baseline. | Low | High |
| 2 | Dead-code removal | Delete obsolete and unused code. | Smaller, clearer codebase. | Low | High |
| 3 | Legacy API removal | Remove deprecated APIs confirmed by evidence. | Fewer compatibility paths. | Medium | High |
| 4 | Determinism audit | Stabilize ordering in diagnostics, modules, maps, and generated files. | Reproducible output. | Medium | High |
| 5 | Diagnostic centralization | Consolidate diagnostic construction and formatting. | Consistent error behavior. | Medium | High |
| 6 | Naming consistency | Normalize symbols, files, types, helpers, and tests. | Easier navigation and maintenance. | Low | Medium |
| 7 | Giant-file splitting | Split files by compiler phase and responsibility. | Clearer architecture. | Medium | High |
| 8 | Parameter bundling | Replace repeated parameter groups with focused structs. | Simpler function signatures. | Medium | Medium |
| 9 | Exported-API documentation | Document the final public API shape. | Better usability and discoverability. | Low | Medium |
| 10 | Error-handling unification | Normalize propagation, classification, and returns. | Predictable failure paths. | Medium | High |
| 11 | Standard-library modernization | Replace custom utilities with standard facilities. | Less code and maintenance. | Low | Medium |
| 12 | Phase-dispatch audit | Verify parser, checker, and generator coverage of every node. | Explicit fail-closed behavior. | Medium | High |
| 13 | Import-resolution single source | Consolidate module graph and resolution logic. | One authoritative resolution path. | High | High |
| 14 | Literal-registry invariant | Centralize builtin literal/type registration. | Eliminates nil-guard bug classes. | Medium | High |
| 15 | Builtin interning unification | Unify builtin identity and add regressions. | Stable builtin behavior. | High | High |
| 16 | Ownership and cleanup audit | Review pointers, allocation, views, `defer`, and `errdefer`. | Clearer lifetime guarantees. | High | High |
| 17 | Test-helper consolidation | Remove duplicate fixtures and assertions. | Shorter, more maintainable tests. | Low | Medium |
| 18 | Reference synchronization | Align code, `reference.md`, specs, and `status.md`. | No contract drift. | Medium | High |
| 19 | Allocation and hot-path optimization | Optimize only measured bottlenecks. | Better performance without premature complexity. | High | Medium |
| 20 | Final conformance pass | Run tests, snippets, diagnostics, and generated-C comparisons. | Verified behavior-preserving refactor. | Low | Very high |








Yes. Six buckets give each spec a coherent purpose while preserving execution order.

| Bucket | Passes | Spec focus | Prompt to create the spec |
|---|---|---|---|
| 1. Safe Cleanup Baseline | 1–3 | Formatting, dead code, legacy APIs. | “Create a refactoring spec for repository hygiene, dead-code removal, and legacy API removal. Preserve behavior, establish a clean baseline, and require tests after every deletion.” |
| 2. Deterministic Compiler Surface | 4–6 | Determinism, diagnostics, naming. | “Create a refactoring spec for deterministic compiler behavior, centralized diagnostics, and naming consistency. Require stable output, preserved diagnostic meaning, and regression coverage.” |
| 3. Structural Simplification | 7–9, 11 | File splits, parameter bundling, API comments, stdlib modernization. | “Create a refactoring spec for restructuring the compiler into clear responsibility-based files, simplifying signatures, documenting exported APIs, and replacing unnecessary custom utilities with standard-library equivalents.” |
| 4. Pipeline and Semantic Invariants | 10, 12–15 | Error handling, phase dispatch, imports, literals, builtin interning. | “Create a refactoring spec for compiler pipeline and semantic invariants. Centralize error handling, make parser/checker/generator dispatch explicit, unify import resolution, enforce literal registration, and unify builtin interning without changing language semantics.” |
| 5. Lifetime, Tests, and Documentation | 16–18 | Ownership, cleanup, test helpers, reference synchronization. | “Create a refactoring spec for ownership and cleanup consistency, test-helper consolidation, and synchronization between implementation, `reference.md`, specs, and `status.md`.” |
| 6. Performance and Conformance | 19–20 | Allocation/hot paths and final validation. | “Create a final refactoring spec for evidence-based allocation and hot-path optimization followed by full conformance validation. Require benchmarks or profiling evidence and no behavior regressions.” |

Recommended dependency chain:

```text
0057 Safe Cleanup
  → 0058 Deterministic Compiler Surface
  → 0059 Structural Simplification
  → 0060 Pipeline and Semantic Invariants
  → 0061 Lifetime, Tests, and Documentation
  → 0062 Performance and Conformance
```

Each spec should include:

- scope and non-goals
- affected packages
- behavior-preservation invariants
- implementation steps
- required tests
- documentation requirements
- completion criteria
- explicit follow-up dependencies