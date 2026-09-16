package corelib

import "fmt"

// MovedType is one former protected type name's new standard-library home.
type MovedType struct {
	Module string
	Alias  string
}

var movedTypes = map[string]MovedType{
	"IO":                  {"std/io", "Io"},
	"Bytes":               {"std/io", "Io"},
	"Seek":                {"std/io", "Io"},
	"File":                {"std/fs", "Fs"},
	"FileMode":            {"std/fs", "Fs"},
	"Duration":            {"std/time", "Time"},
	"Instant":             {"std/time", "Time"},
	"WallTime":            {"std/time", "Time"},
	"Address":             {"std/net", "Net"},
	"TcpConnection":       {"std/net", "Net"},
	"TcpListener":         {"std/net", "Net"},
	"Process":             {"std/process", "Proc"},
	"Pipe":                {"std/process", "Proc"},
	"ProcessOptions":      {"std/process", "Proc"},
	"StartedProcess":      {"std/process", "Proc"},
	"Environment":         {"std/process", "Proc"},
	"EnvironmentVariable": {"std/process", "Proc"},
	"ProcessStream":       {"std/process", "Proc"},
	"ExitStatus":          {"std/process", "Proc"},
	"Signal":              {"std/signal", "Sig"},
	"Signals":             {"std/signal", "Sig"},
	"TerminalSize":        {"std/terminal", "Term"},
}

// RemovedNamespace is one former type-only namespace that has no replacement
// type: its operations become module functions.
type RemovedNamespace struct {
	Module   string
	Alias    string
	Function string
}

var removedNamespaces = map[string]RemovedNamespace{
	"Dns":      {"std/net", "Net", "resolve"},
	"Tcp":      {"std/net", "Net", "connect"},
	"Terminal": {"std/terminal", "Term", "is_attached"},
}

// MovedOperation is one former static operation's new module function.
type MovedOperation struct {
	Module   string
	Alias    string
	Function string
}

var movedOperations = map[string]MovedOperation{
	"File.open":             {"std/fs", "Fs", "open"},
	"IO.stdin":              {"std/io", "Io", "stdin"},
	"IO.stdout":             {"std/io", "Io", "stdout"},
	"IO.stderr":             {"std/io", "Io", "stderr"},
	"Bytes.over":            {"std/io", "Io", "bytes_over"},
	"Duration.nanoseconds":  {"std/time", "Time", "nanoseconds"},
	"Duration.microseconds": {"std/time", "Time", "microseconds"},
	"Duration.milliseconds": {"std/time", "Time", "milliseconds"},
	"Duration.seconds":      {"std/time", "Time", "seconds"},
	"Instant.now":           {"std/time", "Time", "now"},
	"WallTime.now":          {"std/time", "Time", "wall_time"},
	"Task.sleep":            {"std/time", "Time", "sleep"},
	"Address.parse":         {"std/net", "Net", "parse_address"},
	"Dns.resolve":           {"std/net", "Net", "resolve"},
	"Tcp.connect":           {"std/net", "Net", "connect"},
	"Tcp.listen":            {"std/net", "Net", "listen"},
	"Process.start":         {"std/process", "Proc", "start"},
	"Terminal.is_attached":  {"std/terminal", "Term", "is_attached"},
	"Terminal.size":         {"std/terminal", "Term", "size"},
}

var movedConstructors = map[string]MovedOperation{
	"Signals": {"std/signal", "Sig", "subscribe"},
}

// TypeHint returns the migration diagnostic for an unresolved former
// protected type name, or a removed namespace-only name.
func TypeHint(name string) (string, bool) {
	if moved, ok := movedTypes[name]; ok {
		return fmt.Sprintf("%s is declared in %s; add `%s from \"%s\"` to the import block", name, moved.Module, moved.Alias, moved.Module), true
	}
	if removed, ok := removedNamespaces[name]; ok {
		return fmt.Sprintf("%s is removed; %s is a function in %s", name, removed.Function, removed.Module), true
	}
	return "", false
}

// OperationHint returns the migration diagnostic for an unresolved former
// static operation.
func OperationHint(owner, operation string) (string, bool) {
	moved, ok := movedOperations[owner+"."+operation]
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s.%s is now %s in %s; add `%s from \"%s\"` and call `%s.%s`",
		owner, operation, moved.Function, moved.Module, moved.Alias, moved.Module, moved.Alias, moved.Function), true
}

// ConstructorHint returns the migration diagnostic for an unresolved former
// fallible constructor such as Signals(...).
func ConstructorHint(name string) (string, bool) {
	moved, ok := movedConstructors[name]
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s is now %s in %s; add `%s from \"%s\"` and call `%s.%s`",
		name, moved.Function, moved.Module, moved.Alias, moved.Module, moved.Alias, moved.Function), true
}
