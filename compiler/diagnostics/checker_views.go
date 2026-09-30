package diagnostics

import "fmt"

// StaleCollectionView reports a use of a pointer, Slice, or cursor whose
// backing List or String storage was structurally changed at line:column.
func StaleCollectionView(kind, root string, line, column int) Message {
	return message("type.stale-collection-view", CategoryType, StageChecker,
		fmt.Sprintf("this %s points into %s's storage, which was structurally changed at %d:%d", kind, root, line, column))
}

// CollectionViewPassedWithRoot reports a call that receives a view of a
// storage root and can also change that root or let the view escape.
func CollectionViewPassedWithRoot(root string) Message {
	return message("type.collection-view-passed-with-root", CategoryType, StageChecker,
		"call receives a view of "+root+" and can also change "+root)
}

// CollectionViewEscapesRoot reports a view hidden in storage the checker does
// not follow.
func CollectionViewEscapesRoot(root string) Message {
	return message("type.collection-view-escapes-root", CategoryType, StageChecker,
		"view of "+root+" escapes storage tracked by the compiler; place this operation in unsafe only when the root outlives every use")
}
