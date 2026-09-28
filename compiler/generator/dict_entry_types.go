package generator

import (
	"slices"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func discoverSharedDictEntries(program checker.Program) (map[*compilerTypes.ObjectType]bool, error) {
	shared := make(map[*compilerTypes.ObjectType]bool)
	visitor := &programVisitor{Type: func(typ compilerTypes.Type) error {
		if compilerTypes.IsDictEntry(typ) && !typeIsModuleEmitted(typ) {
			shared[typ.Object] = true
		}
		return nil
	}}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return shared, nil
}

func sharedDictEntryDefinitions(merged *programEmission) (string, error) {
	objects := merged.sharedDictEntries
	if len(objects) == 0 {
		return "", nil
	}
	var result strings.Builder
	if err := writeObjectForwardDeclarations(&result, objects, ""); err != nil {
		return "", err
	}
	if err := writeNominalBodies(&result, objects, nil, nil, "", merged.tags); err != nil {
		return "", err
	}
	return result.String(), nil
}

func uniqueObjectsByCName(objects []*compilerTypes.ObjectType) []*compilerTypes.ObjectType {
	byName := make(map[string]*compilerTypes.ObjectType, len(objects))
	for _, object := range objects {
		if previous, exists := byName[object.CName]; exists && previous != object {
			return nil
		}
		byName[object.CName] = object
	}
	result := make([]*compilerTypes.ObjectType, 0, len(byName))
	for _, object := range byName {
		result = append(result, object)
	}
	slices.SortFunc(result, func(left, right *compilerTypes.ObjectType) int {
		return strings.Compare(left.CName, right.CName)
	})
	return result
}

func orderedSharedTypeComponents(headers []string) []string {
	seen := make(map[string]bool, len(headers))
	for _, header := range headers {
		seen[header] = true
	}
	result := make([]string, 0, len(seen))
	for _, header := range []string{
		"hexal/wrap.h", "hexal/heap.h", "hexal/slice.h", "hexal/string.h", "hexal/error.h",
		"hexal/program.h", "hexal/seek.h", "hexal/concurrency.h", "hexal/stash.h", "hexal/pool.h",
		"hexal/list.h", "hexal/dict.h", "hexal/numeric.h", "hexal/print.h", "hexal/io.h",
		"hexal/file.h", "hexal/network.h", "hexal/process.h", "hexal/signal.h", "hexal/terminal.h",
		"hexal/time.h", "hexal/equality.h",
	} {
		if seen[header] {
			result = append(result, header)
		}
	}
	return result
}

type sharedTypeHeaderModel struct {
	Includes    []string
	Definitions string
}

func typesComponents(merged *programEmission) ([]componentArtifact, error) {
	if len(merged.sharedDictEntries) == 0 {
		return nil, nil
	}
	definitions, err := sharedDictEntryDefinitions(merged)
	if err != nil {
		return nil, err
	}
	return []componentArtifact{{key: "hexal/types.h", template: "types.h", model: sharedTypeHeaderModel{
		Includes: merged.sharedTypeComponents, Definitions: definitions,
	}}}, nil
}
