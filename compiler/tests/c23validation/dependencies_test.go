//go:build c23

package c23validation

// Native runtime dependencies for the harness. A program whose compilation
// result names mimalloc or libuv links them the way the build driver does:
// the driver's own dependency plan supplies the embedded sources, per-source
// options, include roots, and link libraries, so the harness keeps no second
// source list. Each toolchain builds one program's dependency set once per
// build root and shares the objects across every fixture that needs it.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"hexal/compiler"
	"hexal/internal/driver"
)

// dependencyBuild is one toolchain's compiled dependency set: the options a
// generated translation unit needs to see the dependency headers, the
// objects to link, and the link libraries.
type dependencyBuild struct {
	once           sync.Once
	includeOptions []string
	objects        []string
	linkOptions    []string
	err            error
}

var (
	dependencyCacheMu sync.Mutex
	dependencyCache   = map[string]*dependencyBuild{}
)

// buildDependencies returns tc's compiled dependency set for dependencies
// under buildRoot, building it on first use.
func buildDependencies(tc toolchain, dependencies []compiler.RuntimeDependency, buildRoot string) *dependencyBuild {
	names := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		names = append(names, string(dependency))
	}
	set := strings.Join(names, "+")
	key := tc.Name + "|" + set + "|" + buildRoot

	dependencyCacheMu.Lock()
	entry, ok := dependencyCache[key]
	if !ok {
		entry = &dependencyBuild{}
		dependencyCache[key] = entry
	}
	dependencyCacheMu.Unlock()

	entry.once.Do(func() {
		staging := filepath.Join(buildRoot, "dependencies", tc.Name, set)
		plan, err := driver.PlanDependencies(staging, dependencies)
		if err != nil {
			entry.err = err
			return
		}
		// Generated C sees only the dependency include roots, as system
		// headers: it is compiled with -Werror, its own sources already define
		// the platform macros they need, and a header warning is not a
		// generated-code defect.
		for index := 0; index+1 < len(plan.CompileOptions); index++ {
			if plan.CompileOptions[index] == "-I" {
				entry.includeOptions = append(entry.includeOptions, "-isystem", plan.CompileOptions[index+1])
				index++
			}
		}
		entry.objects = make([]string, len(plan.Sources))
		errs := make([]error, len(plan.Sources))
		limit := make(chan struct{}, runtime.NumCPU())
		var wait sync.WaitGroup
		for index, source := range plan.Sources {
			entry.objects[index] = filepath.Join(staging, "objects", fmt.Sprintf("dependency-%02d.o", index))
			wait.Add(1)
			go func() {
				defer wait.Done()
				limit <- struct{}{}
				defer func() { <-limit }()
				errs[index] = compileDependencySource(tc, plan.SourceOptions(source), source, entry.objects[index])
			}()
		}
		wait.Wait()
		for _, err := range errs {
			if err != nil {
				entry.err = err
				return
			}
		}
		entry.linkOptions = plan.LinkOptions
	})
	return entry
}

// compileDependencySource compiles one dependency source in the C dialect its
// upstream project qualifies, with no harness warning policy applied.
func compileDependencySource(tc toolchain, options []string, source, object string) error {
	if err := os.MkdirAll(filepath.Dir(object), 0755); err != nil {
		return err
	}
	args := append([]string{}, tc.Command[1:]...)
	args = append(args, "-std=c11")
	if tc.Name == "gcc" {
		// GCC 14+ makes -Wincompatible-pointer-types an error by default, and
		// the pinned libuv passes a const char ** where char ** is declared in
		// win/util.c; that upstream mismatch is not Hexal output.
		args = append(args, "-Wno-error=incompatible-pointer-types")
	}
	args = append(args, options...)
	args = append(args, "-c", source, "-o", object)
	ctx, cancel := context.WithTimeout(context.Background(), buildProcessTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, tc.Command[0], args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s did not compile dependency %s within %s", tc.Name, filepath.Base(source), buildProcessTimeout)
	}
	if err != nil {
		return fmt.Errorf("%s rejected dependency %s: %v\n%s", tc.Name, source, err, output)
	}
	return nil
}
