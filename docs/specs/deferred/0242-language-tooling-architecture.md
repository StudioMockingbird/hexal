# RFC 0242: Language Tooling Architecture

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. This records the boundaries learned from
  TypeScript, not an implementation design
- Created: 2026-09-24
- Origin: RFC 0141 and RFC 0241
- Depends on: a measured need for long-lived tooling and the incremental state
  model eventually designed by RFC 0232
- Coordinates with: RFC 0232 (parallel and incremental compilation), deferred
  RFC 0243 (stable diagnostic identity, which owns the prerequisite named in
  point 3 below), `compiler.Compile`, `docs/reference.md`
- Does not authorize: an LSP server, formatter, linter, stable diagnostic-code
  migration, state inside `compiler.Compile`, or a new syntax tree

## Why this exists

TypeScript demonstrates that compiler-backed editor tooling benefits from one
shared semantic model, but also demonstrates the cost of letting filesystem,
watch, project, compiler, and editor responsibilities blur together. Hexal has
no language server today and should not pay that cost in advance.

This RFC preserves the useful boundaries so a future tooling proposal starts
from them rather than copying TypeScript wholesale.

## Boundaries to retain

1. `compiler.Compile` remains the stateless, deterministic,
   string-in/string-out authority. It neither watches files nor retains a
   project between calls.
2. A future long-lived service may own immutable project snapshots and reuse
   unchanged parse/check facts. That service is a caller of compiler
   facilities, not a hidden mode of `Compile`.
3. Before an editor protocol is exposed, diagnostics need stable identities,
   parameter data, primary spans, and zero or more related spans. Rendered
   English remains presentation, not machine identity. The migration must be
   separately specified because current public results expose rendered
   `Stderr` strings.
4. A formatter is a separate tool or package that shares scanner/parser facts.
   Formatting policy does not enter type checking or code generation.
5. Lint rules remain outside the compiler's correctness pipeline. A future
   `hexal lint` may reuse syntax and checked facts, but a lint failure is not a
   compile failure unless a separate language rule makes it one.
6. Tooling tests use small source files with named markers and assert semantic
   requests and state transitions: completions, definitions, references,
   rename, diagnostics, formatting, and snapshot replacement. They do not
   infer correctness from process survival.
7. C source mapping remains `#line`. Editor tooling maps Hexal source directly;
   it does not introduce JavaScript-style source maps between Hexal and C.

## Questions an eventual implementation spec must answer

- Which checked and parsed values are immutable and reusable across snapshots?
- What exact source, project-option, target, and compiler-version identities
  key reuse?
- How are diagnostic identities introduced without breaking the current
  `CompilationResult.Stderr` contract? Deferred RFC 0243 owns this question and
  records the options; this RFC consumes whatever answer it reaches.
- Does formatting require a trivia-preserving syntax representation, or can it
  operate over tokens without changing the compiler AST?
- How are cancelled editor requests prevented from publishing partial state?
- What measurements justify keeping a service process alive instead of simply
  recompiling the supplied in-memory source map?

## Explicit rejections

- Do not add a separate binder or analyzer package solely to resemble
  TypeScript. Hexal's checker already owns declaration collection and semantic
  facts.
- Do not move filesystem discovery, watching, or package resolution into the
  core compiler.
- Do not make editor needs change nominal type identity or introduce
  structural typing.
- Do not add user-authored declaration files as a second Hexal interface
  language.
- Do not add a linter framework until concrete rules exist that cannot be
  expressed as ordinary compiler diagnostics.
