package modules

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strings"
)

// Verify checks the embedded module inputs against their checked-in manifest.
func Verify() error {
	raw, err := Files.ReadFile("MANIFEST.sha256")
	if err != nil {
		return fmt.Errorf("mimalloc manifest is unavailable: %w", err)
	}
	seen := make(map[string]bool)
	previous := ""
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.SplitN(line, "  ", 2)
		if len(fields) != 2 || len(fields[0]) != sha256.Size*2 || fields[1] == "" {
			return fmt.Errorf("mimalloc manifest has malformed entry %q", line)
		}
		name := fields[1]
		if previous != "" && name <= previous {
			return fmt.Errorf("mimalloc manifest is not in bytewise path order at %q", name)
		}
		previous = name
		if seen[name] {
			return fmt.Errorf("mimalloc manifest repeats %q", name)
		}
		seen[name] = true
		content, err := Files.ReadFile(name)
		if err != nil {
			return fmt.Errorf("mimalloc manifest names missing file %q: %w", name, err)
		}
		hash := sha256.Sum256(content)
		if got := hex.EncodeToString(hash[:]); got != fields[0] {
			return fmt.Errorf("mimalloc file %q has digest %s, want %s", name, got, fields[0])
		}
	}
	if !seen["mimalloc/LICENSE"] {
		return fmt.Errorf("mimalloc manifest omits mimalloc/LICENSE")
	}
	if !seen["MIMALLOC.md"] {
		return fmt.Errorf("mimalloc manifest omits MIMALLOC.md")
	}
	if err := fs.WalkDir(Files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || path == "embed.go" || path == "verify.go" || path == "MANIFEST.sha256" {
			return nil
		}
		if !seen[path] {
			return fmt.Errorf("mimalloc embedded file %q is absent from the manifest", path)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}
