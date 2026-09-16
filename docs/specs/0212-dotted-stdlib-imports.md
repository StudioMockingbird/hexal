# RFC 0212: Dotted Standard-Library Imports

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Proposed
- Created: 2026-09-16
- Scope: make compiler-owned and embedded standard-library imports visibly
  distinct from relative source-file imports
- Depends on: the current import grammar and module-resolution rules
- Does not add: direct item imports, wildcard imports, absolute filesystem
  paths, package discovery, or a second module-alias namespace

## Summary

Replace the quoted slash form for standard-library modules:

```hexal
import
    Prog from "std/program"
end
```

with a dotted virtual-module form:

```hexal
import
    Prog from std.program
end
```

Relative source modules remain quoted paths:

```hexal
import
    Math from "./math",
    Parser from "../parser"
end
```

The alias remains file-local and access remains module-qualified:

```hexal
import
    Io from std.io
end

out: Io.IO := Io.stdout()
```

## Motivation

The current resolver already gives non-relative `std/...` names virtual
standard-library meaning. A source module with the same canonical-looking
name can only be reached through a relative spelling such as
`"./std/program"`. The behavior is deterministic, but the syntax makes a
virtual module look like a filesystem path and hides the distinction at the
call site.

The dotted form makes that distinction lexical:

```text
std.program  -> virtual standard-library module
"./std/program" -> source-map module
```

## Proposed syntax

Extend an import entry so its module reference is either a quoted relative
path or a dotted standard-library path:

```ebnf
import-entry = identifier , "from" , import-module-reference ;
import-module-reference = module-path-literal | stdlib-module-reference ;
stdlib-module-reference = "std" , "." , identifier
                         , { "." , identifier } ;
```

`std` is recognized as the leading component only in the import reference
position. It is not reserved in declarations, expressions, member access, or
import aliases.

The following are valid:

```hexal
import
    Io from std.io,
    Time from std.time,
    Hash from std.crypto.hash
end
```

The following remain invalid or unchanged:

```hexal
import X from "std/io"       -- legacy spelling during migration only
import X from "./io"         -- relative source module
import X from std/io          -- invalid: stdlib references use dots
import X from std             -- invalid: a component is required
```

## Resolution

The parser records a dotted standard-library reference separately from a
quoted path. The resolver converts the dotted components to the existing
internal canonical identity by replacing dots with slashes after `std`:

```text
std.program      -> std/program
std.crypto.hash  -> std/crypto/hash
```

The existing core-library and embedded-source lookup then runs unchanged.
Generated names, nominal identity, source ownership, demand selection, and
artifact keys continue to use the slash-separated canonical identity.

Relative imports continue to resolve only through the supplied source map and
must retain their `./` or `../` spelling and quoted path literal.

## Migration

When this RFC is scheduled, choose one compatibility policy before
implementation:

1. **Clean break (recommended):** accept only dotted stdlib references and
   report a migration diagnostic for quoted `"std/..."` imports.
2. **Temporary compatibility:** accept both spellings, canonicalize them to the
   same identity, and remove the quoted form in a later language-version
   change.

The clean-break policy best preserves one obvious spelling and avoids keeping
two syntaxes for the same module identity. No source-map module is renamed by
this change; a user module formerly reached as `"./std/program"` keeps that
spelling.

## Non-goals

- Importing individual functions or types directly into the file namespace.
- Wildcard or re-export imports.
- Changing the `Alias` binding or `Alias.member` access model.
- Treating arbitrary dotted names as filesystem paths.
- Reserving `std` outside the import-reference position.
- Changing the standard-library API or its runtime components.

## Design questions for scheduling

- Should the clean-break policy or temporary compatibility policy be used?
- Should multi-component names such as `std.crypto.hash` be accepted now, or
  should the first implementation restrict the form to `std.<component>`?
- Should diagnostics print the dotted source spelling or the canonical slash
  identity?

## Implementation outline

If scheduled, update the lexer/parser import-reference production, preserve a
distinct AST/import-entry form for dotted stdlib references, map it during
reachability resolution, update the normative import grammar and semantics in
`docs/reference.md`, and add parser, module-graph, diagnostic, and generated
artifact determinism tests. No compiler runtime or generated-C component
should change.
