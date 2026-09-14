# RFC 0181: Error Kinds

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented. `ErrorKind` and the updated `Error` contract are
  compiler-owned builtins; every compiler-constructed Error (File, IO,
  scheduler/Task/Channel/Mutex, WallTime) classifies through it. Verified by
  `go test ./...` and the tagged C23 suite compiling and running under GCC,
  Clang, and `zig cc`. `docs/reference.md` documents the ErrorKind/Error
  surface
- Created: 2026-09-14
- Updated: 2026-09-14
- Scope: give every `Error` one builtin, matchable classification while keeping
  application-specific failures in ordinary ADTs
- Depends on: closed RFC 0170 and its implemented File Error mapping
- Blocks: RFC 0180 and, transitively, RFCs 0172, 0173, and 0176
- Does not add: grammar, reserved words, user-defined error kinds, exceptions,
  `catch`, per-function error sets, generic Error, native error numbers as
  classification values, or text patterns in `match`

## Summary

Add a protected builtin `ErrorKind` value to every `Error`. Callers classify a
failure by matching its kind instead of comparing display text:

```hexal
result := File.open(path, FileMode.Read())
if result is Error then
    match result.kind is
    | ErrorKind.NotFound then create_defaults()
    | ErrorKind.PermissionDenied then report(result)
    | else then report(result)
    end
end
```

`Error.header` is removed as stored state. `error.header()` derives the same
bounded display header from `error.kind`. A one-off failure uses
`ErrorKind.Other(header = ...)`.

Libraries continue to model domain-specific, exhaustively handled failures as
ordinary ADTs in result unions:

```hexal
type ParseError is
    | UnexpectedToken
    | UnexpectedEnd
end

fun parse(text: String): Config | ParseError | Error do
    ...
end
```

This delivers typed classification without adding an open type, implicit
cross-type conversion, or a second form of ADT declaration.

## Motivation

Header-string comparison does not provide a checked classification contract:

```hexal
if error.header == "not foud" then
    ...
end
```

The misspelling compiles and never matches. The problem grows as File, IO,
networking, processes, and signals acquire common failure categories.

`ErrorKind` provides one compiler-checked vocabulary for failures shared across
runtime and native capabilities. Ordinary ADTs already provide the correct
closed-world mechanism for library-specific failures; duplicating that
capability through user-defined open kinds would add disproportionate language
surface.

## Source contract

### `ErrorKind`

`ErrorKind` is a protected compiler-owned nominal type. The following block is
conceptual metadata, not source syntax accepted as a declaration:

```hexal
type ErrorKind is union
    | NotFound
    | PermissionDenied
    | AlreadyExists
    | InvalidInput
    | InvalidPath
    | NotADirectory
    | IsADirectory
    | DirectoryNotEmpty
    | ReadOnly
    | Busy
    | Interrupted
    | Cancelled
    | TimedOut
    | Unsupported
    | ResourceExhausted
    | Closed
    | AddressInUse
    | AddressUnavailable
    | ConnectionRefused
    | ConnectionReset
    | ConnectionAborted
    | HostUnreachable
    | NetworkUnreachable
    | BrokenPipe
    | NotConnected
    | Other as header: Strand end
end
```

- `ErrorKind` cannot be redeclared or shadowed.
- Unit variants are constructed with the ordinary call shape, for example
  `ErrorKind.NotFound()`.
- `ErrorKind.Other(header = h)` stores one caller-supplied Strand. It is the
  only payload variant.
- `ErrorKind` is complete, finite, copyable, equality-comparable, and valid in
  every ordinary value position allowed for an equivalent ADT. This RFC does
  not expand Dict-key eligibility.
- `==` and `!=` compare variant identity. Two `Other` values are equal only
  when their header bytes are equal. `ErrorKind` has no ordering.
- `ErrorKind.header() -> Strand` returns the derived display header without
  allocation.
- `print(kind)` prints the header returned by the header derivation contract.
- The variant set may grow with native capabilities. This is source-compatible
  because every `ErrorKind` match requires a final `else`; it is not a stable C
  ABI promise.

### Matching

- A type-mode `match` whose scrutinee type is exactly `ErrorKind` uses ordinary
  ADT variant patterns.
- Every such match requires a final `else`, including a match naming every
  variant currently known to the compiler.
- Omitting it is a Type Error:

```text
match on ErrorKind requires a final else arm
```

- Duplicate patterns retain the ordinary duplicate-pattern diagnostic.
- The ordinary rejection of an `else` with no remaining value does not apply
  to `ErrorKind` because future compiler versions may add variants.
- Matching proves only the selected variant. `Other` payload data remains
  available through the enclosing Error's `header()` method; a match creates
  no implicit binding.

### `Error`

```text
Error(kind: ErrorKind, message: String) -> Error
Error.header()                         -> Strand
ErrorKind.header()                     -> Strand
```

- Protected nominal `Error` has fixed immutable fields `file: String`,
  `line: Size`, `column: Size`, `kind: ErrorKind`, and `message: String`.
- `Error(kind, message)` is the only constructor. It injects the current
  module's logical source key, line, and column and stores `kind` and `message`.
- The old `header` field is removed. `error.header()` derives the display
  header from `error.kind` without allocation.
- `Error(header, message)` is rejected with:

```text
Error requires ErrorKind as its first argument; use Error(ErrorKind.Other(header = ...), message)
```

- Copying, propagation, `try`, and `errdefer` retain their existing semantics.
  This RFC does not independently change String ownership or cleanup. Runtime
  producers use fixed static operation messages and cannot create an ownerless
  heap-allocated diagnostic message.
- A direct Error prints `file:line:column: header: message`, unchanged.
- A nested Error prints its object form in declaration order:
  `file`, `line`, `column`, `kind`, `message`. `kind` prints its header text;
  there is no duplicate `header` member.
- Error equality compares all stored fields. Header text is derived and is not
  an additional equality input. Distinct kinds remain unequal even when their
  displayed headers are byte-equal.

For example, these values are unequal:

```hexal
a := Error(ErrorKind.NotFound(), "missing")
b := Error(ErrorKind.Other(header = "not found"), "missing")
same := a == b
```

### Header derivation

- `ErrorKind.Other(header = h)` returns `h`.
- Every unit variant returns the exact fixed Strand below.
- Header strings are presentation only. Classification and equality never
  compare a unit variant's header text.
- Header derivation allocates nothing and imposes no restriction on user type
  or variant names because user-defined kinds do not exist.

| ErrorKind variant | Header |
| --- | --- |
| NotFound | `not found` |
| PermissionDenied | `permission denied` |
| AlreadyExists | `already exists` |
| InvalidInput | `invalid input` |
| InvalidPath | `invalid path` |
| NotADirectory | `not a directory` |
| IsADirectory | `is a directory` |
| DirectoryNotEmpty | `directory not empty` |
| ReadOnly | `read only` |
| Busy | `busy` |
| Interrupted | `interrupted` |
| Cancelled | `cancelled` |
| TimedOut | `timed out` |
| Unsupported | `unsupported` |
| ResourceExhausted | `resource exhausted` |
| Closed | `closed` |
| AddressInUse | `address in use` |
| AddressUnavailable | `address unavailable` |
| ConnectionRefused | `connection refused` |
| ConnectionReset | `connection reset` |
| ConnectionAborted | `connection aborted` |
| HostUnreachable | `host unreachable` |
| NetworkUnreachable | `network unreachable` |
| BrokenPipe | `broken pipe` |
| NotConnected | `not connected` |

## Runtime producers

Every compiler-constructed Error uses a builtin kind. Classification never
depends on native or libuv numbers. Runtime producers use fixed operation
messages and do not retain native numeric codes in Error.

### libuv results

One runtime mapper converts a libuv status to `ErrorKind`. It is the only
libuv status switch; RFC 0180's common mapping and File's current mapping are
reconciled into it.

| libuv result | ErrorKind |
| --- | --- |
| `UV_ENOENT` | NotFound |
| `UV_EACCES`, `UV_EPERM` | PermissionDenied |
| `UV_EEXIST` | AlreadyExists |
| `UV_EINVAL` | InvalidInput, or InvalidPath for open/path parsing |
| `UV_ENAMETOOLONG`, `UV_ELOOP` | InvalidPath |
| `UV_ENOTDIR` | NotADirectory |
| `UV_EISDIR` | IsADirectory |
| `UV_ENOTEMPTY` | DirectoryNotEmpty |
| `UV_EROFS` | ReadOnly |
| `UV_EBUSY` | Busy |
| `UV_EINTR` | Interrupted |
| `UV_ECANCELED` | Cancelled |
| `UV_ETIMEDOUT` | TimedOut |
| `UV_ENOSYS`, `UV_ENOTSUP` | Unsupported |
| `UV_ENOMEM`, `UV_ENOBUFS`, `UV_EMFILE`, `UV_ENFILE` | ResourceExhausted |
| `UV_EADDRINUSE` | AddressInUse |
| `UV_EADDRNOTAVAIL` | AddressUnavailable |
| `UV_ECONNREFUSED` | ConnectionRefused |
| `UV_ECONNRESET` | ConnectionReset |
| `UV_ECONNABORTED` | ConnectionAborted |
| `UV_EHOSTUNREACH` | HostUnreachable |
| `UV_ENETUNREACH` | NetworkUnreachable |
| `UV_EPIPE` | BrokenPipe |
| `UV_ENOTCONN` | NotConnected |
| any other result | Other with the capability's fallback header |

- File `UV_EINVAL` outside open/path parsing changes from `filesystem error`
  to InvalidInput. During open/path parsing it remains InvalidPath.
- Unmapped File failures use `Other(header = "filesystem error")`.
- A libuv failure message is the fixed operation text. It contains no numeric
  status and requires no allocation.

### Native IO

IO does not link libuv. `hexal/io.c` owns two small native mapping front-ends:

- POSIX maps `ENOENT` to NotFound; `EACCES` and `EPERM` to PermissionDenied;
  `EEXIST` to AlreadyExists; `EINVAL` to InvalidInput; `ENAMETOOLONG` and
  `ELOOP` to InvalidPath; `ENOTDIR` to NotADirectory; `EISDIR` to IsADirectory;
  `ENOTEMPTY` to DirectoryNotEmpty; `EROFS` to ReadOnly; `EBUSY` to Busy;
  `EINTR` to Interrupted; `ECANCELED` to Cancelled where provided; `ETIMEDOUT`
  to TimedOut; `ENOSYS` and each distinct value among `ENOTSUP` and
  `EOPNOTSUPP` to Unsupported; `ENOMEM`, `ENOBUFS`, `EMFILE`, and `ENFILE` to
  ResourceExhausted; `EADDRINUSE` to AddressInUse; `EADDRNOTAVAIL` to
  AddressUnavailable; `ECONNREFUSED` to ConnectionRefused; `ECONNRESET` to
  ConnectionReset; `ECONNABORTED` to ConnectionAborted; `EHOSTUNREACH` to
  HostUnreachable; `ENETUNREACH` to NetworkUnreachable; `EPIPE` to BrokenPipe;
  and `ENOTCONN` to NotConnected. Conditional constants are guarded, and
  aliases with the same numeric value never create duplicate C `case` labels.
- Windows maps `ERROR_FILE_NOT_FOUND` and `ERROR_PATH_NOT_FOUND` to NotFound;
  `ERROR_ACCESS_DENIED` to PermissionDenied; `ERROR_INVALID_HANDLE`,
  `ERROR_INVALID_PARAMETER`, and `ERROR_NEGATIVE_SEEK` to InvalidInput;
  `ERROR_BROKEN_PIPE` and `ERROR_NO_DATA` to BrokenPipe;
  `ERROR_NOT_ENOUGH_MEMORY` and `ERROR_OUTOFMEMORY` to ResourceExhausted; and
  `ERROR_OPERATION_ABORTED` to Cancelled.
- Every other native code maps to `Other(header = "IO error")`.
- The Error message is the fixed operation text. It contains no errno or Win32
  number and requires no allocation.

The front-ends must agree with the libuv mapper wherever both report the same
condition. They do not make IO link libuv.

### Non-native producers

| Failure | ErrorKind |
| --- | --- |
| IO or File capability mismatch (`not readable` / `not writable`) | PermissionDenied |
| Bytes self-read or overlapping write | InvalidInput |
| Embedded NUL in a File path | InvalidPath |
| Standard handle unavailable | NotFound |
| Task, Channel, or Mutex creation allocation failure | ResourceExhausted |
| Channel send after close | Closed |
| Wall-clock acquisition failure | Unsupported |
| Operation on a closed RFC 0180 handle | Closed |

These failures have no native code to append. Existing operation-specific
messages remain otherwise unchanged.

## Generated C

`ErrorKind` reuses the existing program-wide discriminant registry:

```c
typedef struct hex_t_ErrorKind {
    hex_tag tag;
    hex_strand other_header;
} hex_t_ErrorKind;

struct hex_t_Error {
    const hex_string *hex_m_file;
    size_t hex_m_line;
    size_t hex_m_column;
    hex_t_ErrorKind hex_m_kind;
    const hex_string *hex_m_message;
};
```

- `hex_t_ErrorKind` is the canonical generated spelling.
- Each variant receives canonical identity `builtin:ErrorKind.<Variant>` and
  preferred C tag `hex_tag_ErrorKind_<Variant>`, subject to the existing
  program-wide collision registry.
- Selecting Error or ErrorKind registers every ErrorKind variant, even when no
  source expression constructs that variant. Tag order remains deterministic.
- `other_header` is zero-filled for unit variants and stores the payload only
  for Other.
- `hex_error_kind_header` is one `static inline` switch in `hexal/error.h`.
  It returns the fixed Strand for unit tags, returns `other_header` for Other,
  and traps for any tag that is not an ErrorKind tag. Do not use a sparse table
  indexed by the general program-wide `hex_tag` enum.
- Because the switch fails closed on an invalid tag, selecting Error or
  ErrorKind also selects the one program-wide runtime-trap declaration and
  definition.
- `error.header()` and ErrorKind printing call that helper directly.
- Error construction does not copy or store a second header.
- Tag values are program-local and are not a foreign or stable ABI.
- Before RFC 0180 lands, File owns the only libuv mapper. RFC 0180 relocates
  that switch into its shared handle component; File then consumes it. At no
  point may two libuv mapping switches coexist. Native IO mapping remains owned
  by `hexal/io.c` and is emitted only with IO.
- No generated public header exposes `errno`, Windows error, or libuv types or
  constants.

## Source compatibility and migration

This is intentionally source-breaking:

- Every `Error(header, message)` becomes
  `Error(ErrorKind.Other(header = header), message)` or a named builtin kind.
- Every `error.header` access becomes `error.header()`. Classification sites
  should match `error.kind` instead of comparing the derived header.
- `Error` object layout and nested print output change because `kind` replaces
  the stored `header` field.
- `ErrorKind` becomes protected; a program using that name must rename it.
- Programs that do not construct, inspect, print, compare, or receive Error and
  do not select an Error-producing runtime component retain byte-identical
  generated output.

## Required sweep

- Remove File's header-string mapper and route it through the common ErrorKind
  mapper.
- Remove IO's `hex_io_header` construction of `errno=` and `winerr=` headers.
  Map classification from the native code, then discard the code and construct
  Error with the existing fixed operation message.
- Remove the hard-coded `Scheduler` Strand from scheduler Error construction.
- Remove the stored `header` member from the Error type model, generated
  template, member access, equality, and aggregate-print paths.
- Replace every compiler, runtime-template, snippet, fixture, and test use of
  `Error(header, message)` and `error.header`.
- Do not retain a second libuv status switch, a sparse tag-to-header table, an
  open `Kind` type, a `kind` parser branch, or user-defined kind metadata.
- Re-run the runtime-producer inventory from production sources at
  implementation time; every producer must appear in the mapping table or be
  recorded as deliberately unchanged before code changes begin.

## Coordination and ordering

- RFC 0181 lands before RFC 0180. RFC 0180's active draft consumes ErrorKind
  rather than header text and uses this RFC's allocation-free fixed-message
  contract.
- RFCs 0172, 0173, and 0176 remain blocked on RFC 0180. Their active drafts use
  ErrorKind rows and preserve operation-specific messages without independently
  choosing native diagnostic-detail storage.
- RFC 0181 owns Error representation, classification, derived headers, fixed
  operation messages, and mapping identities. RFC 0180 owns the libuv handle
  lifecycle and consumes that Error contract.
- Do not implement RFC 0180 concurrently with this RFC; both change the same
  mapper, Error construction, fixtures, and generated artifacts.

## Deferred work

- User-defined open error kinds. Ordinary ADTs remain the supported mechanism
  until concrete library experience shows that placing custom classifications
  inside builtin Error justifies new syntax and type-system machinery.
- Structured native causes or public numeric error codes.
- Payload binding in ErrorKind match patterns.
- Stable foreign ABI values for ErrorKind.

## Accepted costs

- Error gains one tagged ErrorKind value. Removing the separate stored header
  prevents the larger duplicate-header representation.
- ErrorKind registration changes shared generated artifacts for every program
  selecting Error, even when source never constructs ErrorKind directly.

## Rejected alternatives

- **User-defined `kind` declarations and open `Kind`.** They duplicate ordinary
  ADTs while adding grammar, implicit conversions, open matching, imported
  variant ambiguity, and representation-driven name limits.
- **Generic `Error<K>`.** It breaks the single-Error-member rule of `try` and
  makes ordinary fallible signatures generic.
- **Implicitly minted names.** A misspelling would silently create a new
  classification.
- **String or numeric `(domain, code)` classification.** Comparison is again
  unchecked or platform-specific.
- **Stored `Error.header`.** It duplicates information already carried by
  `kind` and materially enlarges every copied Error.
- **Native numeric codes in Error messages or private fields.** Formatting a
  code into String would allocate with no destruction owner; an inline private
  field would enlarge every Error for diagnostic data the source cannot use.
  ErrorKind plus a fixed operation message is the complete v1 diagnostic.
- **Inline fixed-capacity Error messages.** They enlarge every Error and add a
  truncation contract only to preserve platform-specific diagnostic detail.

## Detailed implementation plan

### Phase 0: freeze evidence and coordination

1. Inventory every Error constructor and runtime producer in checker,
   generator, package templates, snippets, integration tests, and tagged C23
   fixtures.
2. Record the pre-change snippet manifest and the exact catalog entries that
   select Error-producing components.
   Because selecting Error or ErrorKind registers every ErrorKind variant and
   selects the fail-closed header helper, expect every Error-selecting program
   to change its shared generated artifacts; this is part of the affected
   inventory, not an unrelated manifest regression.
3. Verify active RFCs 0180, 0172, 0173, and 0176 still consume the settled
   ErrorKind and diagnostic-detail contracts before behavioral code changes
   begin.

### Phase 1: add the type model

1. Add protected `ErrorKind` and its fixed variants in `compiler/types`.
2. Give Other its one Strand member; keep all other variants unit-only.
3. Add `kind: ErrorKind` to Error and remove `header: Strand`.
4. Update completeness, copyability, equality, printing, storability, and
   protected-name classification without changing Dict-key eligibility.

### Phase 2: check source behavior

1. Register ErrorKind constructors and `Error.header()` through the existing
   compiler-owned type/member path.
2. Change Error construction to require `(ErrorKind, String)` and own the exact
   legacy-constructor diagnostic.
3. Require final `else` for ErrorKind matches while retaining ordinary ADT
   duplicate-pattern behavior.
4. Update Error equality and nested-print discovery for the new stored fields.
5. Add no lexer token, parser production, contextual word, implicit type
   conversion, or imported-variant rule.

### Phase 3: extend canonical tags and generation demand

1. Register all ErrorKind variants when either Error or ErrorKind is reachable.
2. Add canonical builtin identities and deterministic preferred C names through
   the existing program-wide collision registry.
3. Ensure ErrorKind-only source selects `hexal/error.h`, Strand support, and the
   program-wide tag enum without selecting unrelated runtime components.

### Phase 4: update generated Error support

1. Change `compiler/generator/packages/error.h` to emit `hex_t_ErrorKind`, the
   new Error layout, and `hex_error_kind_header`.
2. Lower constructors, equality, direct print, nested print, and
   `error.header()` to the new representation.
3. Fail closed in the header switch for a non-ErrorKind tag.
4. Verify declarations precede every generated use and no duplicate ErrorKind
   definition or helper is emitted.

### Phase 5: migrate compiler-owned producers

1. Replace checker-generated Error construction and every scheduler, Channel,
   Mutex, Bytes, Time, IO, and File producer with the table's exact ErrorKind.
2. Add POSIX and Windows IO front-ends that map the native code to ErrorKind,
   discard it, and retain the existing fixed operation message.
3. Change File's existing mapper to return ErrorKind; keep its open-sensitive
   EINVAL rule. Do not create RFC 0180's not-yet-implemented shared component.
4. Verify no native mapping changes a public function signature.

### Phase 6: migrate source corpus

1. Update integration sources, generator expectations, snippets, workbench
   fixtures, and tagged C23 fixtures.
2. Replace header comparisons with kind matches unless the test specifically
   verifies `header()` presentation.
3. Preserve domain-specific failure ADTs; do not convert them into ErrorKind.

### Phase 7: verify generated artifacts

1. Run focused checker, generator, integration, and package-template tests.
2. Run all ordinary Go tests and vet without requiring an external toolchain.
3. Run tagged C23 generation/compilation/runtime fixtures when the qualified
   toolchain is available.
4. Regenerate the snippet manifest only after reviewing every moved artifact.
   No entry outside the Phase 0 affected set may change hash.

### Phase 8: synchronize canonical documents

1. Update `docs/reference.md` only during approved implementation, after
   behavior stabilizes: ErrorKind, Error fields/signature/equality/printing,
   `header()`, matching, producer mappings, native diagnostic messages, and C23
   representation ownership.
2. Remove superseded header-string contracts rather than retaining duplicate
   wording.
3. Remove the open status entry and close/archive this RFC only after code,
   tests, active dependent specs, and reference agree.

## Validation

Validation is exhaustive for this RFC.

### Source and checker

- Every ErrorKind unit constructor has type ErrorKind; Other accepts exactly
  one Strand `header` and rejects missing, extra, or wrongly typed fields.
- ErrorKind and Error cannot be redeclared or shadowed.
- ErrorKind is complete, finite, copyable, equality-comparable, non-orderable,
  printable, and no more eligible as a Dict key than an equivalent ADT.
- ErrorKind values work in bindings, parameters, results, object/ADT members,
  Array, Slice, List, and every other ordinary ADT-valid position.
- Every ErrorKind match without final `else` reports exactly
  `match on ErrorKind requires a final else arm`.
- The final-`else` rule applies only when the scrutinee type is exactly
  ErrorKind. Matching a wider union with an `ErrorKind` whole-type arm retains
  ordinary structural-union exhaustiveness rules.
- A match with final `else` compiles; duplicate variant patterns retain the
  ordinary diagnostic; a currently complete explicit variant list still
  requires `else` and does not reject that `else` as unreachable.
- `Error(ErrorKind.NotFound(), message)` has type Error and records the current
  logical module, line, and column.
- Every Strand-first Error call reports exactly the new constructor diagnostic.
- Error exposes `file`, `line`, `column`, `kind`, and `message`, exposes no
  `header` field, and provides `header() -> Strand`; ErrorKind independently
  provides the same `header() -> Strand` derivation.
- No `kind` keyword, `Kind` type, user kind declaration, implicit kind
  conversion, or three-component imported variant syntax is accepted by this
  RFC. Existing identifiers named `kind` continue to compile unchanged.

### Semantics

- Equal unit ErrorKind variants compare equal; different variants compare
  unequal; Other compares its header bytes; ErrorKind ordering is rejected.
- Two Errors differing only in kind compare unequal even when `header()` is
  byte-equal. Otherwise Error equality follows all five stored fields.
- Each unit variant and Other returns the exact header in this RFC without
  allocation. `ErrorKind.header()`, ErrorKind print, and `Error.header()` agree
  byte-for-byte.
- Direct Error output retains `file:line:column: header: message` with no
  trailing newline. Nested output contains `kind` once and no `header` member.
- `try` and `errdefer` behavior and exact-Error recognition are unchanged.
- An ordinary domain ADT in `Value | DomainError | Error` retains ordinary
  exhaustive matching and never converts to ErrorKind.

### Runtime mappings

- Every libuv row maps to the exact ErrorKind; EINVAL maps according to the
  operation-sensitive rule; every fallback uses the owning capability's exact
  Other header.
- POSIX and Windows IO mappings cover every named code and agree with libuv on
  shared conditions.
- Every native/libuv failure uses its fixed operation message; no header or
  message contains a native number and runtime construction allocates no
  diagnostic String.
- Every non-native producer uses the exact table row and retains its existing
  operation-specific message.
- File, IO, Task, Channel, Mutex, Bytes, and Time contain no legacy
  classification construction after the sweep. RFC 0180 specifies ErrorKind
  construction for its future closed-handle paths before this RFC closes.

### Generated C and artifacts

- ErrorKind-only, Error-only, File, IO, and concurrency probes emit one
  canonical `hex_t_ErrorKind` and one `hex_error_kind_header`.
- Every ErrorKind tag uses the canonical registry identity and deterministic C
  name; all variants are present whenever ErrorKind is selected.
- The header helper is a switch, not a sparse general-tag table, and its default
  traps.
- `hex_t_Error` contains no stored header and Error construction performs no
  header copy.
- Generated public headers contain no errno, Windows, or libuv type/constant.
- Cross-module Error construction, comparison, return, `try`, match, print, and
  both `header()` methods compile and run under the tagged C23 gate. File
  exercises the libuv mapper under that gate; future RFC 0180 capabilities own
  their runtime mapping fixtures.
- Ordinary tests and vet pass without an external toolchain; the tagged C23
  suite passes with the qualified toolchain.
- Snippet manifest entries outside the Phase 0 affected inventory remain
  byte-identical. Every changed entry is attributable to the ErrorKind layout,
  migrated construction/access, or an affected runtime component.
- Active RFCs 0180, 0172, 0173, and 0176 contain no load-bearing
  header-classification contract that disagrees with ErrorKind.

## Reference synchronization

Do not edit `docs/reference.md` from this proposal. Approved implementation
updates the canonical contracts named in Phase 8 after behavior stabilizes.
