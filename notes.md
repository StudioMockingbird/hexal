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