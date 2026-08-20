package compiler

import (
	"fmt"

	"hexal/compiler/generator"
	compilerTypes "hexal/compiler/types"
)

// Project carries build-time settings that are not part of the language.
// The zero value is valid and selects every default, so Compile(sources,
// entrypoint, Project{}) behaves exactly as the two-argument form did.
type Project struct {
	// TaskStackReserve is the per-Task address-space ceiling in bytes.
	// Zero selects generator.DefaultTaskStackReserve (1 MiB).
	TaskStackReserve uint64

	// TaskStackCommit is the bytes committed when a Task is spawned.
	// Zero selects generator.DefaultTaskStackCommit (8 KiB).
	TaskStackCommit uint64
}

// configPageSize is the page size the POSIX guard depends on; Windows rounds
// its own arguments, but the rule applies to both targets so a value is never
// accepted on one and rejected on the other.
const configPageSize = 4096

// validateProject checks the effective settings before any stage runs: the
// zero value selects a default before the rules apply, so a caller is never
// rejected for a field it did not set, except where the default itself
// violates a rule the caller's other field created, e.g. a 4 KiB reserve
// below the default 8 KiB commit. TaskStackReserve is non-zero after
// defaulting by construction.
func validateProject(project Project) error {
	reserve := project.TaskStackReserve
	if reserve == 0 {
		reserve = generator.DefaultTaskStackReserve
	}
	commit := project.TaskStackCommit
	if commit == 0 {
		commit = generator.DefaultTaskStackCommit
	}
	if commit > reserve {
		return projectDiagnostic(fmt.Sprintf("TaskStackCommit %d exceeds TaskStackReserve %d", commit, reserve))
	}
	if reserve%configPageSize != 0 {
		return projectDiagnostic(fmt.Sprintf("TaskStackReserve %d is not a multiple of %d", reserve, configPageSize))
	}
	if commit%configPageSize != 0 {
		return projectDiagnostic(fmt.Sprintf("TaskStackCommit %d is not a multiple of %d", commit, configPageSize))
	}
	return nil
}

// projectDiagnostic builds the one compilation-wide diagnostic class a
// Project can produce; it carries no module or position because no source
// line caused it.
func projectDiagnostic(message string) error {
	return compilerTypes.NewDiagnostic(compilerTypes.ConfigurationError, "compile", 1, 1, message)
}
