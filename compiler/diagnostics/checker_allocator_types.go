package diagnostics

import "fmt"

func AllocatorRequiresElementType(allocator string) Message {
	return message("type.allocator-requires-element-type", CategoryType, StageChecker,
		allocator+" requires exactly one element type")
}

func AllocatorElementMustBeAllocatable(allocator, element string) Message {
	return message("type.allocator-element-not-allocatable", CategoryType, StageChecker,
		fmt.Sprintf("%s element type must be complete, finite, and valid for allocation; got %s", allocator, element))
}

func PoolConstructorUsage() Message {
	return message("type.pool-constructor-usage", CategoryType, StageChecker,
		"Pool requires 1 argument (capacity); use Pool<T>(capacity)")
}

func PoolCapacityMustBeSize() Message {
	return message("type.pool-capacity-not-size", CategoryType, StageChecker, "Pool capacity must be a Size")
}

func PoolCapacityMustBePositive() Message {
	return message("type.pool-capacity-not-positive", CategoryType, StageChecker, "Pool capacity must be positive")
}

func UnknownPoolMethod(name string) Message {
	return message("type.unknown-pool-method", CategoryType, StageChecker,
		"Pool has no method "+name+"; use allocate, free, or destroy")
}

func UnknownStashMethod(name string) Message {
	return message("type.unknown-stash-method", CategoryType, StageChecker,
		"Stash has no method "+name+"; use allocate, reset, or destroy")
}

func FixedElementAllocatorRejectsTypeArguments(allocator string) Message {
	return message("type.fixed-element-allocator-no-type-arguments", CategoryType, StageChecker,
		allocator+" allocation accepts no type arguments; its element type is fixed by the receiver")
}

func AllocationRequiresOneArgument(operation string) Message {
	return message("type.allocation-requires-one-argument", CategoryType, StageChecker, operation+" expects 1 argument")
}

func AllocationInitializerType(allocator, expected, actual string) Message {
	return message("type.allocation-initializer-type", CategoryType, StageChecker,
		fmt.Sprintf("%s allocation initializer requires %s; got %s", allocator, expected, actual))
}

func PoolFreeRequiresPointer() Message {
	return message("type.pool-free-requires-pointer", CategoryType, StageChecker, "free expects 1 argument (pointer)")
}

func PoolFreePointerType(element, actual string) Message {
	return message("type.pool-free-pointer-type", CategoryType, StageChecker,
		fmt.Sprintf("Pool free requires Ptr<%s> or Ptr<mut %s>; got %s", element, element, actual))
}

func PoolPointerFromDifferentPool() Message {
	return message("type.pool-pointer-from-different-pool", CategoryType, StageChecker,
		"pointer was allocated from a different Pool")
}

func AllocatorOperationNoArguments(operation string) Message {
	return message("type.allocator-operation-no-arguments", CategoryType, StageChecker,
		operation+" expects no arguments")
}

func PoolHasLiveTrackedSlot() Message {
	return message("type.pool-has-live-tracked-slot", CategoryType, StageChecker,
		"Pool cannot be destroyed while a locally tracked slot is live")
}

func StashConstructorUsage() Message {
	return message("type.stash-constructor-usage", CategoryType, StageChecker,
		"Stash takes no arguments; use Stash<T>()")
}

func StashAllocationsReleasedByResetOrDestroy() Message {
	return message("type.stash-release-lifecycle", CategoryType, StageChecker,
		"Stash allocations are released by reset or destroy")
}
