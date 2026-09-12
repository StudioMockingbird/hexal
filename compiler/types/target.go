package types

// TargetProfileID names one compiler-owned target profile. The public
// identity is string-backed for stable serialization, diagnostics, and
// build records; arbitrary string conversion does not create a valid
// profile: only the declared constants below name qualified targets, and
// the compiler rejects every other non-empty identity before checking.
type TargetProfileID string

// TargetX86_64WindowsGNU is the one qualified profile: x86-64
// Windows, MinGW-w64 ABI over UCRT, dynamic linkage.
const TargetX86_64WindowsGNU TargetProfileID = "x86_64-windows-gnu"
