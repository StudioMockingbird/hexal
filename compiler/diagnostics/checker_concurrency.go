package diagnostics

import "fmt"

func TaskRequiresOneResultType() Message {
	return message("type.task-result-type-arity", CategoryType, StageChecker, "Task requires exactly one result type")
}
func TaskResultNotCopyable(name string) Message {
	return message("type.task-result-invalid-type", CategoryType, StageChecker, "Task result type must be complete and shallow-copyable; got "+name)
}
func ChannelRequiresOneElementType() Message {
	return message("type.channel-element-type-arity", CategoryType, StageChecker, "Channel requires exactly one element type")
}
func ChannelCannotCarryEOS() Message {
	return message("type.channel-eos-element", CategoryType, StageChecker, "Channel element cannot be or include EoS as a top-level member")
}
func ChannelContainsNonCopyableAtomic() Message {
	return message("type.channel-atomic-element", CategoryType, StageChecker, "Channel element contains a non-copyable Atomic value")
}
func ChannelElementNotCopyable(name string) Message {
	return message("type.channel-element-not-copyable", CategoryType, StageChecker, "Channel element must be complete and shallow-copyable; got "+name)
}
func AtomicRequiresOneElementType() Message {
	return message("type.atomic-element-type-arity", CategoryType, StageChecker, "Atomic requires exactly one element type")
}
func AtomicElementTypeUnsupported() Message {
	return message("type.atomic-element-type-unsupported", CategoryType, StageChecker, "Atomic element type is not supported; use Bool, Int32, UInt32, Int64, UInt64, or Size")
}
func SpawnForbiddenDuringCleanup() Message {
	return message("type.spawn-during-cleanup", CategoryType, StageChecker, "spawn is not permitted inside defer or errdefer")
}
func SpawnRequiresNamedFunctionCall() Message {
	return message("type.spawn-requires-named-function", CategoryType, StageChecker, "spawn requires a direct call to a named function")
}
func SpawnFunctionRequiresResult() Message {
	return message("type.spawn-function-requires-result", CategoryType, StageChecker, "spawn requires a function with a result")
}
func SpawnEntryEnvironmentFunction(name string) Message {
	return message("type.spawn-entry-environment-function", CategoryType, StageChecker, "function "+name+" uses the entry environment and is valid only as a direct entry-module call")
}
func TaskEntryArgumentsNotCopyable() Message {
	return message("type.task-entry-arguments-not-copyable", CategoryType, StageChecker, "task entry arguments must be complete and shallow-copyable")
}
func TaskResultMustBeCopyable() Message {
	return message("type.task-result-not-copyable", CategoryType, StageChecker, "Task result type must be complete and shallow-copyable")
}
func UnknownTaskOperation() Message {
	return message("type.unknown-task-operation", CategoryType, StageChecker, "Task has no such operation; use Task.yield()")
}
func TaskYieldOutsideFunction() Message {
	return message("type.task-yield-outside-function", CategoryType, StageChecker, "Task.yield() is valid only inside a function")
}
func UnknownTaskMethod(name string) Message {
	return message("type.unknown-task-method", CategoryType, StageChecker, "Task has no method "+name+"; use join or detach")
}
func ChannelConstructorUsage() Message {
	return message("type.channel-constructor-usage", CategoryType, StageChecker, "Channel requires 2 arguments (heap, capacity); use Channel<T>(heap, capacity)")
}
func ChannelRequiresHeap(actual string) Message {
	return message("type.channel-requires-heap", CategoryType, StageChecker, "Channel.new requires a Heap allocator; got "+actual)
}
func ChannelCapacityMustBeSize() Message {
	return message("type.channel-capacity-not-size", CategoryType, StageChecker, "Channel capacity must be a Size")
}
func ChannelCapacityMustBePositive() Message {
	return message("type.channel-capacity-not-positive", CategoryType, StageChecker, "compile-time Channel capacity must be positive")
}
func UnknownChannelMethod(name string) Message {
	return message("type.unknown-channel-method", CategoryType, StageChecker, "Channel has no method "+name+"; use send, receive, close, length, capacity, is_closed, or free")
}
func ChannelSendRequires(element, actual string) Message {
	return message("type.channel-send-type", CategoryType, StageChecker, fmt.Sprintf("Channel send requires %s; got %s", element, actual))
}
func FreeRequiresHeap(actual string) Message {
	return message("type.free-requires-heap", CategoryType, StageChecker, "free requires a Heap; got "+actual)
}
func FreeRequiresAllocatorArgument() Message {
	return message("type.free-allocator-argument", CategoryType, StageChecker, "free expects 1 argument (allocator)")
}
func MutexConstructorUsage() Message {
	return message("type.mutex-constructor-usage", CategoryType, StageChecker, "Mutex requires 1 argument (heap); use Mutex(heap)")
}
func MutexRequiresHeap(actual string) Message {
	return message("type.mutex-requires-heap", CategoryType, StageChecker, "Mutex.new requires a Heap allocator; got "+actual)
}
func UnknownMutexMethod(name string) Message {
	return message("type.unknown-mutex-method", CategoryType, StageChecker, "Mutex has no method "+name+"; use lock, unlock, or free")
}
func AtomicConstructorUsage() Message {
	return message("type.atomic-constructor-usage", CategoryType, StageChecker, "Atomic requires 1 argument (initial); use Atomic<T>(initial)")
}
func AtomicInitializerType(expected, actual string) Message {
	return message("type.atomic-initializer-type", CategoryType, StageChecker, fmt.Sprintf("Atomic.new requires %s; got %s", expected, actual))
}
func AtomicMethodMissing(owner, name string) Message {
	return message("type.atomic-method-missing", CategoryType, StageChecker, fmt.Sprintf("%s has no method named %s", owner, name))
}
func AtomicMethodArity(name string, got int) Message {
	return message("type.atomic-method-arity", CategoryType, StageChecker, fmt.Sprintf("%s expects %d argument(s)", name, got))
}
func AtomicMethodUnavailableForBool(name string) Message {
	return message("type.atomic-method-unavailable-bool", CategoryType, StageChecker, name+" is unavailable for Bool")
}
func AtomicOperationType(name, expected, actual string) Message {
	return message("type.atomic-operation-type", CategoryType, StageChecker, fmt.Sprintf("%s requires %s; got %s", name, expected, actual))
}
