// Command hexal is the Hexal CLI: a thin launcher that parses arguments,
// resolves conventions, and wires the compiler and driver together. It
// carries no compiler-version resolution logic, no build behavior, and no
// workbench routes, assets, or snippet logic of its own.
package main

import (
	"flag"
	"fmt"
	"os"

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
		return doctor()
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
  -root <dir>       source root (default: current directory)
  -entry <key>      entrypoint logical key (default: main.hex)
  -out <path>       executable path (default: <root>/build/<entry>)
`, version.String())
}

func build(args []string) error {
	flags := flag.NewFlagSet("build", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var options driver.BuildOptions
	flags.StringVar(&options.Root, "root", "", "source root")
	flags.StringVar(&options.Entrypoint, "entry", "", "entrypoint logical key")
	flags.StringVar(&options.Output, "out", "", "executable path")
	if err := flags.Parse(args); err != nil {
		return err
	}

	result, err := driver.Build(options)
	if err != nil {
		return err
	}
	fmt.Println(result.Executable)
	return nil
}

// doctor prints the version report before any backend finding: the Hexal
// version stays available even when backend discovery fails.
func doctor() error {
	report, problems := driver.Doctor()
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
