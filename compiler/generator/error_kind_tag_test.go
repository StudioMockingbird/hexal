package generator

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"hexal/compiler/specdata"
)

// errorKindTagPattern captures the ErrorKind variant name from the hand-written
// runtime C tag spelling the generator's fallback path also produces.
var errorKindTagPattern = regexp.MustCompile(`hex_tag_ErrorKind_([A-Za-z_]+)`)

// Every ErrorKind tag the hand-written runtime C spells must name a declared
// ErrorKind, and every declared ErrorKind must appear in that C: a renamed
// variant without a C update breaks the runtime at build time, never in the
// pure-Go suite, and a registered kind no C site mentions is a record nothing
// reads or a real gap.
func TestRuntimeErrorKindTagsMatchTheRegistry(t *testing.T) {
	roots := []string{
		filepath.Join("packages"),
		filepath.Join("..", "corelib", "runtime"),
	}
	used := make(map[string]int)
	sites := 0
	for _, root := range roots {
		for _, pattern := range []string{"*.c", "*.h"} {
			matches, err := filepath.Glob(filepath.Join(root, pattern))
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range matches {
				contents, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				for _, match := range errorKindTagPattern.FindAllStringSubmatch(string(contents), -1) {
					sites++
					used[match[1]]++
				}
			}
		}
	}
	if sites == 0 {
		t.Fatal("no hex_tag_ErrorKind sites were found in the runtime C")
	}

	var unregistered []string
	for name := range used {
		if _, ok := specdata.ErrorKind(specdata.ErrorKindID(name)); !ok {
			unregistered = append(unregistered, name)
		}
	}
	sort.Strings(unregistered)
	if len(unregistered) > 0 {
		t.Errorf("runtime C tags undeclared ErrorKinds: %v", unregistered)
	}

	registered := specdata.ErrorKinds()
	var unused []string
	for _, kind := range registered {
		if used[string(kind.ID)] == 0 {
			unused = append(unused, string(kind.ID))
		}
	}
	if len(unused) > 0 {
		t.Errorf("declared ErrorKinds no runtime C site mentions: %v", unused)
	}

	t.Logf("ErrorKind tags: %d names across %d C sites, %d declared", len(used), sites, len(registered))
}
