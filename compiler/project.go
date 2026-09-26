package compiler

import (
	"hexal/compiler/config"
	diagnostics "hexal/compiler/diagnostics"
	compilerTypes "hexal/compiler/types"
)

// Project carries build-time settings that are not part of the language.
// The zero value is valid and selects every default, so Compile(sources,
// entrypoint, Project{}) behaves exactly as the two-argument form did.
type Project struct {
	// TaskStackReserve is the per-Task address-space ceiling in bytes.
	// Zero selects config.DefaultTaskStackReserveBytes (1 MiB).
	TaskStackReserve uint64

	// TaskStackCommit is the bytes committed when a Task is spawned.
	// Zero selects config.DefaultTaskStackCommitBytes (8 KiB).
	TaskStackCommit uint64

	// Target selects one compiler-owned target profile identity. Empty
	// stays host-neutral and preserves the existing deterministic
	// generated-C contract. Any other identity must name a qualified
	// target, or compilation fails before lexing.
	Target compilerTypes.TargetProfileID
}

// validateProject checks the effective settings before any stage runs: the
// zero value selects a default before the rules apply, so a caller is never
// rejected for a field it did not set, except where the default itself
// violates a rule the caller's other field created, e.g. a 4 KiB reserve
// below the default 8 KiB commit. TaskStackReserve is non-zero after
// defaulting by construction.
func validateProject(project Project) error {
	if _, err := resolveTargetProfile(project.Target); err != nil {
		return err
	}
	reserve := project.TaskStackReserve
	if reserve == 0 {
		reserve = config.DefaultTaskStackReserveBytes
	}
	commit := project.TaskStackCommit
	if commit == 0 {
		commit = config.DefaultTaskStackCommitBytes
	}
	if commit > reserve {
		return compilerTypes.Locationless(diagnostics.ProjectTaskStackCommitExceeds(commit, reserve))
	}
	if reserve%config.PageSizeBytes != 0 {
		return compilerTypes.Locationless(diagnostics.ProjectTaskStackReserveAlignment(reserve, config.PageSizeBytes))
	}
	if commit%config.PageSizeBytes != 0 {
		return compilerTypes.Locationless(diagnostics.ProjectTaskStackCommitAlignment(commit, config.PageSizeBytes))
	}
	return nil
}
