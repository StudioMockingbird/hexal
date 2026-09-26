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

func EndianFromBytesInvalidType(owner string) Message {
	return message("type.endian-from-bytes-invalid-type", CategoryType, StageChecker,
		owner+" has no such operation; from_le_bytes and from_be_bytes require a fixed-width integer type")
}

func EndianFromBytesArgumentCount(operation string) Message {
	return message("type.endian-from-bytes-argument-count", CategoryType, StageChecker, operation+" expects exactly 1 argument")
}

func EndianFromBytesTypeMismatch(owner, order string, width int, actual string) Message {
	return message("type.endian-from-bytes-type-mismatch", CategoryType, StageChecker,
		fmt.Sprintf("%s.from_%s expects Array<Byte, %d>; got %s", owner, order, width, actual))
}
