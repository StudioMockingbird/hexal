package types

import "hexal/compiler/specdata"

// ResolveSpecID maps one compiler-owned type identifier from the fact registry
// to its canonical interned Type. The registry imports no compiler package, so
// it stores identifiers; this adapter is the one place that knows both spaces
// and keeps Type values on this side of the boundary. The registry's
// ConcreteTypeIDs inventory lists exactly the identifiers that must resolve,
// and a compiler-side test pins the two together.
//
// A false result means the registry named a type this compiler does not
// define. That is a compiler-development defect: a caller reports a structured
// Unknown Error rather than a user diagnostic. Constructor identifiers resolve
// to false by design, because only applying arguments to a constructor yields
// a concrete Type.
func ResolveSpecID(id specdata.TypeID) (Type, bool) {
	switch id {
	case specdata.TypeBool:
		return Bool, true
	case specdata.TypeInt8:
		return Int8, true
	case specdata.TypeInt16:
		return Int16, true
	case specdata.TypeInt32:
		return Int32, true
	case specdata.TypeInt64:
		return Int64, true
	case specdata.TypeUInt8, specdata.TypeByte:
		return UInt8, true
	case specdata.TypeUInt16:
		return UInt16, true
	case specdata.TypeUInt32, specdata.TypeUInt:
		return UInt32, true
	case specdata.TypeUInt64:
		return UInt64, true
	case specdata.TypeInt:
		return Int32, true
	case specdata.TypeRune:
		return Rune, true
	case specdata.TypeFloat32:
		return Float32, true
	case specdata.TypeFloat64, specdata.TypeFloat:
		return Float64, true
	case specdata.TypeSize:
		return SizeType, true
	case specdata.TypeNil:
		return Nil, true
	case specdata.TypeEoS:
		return EoS, true
	case specdata.TypeUnknown:
		return Unknown, true
	case specdata.TypeHeap:
		return Heap, true
	case specdata.TypeString:
		return StringType, true
	case specdata.TypeError:
		return ErrorType, true
	case specdata.TypeMutex:
		return MutexType, true
	case specdata.TypeByteCursor:
		return ByteCursorType, true
	case specdata.TypeRuneCursor:
		return RuneCursorType, true
	case specdata.TypeGrapheme:
		return GraphemeType, true
	case specdata.TypeGraphemeCursor:
		return GraphemeCursorType, true
	case specdata.TypeErrorKind:
		return ErrorKindType, true
	case specdata.TypeNormalization:
		return NormalizationFormType, true
	case specdata.TypeUnicodeCategory:
		return UnicodeCategoryType, true
	case specdata.TypeIO:
		return IOType, true
	case specdata.TypeBytes:
		return BytesType, true
	case specdata.TypeSeek:
		return SeekType, true
	case specdata.TypeFile:
		return FileType, true
	case specdata.TypeFileMode:
		return FileModeType, true
	case specdata.TypeDuration:
		return DurationType, true
	case specdata.TypeInstant:
		return InstantType, true
	case specdata.TypeWallTime:
		return WallTimeType, true
	case specdata.TypeAddress:
		return AddressType, true
	case specdata.TypeTcpConnection:
		return TcpConnectionType, true
	case specdata.TypeTcpListener:
		return TcpListenerType, true
	case specdata.TypeProcess:
		return ProcessType, true
	case specdata.TypePipe:
		return PipeType, true
	case specdata.TypeProcessOptions:
		return ProcessOptionsType, true
	case specdata.TypeStartedProcess:
		return StartedProcessType, true
	case specdata.TypeEnvironment:
		return EnvironmentType, true
	case specdata.TypeEnvironmentVariable:
		return EnvironmentVariableType, true
	case specdata.TypeProcessStream:
		return ProcessStreamType, true
	case specdata.TypeExitStatus:
		return ExitStatusType, true
	case specdata.TypeSignal:
		return SignalType, true
	case specdata.TypeSignals:
		return SignalsType, true
	case specdata.TypeTerminalSize:
		return TerminalSizeType, true
	default:
		return Type{}, false
	}
}
