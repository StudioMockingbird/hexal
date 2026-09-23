package specdata

import "fmt"

// Error-kind facts: the variant identities of the protected builtin
// classification every Error carries, their declaration order, which one
// variant takes a caller-supplied header, and the constructor that header
// type belongs to.
//
// Wording stays out. A variant's display header is authored text the phase
// that renders it owns, and the header capacity is tunable configuration, so
// neither crosses this boundary. This package imports no compiler package, so
// the header's type is a constructor identifier, never a Type.

// ErrorKindID names one ErrorKind variant. The source spelling is the identity:
// it is the generated tag suffix and the match-declaration name, so a second
// spelling would be a second variant.
type ErrorKindID string

// The ErrorKind variant identities, in declaration order. Order is load-bearing:
// tag registration, ErrorKind.header() derivation, and match declaration order
// all agree on the sequence of errorKinds.
const (
	ErrorKindNotFound           ErrorKindID = "NotFound"
	ErrorKindPermissionDenied   ErrorKindID = "PermissionDenied"
	ErrorKindAlreadyExists      ErrorKindID = "AlreadyExists"
	ErrorKindInvalidInput       ErrorKindID = "InvalidInput"
	ErrorKindInvalidPath        ErrorKindID = "InvalidPath"
	ErrorKindNotADirectory      ErrorKindID = "NotADirectory"
	ErrorKindIsADirectory       ErrorKindID = "IsADirectory"
	ErrorKindDirectoryNotEmpty  ErrorKindID = "DirectoryNotEmpty"
	ErrorKindReadOnly           ErrorKindID = "ReadOnly"
	ErrorKindBusy               ErrorKindID = "Busy"
	ErrorKindInterrupted        ErrorKindID = "Interrupted"
	ErrorKindCancelled          ErrorKindID = "Cancelled"
	ErrorKindTimedOut           ErrorKindID = "TimedOut"
	ErrorKindUnsupported        ErrorKindID = "Unsupported"
	ErrorKindResourceExhausted  ErrorKindID = "ResourceExhausted"
	ErrorKindClosed             ErrorKindID = "Closed"
	ErrorKindAddressInUse       ErrorKindID = "AddressInUse"
	ErrorKindAddressUnavailable ErrorKindID = "AddressUnavailable"
	ErrorKindConnectionRefused  ErrorKindID = "ConnectionRefused"
	ErrorKindConnectionReset    ErrorKindID = "ConnectionReset"
	ErrorKindConnectionAborted  ErrorKindID = "ConnectionAborted"
	ErrorKindHostUnreachable    ErrorKindID = "HostUnreachable"
	ErrorKindNetworkUnreachable ErrorKindID = "NetworkUnreachable"
	ErrorKindBrokenPipe         ErrorKindID = "BrokenPipe"
	ErrorKindNotConnected       ErrorKindID = "NotConnected"
	ErrorKindOther              ErrorKindID = "Other"
)

// ErrorKindSpec is one ErrorKind variant. A variant is either a unit variant,
// Unit true and no HeaderType, or the one payload-carrying variant, Unit false
// with HeaderType set. HeaderType is a type-constructor identifier, never a
// specialization: the header is a bounded text value, and the capacity that
// specializes the constructor is configuration the type layer applies, not a
// fact stored per variant.
type ErrorKindSpec struct {
	ID         ErrorKindID
	Unit       bool
	HeaderType TypeID
}

// errorKinds is the registry in declaration order. It is unexported so no
// importer can rewrite a record; a record owns no slice, so a query result is
// already independent.
var errorKinds = []ErrorKindSpec{
	{ID: ErrorKindNotFound, Unit: true},
	{ID: ErrorKindPermissionDenied, Unit: true},
	{ID: ErrorKindAlreadyExists, Unit: true},
	{ID: ErrorKindInvalidInput, Unit: true},
	{ID: ErrorKindInvalidPath, Unit: true},
	{ID: ErrorKindNotADirectory, Unit: true},
	{ID: ErrorKindIsADirectory, Unit: true},
	{ID: ErrorKindDirectoryNotEmpty, Unit: true},
	{ID: ErrorKindReadOnly, Unit: true},
	{ID: ErrorKindBusy, Unit: true},
	{ID: ErrorKindInterrupted, Unit: true},
	{ID: ErrorKindCancelled, Unit: true},
	{ID: ErrorKindTimedOut, Unit: true},
	{ID: ErrorKindUnsupported, Unit: true},
	{ID: ErrorKindResourceExhausted, Unit: true},
	{ID: ErrorKindClosed, Unit: true},
	{ID: ErrorKindAddressInUse, Unit: true},
	{ID: ErrorKindAddressUnavailable, Unit: true},
	{ID: ErrorKindConnectionRefused, Unit: true},
	{ID: ErrorKindConnectionReset, Unit: true},
	{ID: ErrorKindConnectionAborted, Unit: true},
	{ID: ErrorKindHostUnreachable, Unit: true},
	{ID: ErrorKindNetworkUnreachable, Unit: true},
	{ID: ErrorKindBrokenPipe, Unit: true},
	{ID: ErrorKindNotConnected, Unit: true},
	{ID: ErrorKindOther, HeaderType: TypeInlineString},
}

// ErrorKinds returns every variant record in declaration order as a copy.
func ErrorKinds() []ErrorKindSpec {
	return append([]ErrorKindSpec(nil), errorKinds...)
}

// ErrorKind resolves one variant by identity.
func ErrorKind(id ErrorKindID) (ErrorKindSpec, bool) {
	for _, spec := range errorKinds {
		if spec.ID == id {
			return spec, true
		}
	}
	return ErrorKindSpec{}, false
}

// ErrorKindIndex returns the declaration index of id, or -1 if no record
// declares it. Declaration order is the index's only meaning.
func ErrorKindIndex(id ErrorKindID) int {
	for index, spec := range errorKinds {
		if spec.ID == id {
			return index
		}
	}
	return -1
}

// validateErrorKinds rejects a registry that cannot be trusted: an empty or
// repeated identity, a payload count other than exactly one, a variant that
// declares both unit and payload facts or neither, and a payload header type
// that names a constructor the constructor registry does not declare. A
// consumer resolves the payload header through the constructor registry, so an
// undeclared constructor is a compiler-development defect rather than a
// silently ignored record.
func validateErrorKinds() error {
	seen := make(map[ErrorKindID]bool, len(errorKinds))
	payloads := 0
	for _, spec := range errorKinds {
		if spec.ID == "" {
			return fmt.Errorf("specdata/errors: error kind has an empty id")
		}
		if seen[spec.ID] {
			return fmt.Errorf("specdata/errors: error kind %q is declared twice", spec.ID)
		}
		seen[spec.ID] = true
		switch {
		case spec.Unit && spec.HeaderType != "":
			return fmt.Errorf("specdata/errors: error kind %q declares both a unit variant and a payload header", spec.ID)
		case !spec.Unit && spec.HeaderType == "":
			return fmt.Errorf("specdata/errors: error kind %q declares neither a unit variant nor a payload header", spec.ID)
		}
		if spec.HeaderType == "" {
			continue
		}
		payloads++
		if !isConstructorTypeID(spec.HeaderType) {
			return fmt.Errorf("specdata/errors: error kind %q header type %q is not a declared constructor", spec.ID, spec.HeaderType)
		}
	}
	if payloads != 1 {
		return fmt.Errorf("specdata/errors: registry declares %d payload-carrying variants, want exactly one", payloads)
	}
	return nil
}
