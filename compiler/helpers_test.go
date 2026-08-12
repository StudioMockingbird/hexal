package compiler

// Shared test helpers.

import "strings"

func withoutLineDirectives(source string) string {
	lines := strings.Split(source, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		if !strings.HasPrefix(line, "#line ") {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}
