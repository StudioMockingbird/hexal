package types

// TargetProfileID names one compiler-owned target profile. The public
// identity is string-backed for stable serialization, diagnostics, and
// build records; arbitrary string conversion does not create a valid
// profile: only the declared constants below name qualified targets, and
// the compiler rejects every other non-empty identity before checking.
type TargetProfileID string

// TargetX86_64WindowsGNU is the x86-64 Windows profile: MinGW-w64 ABI over
// UCRT, dynamic linkage. The identity names the CRT so it can never be
// confused with an MSVC/MSVCRT target; the older ambiguous spelling is
// rejected, not retained as an alias. It remains a core C-generation target
// whose output is checked by pure-Go generated-C tests; this release has no
// native Windows driver.
const TargetX86_64WindowsGNU TargetProfileID = "x86_64-windows-gnu-ucrt"

// TargetX86_64LinuxGNU is the x86-64 Linux profile: glibc, LP64. It is the
// target of the one qualified native driver.
const TargetX86_64LinuxGNU TargetProfileID = "x86_64-linux-gnu"
