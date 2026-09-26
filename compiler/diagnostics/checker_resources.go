package diagnostics

import "fmt"

func TerminalHasNoTypeArguments() Message {
	return message("type.terminal-no-type-arguments", CategoryType, StageChecker, "Terminal operations take no type arguments")
}

func TerminalOperationArity(operation string, actual int) Message {
	return message("type.terminal-operation-arity", CategoryType, StageChecker,
		fmt.Sprintf("%s expects 1 argument (stream: IO); got %d", operation, actual))
}

func UnknownTerminalOperation() Message {
	return message("type.unknown-terminal-operation", CategoryType, StageChecker,
		"Terminal has no such operation; use Terminal.is_attached or Terminal.size")
}

func SignalsConstructorArguments() Message {
	return message("type.signals-constructor-arguments", CategoryType, StageChecker,
		"Signals requires 1 argument (subscriptions: Slice<Signal>); use Signals(subscriptions)")
}

func UnknownSignalsMethod(name string) Message {
	return message("type.unknown-signals-method", CategoryType, StageChecker,
		"Signals has no method "+name+"; use next or close")
}

func SignalsMethodTakesNoArguments(name string) Message {
	return message("type.signals-method-no-arguments", CategoryType, StageChecker, name+" expects no arguments")
}

func OnlySignalsCloseMayBeDeferred() Message {
	return message("type.only-signals-close-deferred", CategoryType, StageChecker,
		"only Signals.close() may be deferred")
}

func UnknownProcessOperation() Message {
	return message("type.unknown-process-operation", CategoryType, StageChecker,
		"Process has no such operation; use Process.start(options)")
}

func ProcessStartArity(actual int) Message {
	return message("type.process-start-arity", CategoryType, StageChecker,
		fmt.Sprintf("start expects 1 argument (options: ProcessOptions); got %d", actual))
}

func UnknownProcessMethod(name string) Message {
	return message("type.unknown-process-method", CategoryType, StageChecker,
		"Process has no method "+name+"; use wait, terminate, or close")
}

func ProcessMethodTakesNoArguments(name string) Message {
	return message("type.process-method-no-arguments", CategoryType, StageChecker, name+" expects no arguments")
}

func OnlyProcessCloseMayBeDeferred() Message {
	return message("type.only-process-close-deferred", CategoryType, StageChecker,
		"only Process.close() may be deferred")
}

func UnknownPipeMethod(name string) Message {
	return message("type.unknown-pipe-method", CategoryType, StageChecker,
		"Pipe has no method "+name+"; use read, write, shutdown, or close")
}

func PipeMethodTakesNoTypeArguments(name string) Message {
	return message("type.pipe-method-no-type-arguments", CategoryType, StageChecker,
		name+" takes no type arguments")
}

func OnlyPipeCloseMayBeDeferred() Message {
	return message("type.only-pipe-close-deferred", CategoryType, StageChecker,
		"only Pipe.close() may be deferred")
}

func NoArgumentsExpected(name string) Message {
	return message("type.no-arguments-expected", CategoryType, StageChecker, name+" expects no arguments")
}
