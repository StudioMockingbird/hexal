package version

import (
	"os"
	"runtime/debug"
	"time"
)

// value is fixed from the executable's file timestamp. Go does not expose the
// build instant to a program compiled by plain go install, but the installed
// artifact has the timestamp of the build/install operation.
var value = buildVersion()

// development is the only non-timestamp value, reported by unversioned
// developer builds.
const development = "development"

// timestampLayout is both the Format and the Parse layout, so the grammar a
// timestamp must satisfy is exactly the rendering this package produces.
const timestampLayout = "2006-Jan-02-15-04"

// String returns the toolchain identity: an executable timestamp or
// "development". The value is fixed for the process lifetime.
func String() string {
	return value
}

// Valid reports whether the identity is usable: exactly "development" or
// exactly one canonical timestamp rendering with a real calendar date. The
// CLI checks this before command dispatch; an invalid build value is a
// configuration failure, never repaired or reformatted at runtime.
func Valid() bool {
	if value == development {
		return true
	}
	return validTimestamp(value)
}

func buildVersion() string {
	if executable, err := os.Executable(); err == nil {
		if info, err := os.Stat(executable); err == nil {
			return info.ModTime().UTC().Format(timestampLayout)
		}
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return development
	}
	return versionFromBuildInfo(info)
}

func versionFromBuildInfo(info *debug.BuildInfo) string {
	if info == nil {
		return development
	}
	for _, setting := range info.Settings {
		if setting.Key != "vcs.time" {
			continue
		}
		builtAt, err := time.Parse(time.RFC3339, setting.Value)
		if err != nil {
			return development
		}
		return builtAt.UTC().Format(timestampLayout)
	}
	return development
}

// validTimestamp enforces the timestamp grammar by parsing and re-rendering:
// equality with the input proves every field round-tripped, which rejects a
// non-canonical month spelling (time.Parse matches month names case-insensitively)
// and a one-digit hour (time.Parse accepts both widths) while delegating the
// four-digit year, two-digit day and minute, separators, trailing text, and
// calendar including leap years to time.Parse.
func validTimestamp(text string) bool {
	parsed, err := time.Parse(timestampLayout, text)
	if err != nil {
		return false
	}
	return text == parsed.Format(timestampLayout)
}
