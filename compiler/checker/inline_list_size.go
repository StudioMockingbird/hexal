package checker

import (
	"math"

	"hexal/compiler/config"
	compilerTypes "hexal/compiler/types"
)

func inlineListWithinBudget(element compilerTypes.Type, capacity uint64) bool {
	if compilerTypes.ContainsTypeParameter(element) {
		return true
	}
	size, ok := estimatedStorageBytes(element, make(map[string]bool))
	if !ok || size == 0 || capacity > (math.MaxUint64-16)/size {
		return false
	}
	total := uint64(16) + capacity*size
	return total < config.MaxInlineListEstimatedBytes
}

func estimatedStorageBytes(typ compilerTypes.Type, visiting map[string]bool) (uint64, bool) {
	if typ.ScalarKind != compilerTypes.ScalarNone {
		width := uint64((typ.Bits + 7) / 8)
		if width == 0 {
			width = 1
		}
		return width, true
	}
	if compilerTypes.IsEoS(typ) {
		return 1, true
	}
	if compilerTypes.IsNil(typ) {
		return 8, true
	}
	if typ.Element != nil || typ.Signature != nil || typ.NullableBase != nil {
		return 8, true
	}
	if typ.Slice != nil {
		return 16, true
	}
	if typ.InlineString != nil {
		return checkedAdd(8, typ.InlineString.Capacity)
	}
	if typ.InlineList != nil {
		element, ok := estimatedStorageBytes(typ.InlineList.Element, visiting)
		if !ok || typ.InlineList.Capacity > (math.MaxUint64-16)/element {
			return 0, false
		}
		return 16 + typ.InlineList.Capacity*element, true
	}
	if estimatedHandle(typ) {
		return 32, true
	}
	if typ.Object != nil {
		if typ.Object.Incomplete || visiting[typ.CanonicalKey] {
			return 0, false
		}
		visiting[typ.CanonicalKey] = true
		total := uint64(0)
		for _, member := range typ.Object.Members {
			size, ok := estimatedStorageBytes(member.Type, visiting)
			if !ok {
				delete(visiting, typ.CanonicalKey)
				return 0, false
			}
			total, ok = checkedAdd(total, size)
			if !ok {
				delete(visiting, typ.CanonicalKey)
				return 0, false
			}
			total, ok = checkedAdd(total, 16)
			if !ok {
				delete(visiting, typ.CanonicalKey)
				return 0, false
			}
		}
		delete(visiting, typ.CanonicalKey)
		return roundUp16(total)
	}
	if typ.Adt != nil || typ.Union != nil {
		if typ.CanonicalKey == "" || visiting[typ.CanonicalKey] {
			return 0, false
		}
		visiting[typ.CanonicalKey] = true
		defer delete(visiting, typ.CanonicalKey)
		largest := uint64(0)
		visitPayload := func(members []compilerTypes.ObjectMember) bool {
			total := uint64(0)
			for _, member := range members {
				size, ok := estimatedStorageBytes(member.Type, visiting)
				if !ok {
					return false
				}
				total, ok = checkedAdd(total, size)
				if !ok {
					return false
				}
			}
			if total > largest {
				largest = total
			}
			return true
		}
		if typ.Adt != nil {
			for _, variant := range typ.Adt.Variants {
				if !visitPayload(variant.Payload) {
					return 0, false
				}
			}
		} else {
			for _, member := range typ.Union.Members {
				size, ok := estimatedStorageBytes(member, visiting)
				if !ok {
					return 0, false
				}
				if size > largest {
					largest = size
				}
			}
		}
		total, ok := checkedAdd(16, largest)
		if !ok {
			return 0, false
		}
		return roundUp16(total)
	}
	return 0, false
}

func estimatedHandle(typ compilerTypes.Type) bool {
	return compilerTypes.IsHeap(typ) || compilerTypes.IsString(typ) || typ.List != nil || typ.Dict != nil ||
		typ.Task != nil || typ.Channel != nil || compilerTypes.IsMutex(typ) || compilerTypes.IsFile(typ) ||
		typ.Stash != nil || typ.Pool != nil || compilerTypes.IsProcess(typ) || compilerTypes.IsPipe(typ) ||
		compilerTypes.IsSignals(typ) || compilerTypes.IsTcpConnection(typ) || compilerTypes.IsTcpListener(typ)
}

func checkedAdd(left, right uint64) (uint64, bool) {
	if right > math.MaxUint64-left {
		return 0, false
	}
	return left + right, true
}

func roundUp16(size uint64) (uint64, bool) {
	if size > math.MaxUint64-15 {
		return 0, false
	}
	return (size + 15) &^ 15, true
}
