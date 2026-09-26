package diagnostics

import "fmt"

func FileOperationUnsupported() Message {
	return message("type.file-operation-unsupported", CategoryType, StageChecker, "File has no such operation; use File.open(path, mode)")
}

func FileOpenArity(got int) Message {
	return message("type.file-open-arity", CategoryType, StageChecker,
		fmt.Sprintf("open expects 2 arguments (path: String, mode: FileMode); got %d", got))
}

func FileMethodNotFound(name string) Message {
	return message("type.file-method-not-found", CategoryType, StageChecker,
		"File has no method "+name+"; use read, write, seek, flush, or close")
}

func FileMethodNoTypeArguments(name string) Message {
	return message("type.file-method-no-type-arguments", CategoryType, StageChecker, name+" takes no type arguments")
}

func OnlyFileCloseMayBeDeferred() Message {
	return message("type.file-only-close-may-be-deferred", CategoryType, StageChecker, "only File.close() may be deferred on a file")
}
