package driver

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"hexal/compiler"
	"hexal/internal/backend"
	moduledeps "hexal/modules"
)

type nativeDependency struct {
	compileOptions []string
	sources        []string
	objects        []string
}

func materializeDependencies(staging string, dependencies []compiler.RuntimeDependency) (nativeDependency, error) {
	result := nativeDependency{}
	seen := make(map[compiler.RuntimeDependency]bool)
	for _, dependency := range dependencies {
		if seen[dependency] {
			return nativeDependency{}, fmt.Errorf("duplicate runtime dependency %q", dependency)
		}
		seen[dependency] = true
		switch dependency {
		case compiler.RuntimeMimalloc:
			if err := moduledeps.Verify(); err != nil {
				return nativeDependency{}, err
			}
			includeRoot := filepath.Join(staging, "dependencies", "mimalloc", "include")
			if err := materializeEmbeddedTree(staging, "dependencies/mimalloc", "mimalloc/include", includeRoot); err != nil {
				return nativeDependency{}, err
			}
			sourceRoot := filepath.Join(staging, "dependencies", "mimalloc", "src")
			if err := materializeEmbeddedTree(staging, "dependencies/mimalloc", "mimalloc/src", sourceRoot); err != nil {
				return nativeDependency{}, err
			}
			result.compileOptions = append(result.compileOptions, "-I", includeRoot)
			result.sources = append(result.sources, filepath.Join(staging, "dependencies", "mimalloc", "src", "static.c"))
			result.objects = append(result.objects, filepath.Join(staging, "objects", "mimalloc.o"))
		default:
			return nativeDependency{}, fmt.Errorf("unknown runtime dependency %q", dependency)
		}
	}
	return result, nil
}

func materializeEmbeddedTree(staging, prefix, tree, destination string) error {
	root := filepath.Join(staging, prefix)
	if err := fs.WalkDir(moduledeps.Files, tree, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, err := moduledeps.Files.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(tree, path)
		if err != nil {
			return err
		}
		target := filepath.Join(destination, relative)
		absoluteRoot, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		absoluteTarget, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if absoluteTarget != absoluteRoot && !strings.HasPrefix(absoluteTarget, absoluteRoot+string(os.PathSeparator)) {
			return fmt.Errorf("embedded dependency path escapes staging root: %q", path)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644)
	}); err != nil {
		return fmt.Errorf("materializing embedded mimalloc %s: %w", tree, err)
	}
	return nil
}

func compileNativeDependencies(selected *backend.Backend, staging string, dependency nativeDependency, result *BuildResult) error {
	if len(dependency.sources) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(staging, "objects"), 0o755); err != nil {
		return err
	}
	for index, source := range dependency.sources {
		options := append([]string{"-DMI_BUILD_RELEASE", "-DMI_WIN_INIT_USE_RAW_DLLMAIN"}, dependency.compileOptions...)
		invocation, err := selected.CompileOneDialect(qualifiedTriple, "c11", options, source, dependency.objects[index])
		if err != nil {
			return &BuildError{Stage: StageCompile, Message: fmt.Sprintf("cannot run backend for native dependency %s: %v", filepath.Base(source), err)}
		}
		result.Commands = append(result.Commands, CommandResult{
			Stage:            StageCompile,
			Tool:             selected.Exe,
			Arguments:        invocation.Args,
			WorkingDirectory: staging,
			Stdout:           invocation.Stdout,
			Stderr:           invocation.Stderr,
			ExitCode:         invocation.ExitCode,
		})
		if invocation.ExitCode != 0 {
			return &BuildError{
				Stage:   StageCompile,
				Message: fmt.Sprintf("C compilation of native dependency %s failed", filepath.Base(source)),
				Command: &result.Commands[len(result.Commands)-1],
			}
		}
	}
	return nil
}
