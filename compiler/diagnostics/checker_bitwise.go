package diagnostics

import "fmt"

func AtomicValueCannotBeCopied() Message {
	return message("type.atomic-not-copyable", CategoryType, StageChecker, "Atomic values cannot be copied, assigned, addressed, or stored here")
}

func BitCastTypeArgumentCount() Message {
	return message("type.bit-cast-type-argument-count", CategoryType, StageChecker, "bit_cast requires exactly 1 explicit type argument")
}

func BitCastValueArgumentCount() Message {
	return message("type.bit-cast-value-argument-count", CategoryType, StageChecker, "bit_cast accepts no value arguments")
}

func BitCastIneligibleTypes(source, target string) Message {
	return message("type.bit-cast-ineligible-types", CategoryType, StageChecker,
		"bit_cast requires equal-width eligible scalar types; got "+source+" and "+target)
}

func BitCastWidthMismatch(source, target string) Message {
	return message("type.bit-cast-width-mismatch", CategoryType, StageChecker,
		fmt.Sprintf("bit_cast requires equal-width eligible scalar types; got %s and %s", source, target))
}

func EndianConversionNoArguments(name string) Message {
	return message("type.endian-conversion-no-arguments", CategoryType, StageChecker, name+" takes no arguments")
}

func EndianConversionInvalidReceiver(operation, receiver string) Message {
	return message("type.endian-conversion-invalid-receiver", CategoryType, StageChecker,
		operation+" requires a fixed-width integer receiver; got "+receiver)
}

func EndianDecodeInvalidType(operation, target string) Message {
	return message("type.endian-decode-invalid-type", CategoryType, StageChecker,
		operation+" requires a fixed-width integer type argument; got "+target)
}

func EndianDecodeArgumentCount(operation string) Message {
	return message("type.endian-decode-argument-count", CategoryType, StageChecker, operation+" expects exactly 1 type argument and no value arguments")
}

func EndianDecodeTypeMismatch(operation, target string, width int, actual string) Message {
	return message("type.endian-decode-type-mismatch", CategoryType, StageChecker,
		fmt.Sprintf("%s<%s> requires a List<Byte, %d> receiver; got %s", operation, target, width, actual))
}
