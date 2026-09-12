package version

import (
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"time"
)

// value is fixed from the executable's file timestamp. Go does not expose the
// build instant to a program compiled by plain go install, but the installed
// artifact has the timestamp of the build/install operation.
var value = buildVersion()

// development is the only non-timestamp value, reported by unversioned
// developer builds.
const development = "development"

var timestampPattern = regexp.MustCompile(`^(\d{4})-([A-Za-z]{3})-(\d{2})-(\d{2})-(\d{2})$`)

var months = map[string]time.Month{
	"Jan": time.January, "Feb": time.February, "Mar": time.March,
	"Apr": time.April, "May": time.May, "Jun": time.June,
	"Jul": time.July, "Aug": time.August, "Sep": time.September,
	"Oct": time.October, "Nov": time.November, "Dec": time.December,
}

// String returns the toolchain identity: an executable timestamp or
// "development". The value is fixed for the process lifetime.
func String() string {
	return value
}

// IsDevelopment reports whether this is an unversioned developer build.
func IsDevelopment() bool {
	return value == development
}

// Valid reports whether the identity is usable: exactly "development" or a
// timestamp matching the version grammar with a real calendar minute. The
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
			return info.ModTime().UTC().Format("2006-Jan-02-15-04")
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
		return builtAt.UTC().Format("2006-Jan-02-15-04")
	}
	return development
}

// validTimestamp checks grammar and calendar: four-digit year, English month
// abbreviation, and a day, hour, and minute that exist. Lexical order is not
// chronological across months, so consumers parse the month table first.
func validTimestamp(text string) bool {
	fields := timestampPattern.FindStringSubmatch(text)
	if fields == nil {
		return false
	}
	month, ok := months[fields[2]]
	if !ok {
		return false
	}
	year, _ := strconv.Atoi(fields[1])
	day, _ := strconv.Atoi(fields[3])
	hour, _ := strconv.Atoi(fields[4])
	minute, _ := strconv.Atoi(fields[5])
	if day < 1 || day > daysIn(month, year) || hour > 23 || minute > 59 {
		return false
	}
	return true
}

// daysIn returns the days of month in year, so February 29 is accepted only
// in leap years.
func daysIn(month time.Month, year int) int {
	switch month {
	case time.April, time.June, time.September, time.November:
		return 30
	case time.February:
		if year%4 == 0 && (year%100 != 0 || year%400 == 0) {
			return 29
		}
		return 28
	default:
		return 31
	}
}
