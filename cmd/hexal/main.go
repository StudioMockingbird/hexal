// Command hexal is the Hexal CLI: a thin launcher that parses arguments,
// resolves conventions, and wires the compiler and driver together. It
// carries no compiler-version resolution logic, no build behavior, and no
// workbench routes, assets, or snippet logic of its own.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	compilerTypes "hexal/compiler/types"
	"hexal/internal/driver"
	"hexal/internal/version"
	"hexal/workbench"
)

func main() {
	// One error-reporting site. Commands return an error; nothing below
	// prints-and-exits internally.
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "hexal: %v\n", err)
		os.Exit(1)
	}
}

// run validates the toolchain identity before command dispatch: an invalid
// build identity is a configuration failure, never repaired at runtime.
func run(args []string) error {
	if !version.Valid() {
		return fmt.Errorf("invalid toolchain version %q", version.String())
	}
	return dispatch(args)
}

func dispatch(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usageText())
		return fmt.Errorf("expected a command")
	}
	// --version is a top-level alias, not a flag on any subcommand.
	if args[0] == "--version" {
		fmt.Println(versionLine())
		return nil
	}
	switch args[0] {
	case "build":
		return build(args[1:])
	case "doctor":
		return doctor(args[1:])
	case "version":
		fmt.Println(versionLine())
		return nil
	case "play":
		return workbench.Serve(version.String())
	case "help", "-h", "--help":
		fmt.Print(usageText())
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

// versionLine is the one direct version query: exactly one line.
func versionLine() string {
	return "hexal " + version.String()
}

// usageText renders help with the toolchain version as its first line. The
// remaining text stays stable.
func usageText() string {
	return fmt.Sprintf(`Hexal %s
usage:
  hexal build [options]   compile a Hexal program to a native executable
  hexal doctor            verify the local toolchain and environment
  hexal version           print the toolchain version
  hexal play              start the local workbench server
  hexal help              print this message

build options:
  -cc <path>            exact Zig executable path (required)
  -target <profile>     exact Hexal target profile (required)
  -runtime-dir <dir>    runtime-pack root override
  -mode <name>          debug or release (default: debug)
  -root <dir>           source root (default: current directory)
  -entry <key>          entrypoint logical key (default: main.hex)
  -out <path>           executable path (default: <root>/build/<entry>)
  -c-source <path>      compile and link one foreign C source (repeatable)
  -c-include <dir>      add one C header search directory (repeatable)
  -c-define <name[=v]>  define one C preprocessor macro (repeatable)
  -c-env <name=value>   override one environment variable for every C tool (repeatable)
  -c-standard <dialect> C dialect for foreign sources: c89, c99, c11, c17, c23
                        or their gnu forms (default: c17)
  -object <path>        link one precompiled object (repeatable)
  -archive <path>       link one static archive (repeatable)
  -system-library <name> link one system library by logical name (repeatable)

doctor options:
  -cc <path>            exact Zig executable path (required)
  -target <profile>     exact Hexal target profile (required)
  -runtime-dir <dir>    runtime-pack root override

Every relative foreign-input path resolves against -root. Generated Hexal
translation units remain C23; foreign sources use -c-standard.

Modes never change program behavior: generated C, output, and runtime trap
messages are identical. debug is unoptimized with debug information and an
undefined-behavior backstop; release is optimized, stripped, and smaller.
`, version.String())
}

func build(args []string) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var options driver.BuildOptions
	var mode, target string
	var cSources, cIncludeDirs, cDefines, cEnvironment, objects, archives, systemLibraries stringList
	flags.StringVar(&options.CompilerPath, "cc", "", "exact Zig executable path (required)")
	flags.StringVar(&target, "target", "", "exact Hexal target profile (required)")
	flags.StringVar(&options.RuntimeDir, "runtime-dir", "", "runtime-pack root override")
	flags.StringVar(&mode, "mode", "", "build mode: debug or release")
	flags.StringVar(&options.Root, "root", "", "source root")
	flags.StringVar(&options.Entrypoint, "entry", "", "entrypoint logical key")
	flags.StringVar(&options.Output, "out", "", "executable path")
	flags.Var(&cSources, "c-source", "compile and link one foreign C source (repeatable)")
	flags.Var(&cIncludeDirs, "c-include", "add one C header search directory (repeatable)")
	flags.Var(&cDefines, "c-define", "define one C preprocessor macro NAME[=VALUE] (repeatable)")
	flags.Var(&cEnvironment, "c-env", "override one environment variable NAME=VALUE for every C tool (repeatable)")
	flags.StringVar(&options.CStandard, "c-standard", "", "C dialect for foreign sources (default: c17)")
	flags.Var(&objects, "object", "link one precompiled object (repeatable)")
	flags.Var(&archives, "archive", "link one static archive (repeatable)")
	flags.Var(&systemLibraries, "system-library", "link one system library by logical name (repeatable)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	// The mode is resolved before anything else so an unknown value fails
	// ahead of discovery and compilation, not after them.
	parsed, err := driver.ParseMode(mode)
	if err != nil {
		return err
	}
	options.Mode = parsed
	options.Target = compilerTypes.TargetProfileID(target)
	options.CSources = cSources
	options.CIncludeDirs = cIncludeDirs
	options.CDefines = cDefines
	options.CEnvironment = cEnvironment
	options.Objects = objects
	options.Archives = archives
	options.SystemLibraries = systemLibraries

	result, err := driver.Build(options)
	if err != nil {
		return err
	}
	fmt.Println(result.Executable)
	return nil
}

// stringList is one repeatable string flag value: it preserves command-line
// occurrence order and rejects an empty operand, which no option accepts.
// Comma splitting is never performed: a value containing a comma is one
// option.
type stringList []string

func (list *stringList) String() string {
	return strings.Join(*list, ",")
}

func (list *stringList) Set(value string) error {
	if value == "" {
		return fmt.Errorf("empty operand")
	}
	*list = append(*list, value)
	return nil
}

// doctor prints the version report before any backend finding: the Hexal
// version stays available even when backend selection fails.
func doctor(args []string) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var options driver.DoctorOptions
	var target string
	flags.StringVar(&options.CompilerPath, "cc", "", "exact Zig executable path (required)")
	flags.StringVar(&target, "target", "", "exact Hexal target profile (required)")
	flags.StringVar(&options.RuntimeDir, "runtime-dir", "", "runtime-pack root override")
	if err := flags.Parse(args); err != nil {
		return err
	}
	options.Target = compilerTypes.TargetProfileID(target)
	report, problems := driver.Doctor(options)
	for _, line := range report {
		fmt.Println(line)
	}
	if len(problems) == 0 {
		fmt.Println("doctor: all checks passed")
		return nil
	}
	fmt.Fprintf(os.Stderr, "doctor found %d problem(s):\n", len(problems))
	for _, problem := range problems {
		fmt.Fprintf(os.Stderr, "- %s\n", problem)
	}
	return fmt.Errorf("doctor reported %d problem(s)", len(problems))
}
