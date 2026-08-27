package integration

// The module-path allowlist closes two reproduced defects at the
// compiler.Compile boundary: a crafted logical key injecting preprocessor
// text into generated output, and a crafted key escaping the artifact
// namespace through path traversal. Both are now rejected before any source
// is lexed, with no artifact and no partial statistics.

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// A logical key crafted to land inside a generated #include, #line
// directive, and header guard now exits with a diagnostic instead of
// successfully emitting the injected text.
func TestInjectedModulePathIsRejectedNotCompiled(t *testing.T) {
	malicious := "app.hex\"\n#define HEXAL_OWNED 1\n#include <stdio.h>\n\"x"
	result := compiler.Compile(map[string]string{malicious: "x: Int32 := 1\n"}, malicious, compiler.Project{})
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("ExitCode = %d, want ExitFailure", result.ExitCode)
	}
	for name, content := range result.Files {
		if strings.Contains(content, "HEXAL_OWNED") {
			t.Fatalf("artifact %s contains injected preprocessor text: %s", name, content)
		}
	}
}

// A path-traversal logical key is rejected outright; no accepted compilation
// can ever produce an artifact name containing ".." as a path segment.
func TestPathTraversalModulePathIsRejected(t *testing.T) {
	result := compiler.Compile(map[string]string{"../../../etc/passwd.hex": "x: Int32 := 1\n"}, "../../../etc/passwd.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("ExitCode = %d, want ExitFailure", result.ExitCode)
	}
	if len(result.Files) != 0 {
		t.Fatalf("Files = %v, want empty", result.Files)
	}
}

// No artifact name produced by any accepted compilation contains ".." as a
// path segment, checked positively over a program the allowlist accepts.
func TestAcceptedCompilationNeverEmitsTraversalArtifactNames(t *testing.T) {
	sources := map[string]string{
		"app.hex":               "module Shapes = import \"./graphics/shapes_2\"\nx: Int32 := 1\n",
		"graphics/shapes_2.hex": "fun area(): Int32 do\n    return 1\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("ExitCode = %d, want ExitSuccess: %v", result.ExitCode, result.Stderr)
	}
	for name := range result.Files {
		for _, segment := range strings.Split(name, "/") {
			if segment == ".." {
				t.Fatalf("Files = %v, artifact %s contains a \"..\" path segment", result.Files, name)
			}
		}
	}
}

// Legal keys -- a single component, a nested identifier path, and mixed-case
// components -- all compile unaffected by the allowlist.
func TestLegalModulePathsCompile(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		sources map[string]string
		entry   string
	}{
		{"single component", map[string]string{"app.hex": "x: Int32 := 1\n"}, "app.hex"},
		{
			"nested identifier path",
			map[string]string{
				"app.hex":               "module Shapes = import \"./graphics/shapes_2\"\nx: Int32 := 1\n",
				"graphics/shapes_2.hex": "fun area(): Int32 do\n    return 1\nend\n",
			},
			"app.hex",
		},
		{
			"mixed-case components",
			map[string]string{
				"App.hex":              "module Shapes = import \"./Graphics/Shapes2\"\nx: Int32 := 1\n",
				"Graphics/Shapes2.hex": "fun area(): Int32 do\n    return 1\nend\n",
			},
			"App.hex",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compiler.Compile(testCase.sources, testCase.entry, compiler.Project{})
			if result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("ExitCode = %d, want ExitSuccess: %v", result.ExitCode, result.Stderr)
			}
		})
	}
}
