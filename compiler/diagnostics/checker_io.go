package diagnostics

import "fmt"

func UnknownIOOperation() Message {
	return message("type.unknown-io-operation", CategoryType, StageChecker,
		"IO has no such operation; use IO.stdin(), IO.stdout(), or IO.stderr()")
}

func UnknownBytesOperation() Message {
	return message("type.unknown-bytes-operation", CategoryType, StageChecker,
		"Bytes has no such operation; use Bytes.over(buffer)")
}

func MemoryStreamOutlivesSourceList() Message {
	return message("type.memory-stream-outlives-source-list", CategoryType, StageChecker,
		"memory stream outlives its source list, freed on every path to this point")
}

func UnknownIOMethod(name string) Message {
	return message("type.unknown-io-method", CategoryType, StageChecker,
		"IO has no method "+name+"; use read, write, seek, or close")
}

func OnlyStreamCloseMayBeDeferred() Message {
	return message("type.only-stream-close-deferred", CategoryType, StageChecker,
		"only IO.close() may be deferred on a stream")
}

func UnknownBytesMethod(name string) Message {
	return message("type.unknown-bytes-method", CategoryType, StageChecker,
		"Bytes has no method "+name+"; use read, write, or seek")
}

func MemoryStreamOperationsCannotBeDeferred() Message {
	return message("type.memory-stream-operations-cannot-defer", CategoryType, StageChecker,
		"memory stream operations may not be deferred")
}

func IOReadArity(actual int) Message {
	return message("type.io-read-arity", CategoryType, StageChecker,
		fmt.Sprintf("read expects 2 arguments (into: List<Byte>, max: Size); got %d", actual))
}

func IOWriteArity(actual int) Message {
	return message("type.io-write-arity", CategoryType, StageChecker,
		fmt.Sprintf("write expects 1 argument (from: Slice<Byte>); got %d", actual))
}

func IOSeekArity(actual int) Message {
	return message("type.io-seek-arity", CategoryType, StageChecker,
		fmt.Sprintf("seek expects 1 argument (to: Seek); got %d", actual))
}

func StreamNotReadable() Message {
	return message("type.stream-not-readable", CategoryType, StageChecker, "stream is not readable")
}

func StreamNotWritable() Message {
	return message("type.stream-not-writable", CategoryType, StageChecker, "stream is not writable")
}

func StreamClosedOnEveryPath() Message {
	return message("type.stream-closed-on-every-path", CategoryType, StageChecker,
		"this stream was closed on every path to this point")
}
