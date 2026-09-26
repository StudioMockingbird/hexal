package diagnostics

import "fmt"

func TimeOperationsNoTypeArguments(owner string) Message {
	return message("type.time-no-type-arguments", CategoryType, StageChecker, owner+" operations take no type arguments")
}
func UnknownDurationOperation() Message {
	return message("type.unknown-duration-operation", CategoryType, StageChecker, "Duration has no such operation; use Duration.nanoseconds, microseconds, milliseconds, or seconds")
}
func DurationConstructorArity(name string, actual int) Message {
	return message("type.duration-constructor-arity", CategoryType, StageChecker, fmt.Sprintf("Duration.%s expects 1 argument (value: UInt64); got %d", name, actual))
}
func UnknownInstantOperation() Message {
	return message("type.unknown-instant-operation", CategoryType, StageChecker, "Instant has no such operation; use Instant.now()")
}
func UnknownWallTimeOperation() Message {
	return message("type.unknown-wall-time-operation", CategoryType, StageChecker, "WallTime has no such operation; use WallTime.now()")
}
func TaskSleepArity(actual int) Message {
	return message("type.task-sleep-arity", CategoryType, StageChecker, fmt.Sprintf("Task.sleep expects 1 argument (duration: Duration); got %d", actual))
}
func GenericTimeMethodUnsupported(receiver, name string) Message {
	return message("type.generic-time-method-unsupported", CategoryType, StageChecker, receiver+" has no generic method "+name+"; convert through its named accessors")
}
func UnknownDurationMethod(name string) Message {
	return message("type.unknown-duration-method", CategoryType, StageChecker, "Duration has no method "+name+"; use as_nanoseconds, as_microseconds, as_milliseconds, or as_seconds")
}
func InstantDurationSinceArity(actual int) Message {
	return message("type.instant-duration-since-arity", CategoryType, StageChecker, fmt.Sprintf("duration_since expects 1 argument (earlier: Instant); got %d", actual))
}
func UnknownInstantMethod(name string) Message {
	return message("type.unknown-instant-method", CategoryType, StageChecker, "Instant has no method "+name+"; use elapsed or duration_since")
}
func UnknownWallTimeMethod(name string) Message {
	return message("type.unknown-wall-time-method", CategoryType, StageChecker, "WallTime has no method "+name+"; use seconds or nanosecond")
}
func TimeOperatorRequiresIdenticalTypes(operator, left, right string) Message {
	return message("type.time-operator-identical-types", CategoryType, StageChecker, fmt.Sprintf("operator %s requires identical operand types; got %s and %s", operator, left, right))
}
func TimeOperatorUndefined(operator, typ string) Message {
	return message("type.time-operator-undefined", CategoryType, StageChecker, fmt.Sprintf("operator %s is not defined for %s", operator, typ))
}
