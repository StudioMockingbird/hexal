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
	linkObjects    []string
	archiveObjects []string
	libuvArchive   string
	linkOptions    []string
}

var libuvWindowsSources = []string{
	"fs-poll.c", "idna.c", "inet.c", "random.c", "strscpy.c", "strtok.c",
	"thread-common.c", "threadpool.c", "timer.c", "uv-common.c", "uv-data-getter-setters.c", "version.c",
	"win/async.c", "win/core.c", "win/detect-wakeup.c", "win/dl.c", "win/error.c", "win/fs.c",
	"win/fs-event.c", "win/getaddrinfo.c", "win/getnameinfo.c", "win/handle.c", "win/loop-watcher.c",
	"win/pipe.c", "win/thread.c", "win/poll.c", "win/process.c", "win/process-stdio.c", "win/signal.c",
	"win/snprintf.c", "win/stream.c", "win/tcp.c", "win/tty.c", "win/udp.c", "win/util.c",
	"win/winapi.c", "win/winsock.c",
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
			object := filepath.Join(staging, "objects", "mimalloc.o")
			result.objects = append(result.objects, object)
			result.linkObjects = append(result.linkObjects, object)
			// mimalloc's own Windows link set (CMakeLists mi_libraries); MinGW
			// supplies some implicitly, MSVC-target toolchains supply none.
			result.linkOptions = append(result.linkOptions, "-lpsapi", "-lshell32", "-luser32", "-ladvapi32", "-lbcrypt")
		case compiler.RuntimeLibuv:
			if err := moduledeps.Verify(); err != nil {
				return nativeDependency{}, err
			}
			includeRoot := filepath.Join(staging, "dependencies", "libuv", "include")
			if err := materializeEmbeddedTree(staging, "dependencies/libuv", "libuv/include", includeRoot); err != nil {
				return nativeDependency{}, err
			}
			sourceRoot := filepath.Join(staging, "dependencies", "libuv", "src")
			if err := materializeEmbeddedTree(staging, "dependencies/libuv", "libuv/src", sourceRoot); err != nil {
				return nativeDependency{}, err
			}
			result.compileOptions = append(result.compileOptions, "-I", includeRoot, "-I", sourceRoot, "-DWIN32_LEAN_AND_MEAN", "-D_WIN32_WINNT=0x0A00", "-D_CRT_DECLARE_NONSTDC_NAMES=0", "-D_CRT_SECURE_NO_WARNINGS", "-fno-strict-aliasing")
			for index, relative := range libuvWindowsSources {
				result.sources = append(result.sources, filepath.Join(sourceRoot, filepath.FromSlash(relative)))
				object := filepath.Join(staging, "objects", fmt.Sprintf("libuv-%02d.o", index))
				result.objects = append(result.objects, object)
				result.archiveObjects = append(result.archiveObjects, object)
			}
			result.libuvArchive = filepath.Join(staging, "objects", "libuv.a")
			result.linkObjects = append(result.linkObjects, result.libuvArchive)
			result.linkOptions = append(result.linkOptions, "-lpsapi", "-luser32", "-ladvapi32", "-liphlpapi", "-luserenv", "-lws2_32", "-ldbghelp", "-lole32", "-lshell32")
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
		return fmt.Errorf("materializing embedded dependency %s: %w", tree, err)
	}
	return nil
}

func nativeCompileOptions(source string, base []string) []string {
	options := append([]string(nil), base...)
	if strings.Contains(source, string(filepath.Separator)+"mimalloc"+string(filepath.Separator)) {
		return append([]string{"-O2", "-DMI_BUILD_RELEASE", "-DMI_WIN_INIT_USE_RAW_DLLMAIN"}, options...)
	}
	if strings.Contains(source, string(filepath.Separator)+"libuv"+string(filepath.Separator)) {
		return append([]string{"-O2"}, options...)
	}
	return options
}

func compileNativeDependencies(selected *backend.Backend, staging string, dependency nativeDependency, result *BuildResult) error {
	if len(dependency.sources) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(staging, "objects"), 0o755); err != nil {
		return err
	}
	for index, source := range dependency.sources {
		options := nativeCompileOptions(source, dependency.compileOptions)
		invocation, err := selected.CompileOneDialect(qualifiedTriple, "c11", options, source, dependency.objects[index])
		if err != nil {
			return &BuildError{Stage: StageCompile, Message: fmt.Sprintf("cannot run backend for native dependency %s: %v", filepath.Base(source), err)}
		}
		result.Commands = append(result.Commands, CommandResult{
			Stage:                StageCompile,
			Tool:                 selected.Exe,
			Arguments:            invocation.Args,
			WorkingDirectory:     staging,
			Stdout:               invocation.Stdout,
			Stderr:               invocation.Stderr,
			ExitCode:             invocation.ExitCode,
			EnvironmentOverrides: append([]string(nil), selected.EnvironmentOverrides...),
		})
		if invocation.ExitCode != 0 {
			return &BuildError{
				Stage:   StageCompile,
				Message: fmt.Sprintf("C compilation of native dependency %s failed", filepath.Base(source)),
				Command: &result.Commands[len(result.Commands)-1],
			}
		}
	}
	if len(dependency.archiveObjects) > 0 {
		invocation, err := selected.ArchiveObjects(dependency.archiveObjects, dependency.libuvArchive)
		if err != nil {
			return &BuildError{Stage: StageCompile, Message: fmt.Sprintf("cannot run archiver for libuv: %v", err)}
		}
		result.Commands = append(result.Commands, CommandResult{
			Stage:                StageCompile,
			Tool:                 selected.Exe,
			Arguments:            invocation.Args,
			WorkingDirectory:     staging,
			Stdout:               invocation.Stdout,
			Stderr:               invocation.Stderr,
			ExitCode:             invocation.ExitCode,
			EnvironmentOverrides: append([]string(nil), selected.EnvironmentOverrides...),
		})
		if invocation.ExitCode != 0 {
			return &BuildError{
				Stage:   StageCompile,
				Message: "creating libuv static archive failed",
				Command: &result.Commands[len(result.Commands)-1],
			}
		}
	}
	return nil
}

// DependencyPlan is the materialized native-dependency input set of one
// program: the compile options generated translation units and dependency
// sources share, each dependency source, and the link options the final
// executable needs. External qualification harnesses build dependencies from
// it exactly as the driver does rather than keeping a second source list.
type DependencyPlan struct {
	CompileOptions []string
	Sources        []string
	LinkOptions    []string
}

// PlanDependencies materializes the embedded dependency snapshots under
// staging and returns their build plan.
func PlanDependencies(staging string, dependencies []compiler.RuntimeDependency) (DependencyPlan, error) {
	native, err := materializeDependencies(staging, dependencies)
	if err != nil {
		return DependencyPlan{}, err
	}
	return DependencyPlan{CompileOptions: native.compileOptions, Sources: native.sources, LinkOptions: native.linkOptions}, nil
}

// SourceOptions returns the complete option list for compiling one dependency
// source, including its optimization level and dependency-specific defines.
func (plan DependencyPlan) SourceOptions(source string) []string {
	return nativeCompileOptions(source, plan.CompileOptions)
}
