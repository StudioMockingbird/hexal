package diagnostics

type PrintUnsupportedKind uint8

const (
	PrintUnsupportedDirect PrintUnsupportedKind = iota
	PrintUnsupportedObjectMember
	PrintUnsupportedAggregateElement
	PrintUnsupportedDictKey
	PrintUnsupportedDictValue
	PrintUnsupportedUnion
)

type PrintUnsupportedDetails struct {
	Kind      PrintUnsupportedKind
	Member    string
	ValueType string
}

func PrintTypeArgumentsNotAllowed() Message {
	return message("type.print-type-arguments", CategoryType, StageChecker, "print does not take type arguments")
}

func PrintRequiresArgument() Message {
	return message("type.print-requires-argument", CategoryType, StageChecker, "print expects at least 1 argument")
}

func PrintForeignRecordNotSupported(name string) Message {
	return message("type.print-foreign-record", CategoryType, StageChecker, "print does not support foreign record "+name)
}

func PrintValueNotSupported(name string, details PrintUnsupportedDetails) Message {
	text := "print does not support " + name
	switch details.Kind {
	case PrintUnsupportedObjectMember:
		text += " because " + details.Member + " is " + details.ValueType
	case PrintUnsupportedAggregateElement:
		text += " because its element is " + details.ValueType
	case PrintUnsupportedDictKey:
		text += " because its key is " + details.ValueType
	case PrintUnsupportedDictValue:
		text += " because its value is " + details.ValueType
	case PrintUnsupportedUnion:
		text += "; narrow or match it first"
	}
	return message("type.print-value-unsupported", CategoryType, StageChecker, text)
}
