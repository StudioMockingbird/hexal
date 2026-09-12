package version

import (
	"runtime/debug"
	"testing"
	"time"
)

// The grammar accepts every English month and a real calendar minute, and
// rejects wrong width, casing, separators, impossible fields, and trailing
// text. Build metadata is fixed for the executable, so tests drive the
// validator directly rather than mutating the process identity.
func TestValidTimestamp(t *testing.T) {
	for _, month := range []string{
		"Jan", "Feb", "Mar", "Apr", "May", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
	} {
		if !validTimestamp("2026-" + month + "-12-18-05") {
			t.Errorf("validTimestamp rejected month %q", month)
		}
	}
	for _, text := range []string{
		"2026-Sep-12-18-05",
		"2024-Feb-29-00-00",
		"2000-Feb-29-23-59",
	} {
		if !validTimestamp(text) {
			t.Errorf("validTimestamp(%q) = false, want true", text)
		}
	}
	for _, text := range []string{
		"",
		"development",
		"2026-sep-12-18-05",
		"2026-SEP-12-18-05",
		"2026-Xxx-12-18-05",
		"26-Sep-12-18-05",
		"2026-Sep-1-18-05",
		"2026-Sep-12-8-05",
		"2026-Sep-12-18-5",
		"2026/Sep/12/18/05",
		"2026-Sep-12-18-05-00",
		"2026-Sep-12-18-05 ",
		"2026-Sep-12-18-05+00:00",
		"2026-Sep-00-18-05",
		"2026-Sep-31-18-05",
		"2026-Apr-31-18-05",
		"2023-Feb-29-18-05",
		"1900-Feb-29-18-05",
		"2026-Sep-12-24-05",
		"2026-Sep-12-18-60",
		"hexal 2026-Sep-12-18-05",
	} {
		if validTimestamp(text) {
			t.Errorf("validTimestamp(%q) = true, want false", text)
		}
	}
}

func TestBuildVersionUsesVCSMinute(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.time", Value: "2026-09-12T16:57:44Z"},
	}}
	if got := versionFromBuildInfo(info); got != "2026-Sep-12-16-57" {
		t.Fatalf("versionFromBuildInfo() = %q, want timestamp", got)
	}
}

func TestBuildVersionFallsBackWithoutVCSMetadata(t *testing.T) {
	if got := versionFromBuildInfo(&debug.BuildInfo{}); got != development {
		t.Fatalf("versionFromBuildInfo() = %q, want development", got)
	}
}

func TestBuildVersionRejectsInvalidVCSMetadata(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.time", Value: "not-a-time"},
	}}
	if got := versionFromBuildInfo(info); got != development {
		t.Fatalf("versionFromBuildInfo() = %q, want development", got)
	}
}

func TestBuildVersionIsUTC(t *testing.T) {
	info := &debug.BuildInfo{Settings: []debug.BuildSetting{
		{Key: "vcs.time", Value: "2026-09-12T00:05:00+05:30"},
	}}
	want := time.Date(2026, time.September, 11, 18, 35, 0, 0, time.UTC).Format("2006-Jan-02-15-04")
	if got := versionFromBuildInfo(info); got != want {
		t.Fatalf("versionFromBuildInfo() = %q, want %q", got, want)
	}
}
