package specdata

import (
	"errors"
	"fmt"
)

// TargetID identifies one TargetFacts record inside this registry. It is a
// registry key, not the language-visible target identity: compiler/types owns
// TargetProfileID, and driver qualification is not a language fact at all.
type TargetID string

// Registry keys for the targets the compiler currently describes.
const (
	TargetWindowsUCRT TargetID = "windows-ucrt"
	TargetLinuxGNU    TargetID = "linux-gnu"
)

// TargetFacts is one target's semantic facts. A fact enters only when checking
// or generation consumes it; absent a consumer, the fact stays out. Target
// identity and driver qualification live outside this package, so a record
// carries neither, and ID keys the registry rather than naming the target to
// any caller.
type TargetFacts struct {
	ID            TargetID
	OS            string
	Architecture  string
	LittleEndian  bool
	PointerWidth  int
	SizeWidth     int
	WindowsTarget bool
	Threading     string
	TLS           bool
	Fibers        bool
	NativeIO      bool
}

// targetFacts lists every target record. A slice rather than a map lets
// Validate report a repeated identifier as a source-tree defect instead of the
// compiler refusing an identical map key before the check runs.
var targetFacts = []TargetFacts{
	{
		ID:            TargetWindowsUCRT,
		OS:            "windows",
		Architecture:  "x86_64",
		LittleEndian:  true,
		PointerWidth:  64,
		SizeWidth:     64,
		WindowsTarget: true,
		Threading:     "windows",
		TLS:           true,
		Fibers:        true,
		NativeIO:      true,
	},
	{
		ID:            TargetLinuxGNU,
		OS:            "linux",
		Architecture:  "x86_64",
		LittleEndian:  true,
		PointerWidth:  64,
		SizeWidth:     64,
		WindowsTarget: false,
		Threading:     "posix",
		TLS:           true,
		Fibers:        true,
		NativeIO:      true,
	},
}

// Target returns the facts record for id. The bool is false when no record
// declares id; a caller treats that as a compiler-development defect, because
// Validate rejects a registry that cannot answer its own keys.
func Target(id TargetID) (TargetFacts, bool) {
	for _, facts := range targetFacts {
		if facts.ID == id {
			return facts, true
		}
	}
	return TargetFacts{}, false
}

// validateTargets rejects a registry that cannot be trusted: a record with no
// identifier or one another record already names, or a record that leaves a
// fact every consumer assumes set empty or non-positive.
func validateTargets() error {
	seen := make(map[TargetID]bool, len(targetFacts))
	for _, facts := range targetFacts {
		switch {
		case facts.ID == "":
			return errors.New("empty target id")
		case seen[facts.ID]:
			return fmt.Errorf("target %s: repeated", facts.ID)
		case facts.OS == "":
			return fmt.Errorf("target %s: os", facts.ID)
		case facts.Architecture == "":
			return fmt.Errorf("target %s: architecture", facts.ID)
		case facts.PointerWidth <= 0 || facts.SizeWidth <= 0:
			return fmt.Errorf("target %s: width", facts.ID)
		case facts.Threading == "":
			return fmt.Errorf("target %s: threading", facts.ID)
		}
		seen[facts.ID] = true
	}
	return nil
}
