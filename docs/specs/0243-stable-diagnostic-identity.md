# RFC 0243: Stable Diagnostic Identity

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. This records the problem, the constraint
  that shapes any solution, and the decisions an implementation RFC must make.
  It authorizes no migration of the 1,423 existing emission sites
- Created: 2026-09-24
- Origin: RFC 0241's finding T1, split out because deferred RFC 0242 names
  stable diagnostic identity as a prerequisite for an editor protocol while
  explicitly not authorizing it — its "Does not authorize" line covers "stable
  diagnostic-code migration", and its open questions ask how identities are
  introduced without breaking `CompilationResult.Stderr`. Nothing owned the
  work in between
- Depends on: nothing. Every mechanism considered here extends code that ships
- Coordinates with: archived RFC 0229 (data-driven compiler facts, whose
  Decision 7 constrains the shape), archived RFC 0238 (declarative fact
  guards, whose pattern becomes applicable once identities exist), deferred
  RFC 0242 (language tooling), RFC 0241 (the review that found this)
- Does not update `docs/reference.md`: nothing here is language-visible yet.
  An implementation that renders a code in diagnostic output would change the
  diagnostic contract and must synchronize the reference at that point

## The problem

A Hexal diagnostic has no identity. `compiler/types/types.go:251` renders

```go
"[" + string(diagnostic.Category) + "] " + diagnostic.Message + location
```

and `Message` is a string composed at the emitting site:

```go
diagnostics = append(diagnostics, nameErrorAt(declaration.Name,
    declaration.Name.Lexeme+" is a protected built-in name"))
```

The only handle anything has on a diagnostic is its rendered English. Measured
in this tree: **1,423 emission sites**, and roughly **1,192 distinct message
literals in `compiler/checker` alone**.

Three costs follow, and all three are live rather than hypothetical.

**Rewording a message silently breaks every consumer.** Tests, specs, and
documentation quote message text because there is nothing else to quote. During
the RFC 0163 and RFC 0165 work three separate specs were found quoting
diagnostics that had since been reworded — `use rune_cursor()` had become
`use bytes() for indexed byte access` after RFC 0224, and a `.value`-era null
message had become a dereference message after RFC 0161. Each was a silent
documentation defect that only a manual re-probe caught. An identity would have
made those references stable across the rewording.

**A user cannot name a diagnostic.** There is no way to search for one, to
report one without pasting a sentence, or to suppress one. `TS2345` is a thing
a person can look up; `[Type Error] this pointer's storage was released on
every path to this point` is prose.

**The diagnostic surface cannot be enumerated.** There is no list of what the
compiler can report, so no guard can check one. Archived RFC 0238 established
the pattern that closes exactly this class of gap — assert that every spelling
used in code corresponds to a declared record and vice versa — and it cannot
reach diagnostics, because there is nothing declared to compare against. Every
other comparable surface in the compiler now has such a guard; diagnostics are
the largest one that does not.

## The constraint that shapes any solution

**Wording does not move into a catalog.** Archived RFC 0229's Decision 7
deliberately keeps diagnostic wording with the emitting phase, and RFC 0241's
T1 reaffirmed it after seeing TypeScript's alternative: a 2,213-entry JSON
catalog generating 6,645 lines of Go, where Go identifiers and localization
keys are derived from English text and placeholder mistakes surface at runtime.

That constraint is usually read as blocking this work. It does not, because
**identity and wording are separable**, and separating them is what RFC 0229
already does everywhere else.

RFC 0229's own formulation is that data selects and describes supported cases
while behaviour stays in explicit code. A diagnostic's *identity* — that this
compiler can report a released-pointer read, that the identity is stable, that
it belongs to the Type Error category — is a compiler-owned fact of exactly the
kind `compiler/specdata` already holds for error kinds, components, operators,
and type constructors. A diagnostic's *wording* is the emitting phase's
business, because only that phase knows which binding, type, or operation to
name.

So the shape this RFC points at is a registry of identities with no text in it,
consumed at emission sites that keep composing their own messages. That is
additive to Decision 7 rather than a reversal of it, and it is the reason this
RFC exists separately from the catalog question RFC 0241 declined.

## What an implementation would have to decide

Deferred deliberately: none of these is urgent, and answering them cheaply now
would cost more than waiting.

### 1. Does the identity appear in rendered output?

The question deferred RFC 0242 raises, since `CompilationResult.Stderr` is a
public contract and the snippet manifest and every integration test read it.

- **Internal only.** Identity exists for tests, tooling, and guards; rendered
  text is unchanged; nothing breaks. Users still cannot name a diagnostic, so
  two of the three costs above remain.
- **Rendered**, as `[Type Error HX1002] message at file:line:col`. Users gain a
  searchable handle. Every test asserting on full diagnostic text changes once,
  and `docs/reference.md`'s diagnostic contract moves.
- **Rendered behind an option.** Both, at the cost of two output shapes to
  keep consistent.

### 2. What is the identity?

A numeric code, a string key, or both. TypeScript carries both and the string
key is what survives serialization. A number is shorter to type and to search;
a key is self-describing in a test assertion. Whichever is chosen, it must be
stable across rewording — that is the entire point — and therefore must never
be derived from the message text, which is how TypeScript's derivation of Go
identifiers from English creates churn.

### 3. Is migration incremental or total?

1,423 sites is a large mechanical change with no user-visible benefit until
question 1 is answered in favour of rendering. An incremental adoption — where
newly added diagnostics carry identity and existing ones acquire it as they are
touched — is cheaper but leaves the enumeration guard unable to assert
completeness, which is most of the value. A total migration makes the guard
exact and is one large diff.

### 4. Where does the registry live?

`compiler/specdata` is the established home for compiler-owned facts and
already carries the integrity pass that would validate a diagnostic registry.
The alternative is a package closer to `compiler/types`, where `Diagnostic`
lives. This is a placement decision, not a design one.

### 5. Do related spans come with it?

Deferred RFC 0242 lists "stable identities, parameter data, primary spans, and
zero or more related spans" as one prerequisite bundle. Hexal already has
primary spans from archived RFC 0221. Related spans and structured parameters
are separable from identity and may belong to whichever RFC first needs them;
this RFC does not claim them.

## Non-goals

- A central wording catalog. See The constraint that shapes any solution.
- Localization. It is the usual motivation for a catalog and Hexal has no
  requirement for it; nothing here should be justified by a future translation
  that may never be wanted.
- Warnings, suppression, or any severity below an error. Hexal has one error
  class per category and no warning concept; adding suppression would be a
  language-surface decision with its own spec.
- Changing when or where a diagnostic is produced, or which phase owns it.
- An editor protocol, which deferred RFC 0242 owns.

## Why this is deferred rather than scheduled

The cost is real but slow-acting: stale quotations in documentation, an
unenumerable surface, and users who cannot name what they are seeing. None of
it blocks compilation, and the work is a 1,423-site mechanical change whose
main beneficiary — structured tooling — does not exist yet.

The condition that should reactivate it is the first of these to become true:

- an editor protocol or other structured consumer is specified, at which point
  deferred RFC 0242 requires identity as a prerequisite;
- a decision to render codes to users, which makes the benefit immediate;
- or a third instance of a reworded diagnostic silently invalidating recorded
  documentation, which would establish the churn cost as recurring rather than
  incidental. Two are already on record.

## Validation

Non-exhaustive. A promoted implementation RFC must replace this with an
exhaustive Validation section, which will depend on the answers to the
decisions above. The shape it must take:

- Every diagnostic the compiler can emit has an identity that is stable across
  rewording of its text.
- A guard in archived RFC 0238's pattern fails when an emission site uses an
  identity no registry entry declares, and when a registry entry names an
  identity no site emits.
- Rewording a message changes no test that asserts on identity.
- Diagnostic wording remains at the emitting site, and no message text moves
  into a registry.
- Generated C is byte-identical and the snippet manifest moves no hash.
- If identity is rendered, `docs/reference.md`'s diagnostic contract is updated
  in the same change and every consumer of `CompilationResult.Stderr` is
  migrated deliberately rather than incidentally.
