package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedDictState records the dictionary types that need header and
// helper definitions, in deterministic order.
type generatedDictState struct {
	order []compilerTypes.Type
	seen  map[*compilerTypes.DictInfo]bool
}

// discoverGeneratedDicts walks every type reachable from the program and
// collects the distinct dictionary types. Discovery order is then sorted by
// C name so the generated header is deterministic.
func discoverGeneratedDicts(program checker.Program) (*generatedDictState, error) {
	state := &generatedDictState{seen: make(map[*compilerTypes.DictInfo]bool)}
	seenObjects := make(map[*compilerTypes.ObjectType]bool)
	seenADTs := make(map[*compilerTypes.AdtType]bool)
	var walkType func(compilerTypes.Type) error
	var walkOperand func(checker.Operand) error
	var walkExpression func(checker.Expression) error
	var walkStatements func([]checker.Statement) error
	walkType = func(typ compilerTypes.Type) error {
		if typ.Dict != nil {
			if !state.seen[typ.Dict] {
				state.seen[typ.Dict] = true
				state.order = append(state.order, typ)
			}
			if err := walkType(typ.Dict.Key); err != nil {
				return err
			}
			return walkType(typ.Dict.Value)
		}
		if typ.List != nil {
			return walkType(typ.List.Element)
		}
		if typ.View != nil {
			return walkType(typ.View.Element)
		}
		if typ.Array != nil {
			return walkType(typ.Array.Element)
		}
		if typ.Union != nil {
			for _, member := range typ.Union.Members {
				if err := walkType(member); err != nil {
					return err
				}
			}
		}
		if typ.NullableBase != nil {
			return walkType(*typ.NullableBase)
		}
		if typ.Element != nil {
			return walkType(*typ.Element)
		}
		if typ.Signature != nil {
			for _, parameter := range typ.Signature.Parameters {
				if err := walkType(parameter); err != nil {
					return err
				}
			}
			if typ.Signature.Result != nil {
				return walkType(*typ.Signature.Result)
			}
		}
		if typ.Object != nil {
			if seenObjects[typ.Object] {
				return nil
			}
			seenObjects[typ.Object] = true
			for _, member := range typ.Object.Members {
				if err := walkType(member.Type); err != nil {
					return err
				}
			}
		}
		if typ.Adt != nil {
			if seenADTs[typ.Adt] {
				return nil
			}
			seenADTs[typ.Adt] = true
			for _, variant := range typ.Adt.Variants {
				for _, member := range variant.Payload {
					if err := walkType(member.Type); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	walkExpression = func(node checker.Expression) error {
		if err := walkType(node.OperandType); err != nil {
			return err
		}
		if err := walkType(node.ResultType); err != nil {
			return err
		}
		if node.Element != (compilerTypes.Type{}) {
			if err := walkType(node.Element); err != nil {
				return err
			}
		}
		if node.TestType != (compilerTypes.Type{}) {
			if err := walkType(node.TestType); err != nil {
				return err
			}
		}
		if node.Operand != nil {
			if err := walkExpression(*node.Operand); err != nil {
				return err
			}
		}
		if node.Left != nil {
			if err := walkExpression(*node.Left); err != nil {
				return err
			}
		}
		if node.Right != nil {
			if err := walkExpression(*node.Right); err != nil {
				return err
			}
		}
		for _, argument := range node.Arguments {
			if err := walkOperand(argument); err != nil {
				return err
			}
		}
		return nil
	}
	walkOperand = func(source checker.Operand) error {
		if err := walkType(source.Type); err != nil {
			return err
		}
		switch source.Kind {
		case checker.ObjectOperand:
			if source.Object != nil {
				for _, initializer := range source.Object.Initializers {
					if err := walkOperand(initializer.Source); err != nil {
						return err
					}
				}
			}
		case checker.VariableOperand, checker.ExpressionOperand:
			return walkExpression(source.Node)
		}
		return nil
	}
	walkStatements = func(statements []checker.Statement) error {
		for _, statement := range statements {
			switch statement := statement.(type) {
			case checker.Declaration:
				if err := walkType(statement.Type); err != nil {
					return err
				}
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
			case checker.Assignment:
				if err := walkType(statement.Type); err != nil {
					return err
				}
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
				if err := walkOperand(statement.Target); err != nil {
					return err
				}
			case checker.CallStatement:
				if err := walkExpression(statement.Call.Node); err != nil {
					return err
				}
			case checker.ReturnStatement:
				if statement.Value != nil {
					if err := walkOperand(*statement.Value); err != nil {
						return err
					}
				}
			case checker.IfStatement:
				if err := walkOperand(statement.Condition); err != nil {
					return err
				}
				if err := walkStatements(statement.Then); err != nil {
					return err
				}
				for _, branch := range statement.ElseIf {
					if err := walkOperand(branch.Condition); err != nil {
						return err
					}
					if err := walkStatements(branch.Body); err != nil {
						return err
					}
				}
				if statement.Else != nil {
					if err := walkStatements(statement.Else); err != nil {
						return err
					}
				}
			case checker.ForStatement:
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.WhileStatement:
				if err := walkOperand(statement.Condition); err != nil {
					return err
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.FunctionDeclaration:
				if err := walkType(statement.Type); err != nil {
					return err
				}
				for _, parameter := range statement.Parameters {
					if err := walkType(parameter.Type); err != nil {
						return err
					}
				}
				if statement.Result != nil {
					if err := walkType(*statement.Result); err != nil {
						return err
					}
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.MethodDeclaration:
				if err := walkType(statement.SelfType); err != nil {
					return err
				}
				for _, parameter := range statement.Parameters {
					if err := walkType(parameter.Type); err != nil {
						return err
					}
				}
				if statement.Result != nil {
					if err := walkType(*statement.Result); err != nil {
						return err
					}
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, declaration := range program.TypeDeclarations {
		if err := walkType(declaration.Type); err != nil {
			return nil, err
		}
	}
	if err := walkStatements(program.Statements); err != nil {
		return nil, err
	}
	for _, function := range program.SpecializedFunctions {
		if err := walkType(function.Type); err != nil {
			return nil, err
		}
		for _, parameter := range function.Parameters {
			if err := walkType(parameter.Type); err != nil {
				return nil, err
			}
		}
		if err := walkStatements(function.Body); err != nil {
			return nil, err
		}
	}
	for _, method := range program.SpecializedMethods {
		if err := walkType(method.SelfType); err != nil {
			return nil, err
		}
		for _, parameter := range method.Parameters {
			if err := walkType(parameter.Type); err != nil {
				return nil, err
			}
		}
		if err := walkStatements(method.Body); err != nil {
			return nil, err
		}
	}
	sort.SliceStable(state.order, func(left, right int) bool {
		return state.order[left].CName < state.order[right].CName
	})
	return state, nil
}

// dictSuffix returns the key-and-value-derived suffix of one dictionary
// type's C names.
func dictSuffix(dict compilerTypes.Type) string {
	return strings.TrimPrefix(dict.CName, "hex_dict_")
}

// writeDictDefinitions emits the hex_strand value type once, plus one entry
// struct and the hash/probe/insert/get/contains/remove/free helpers per
// dictionary type. Dictionary String values are copied in, borrowed out, and
// moved out on remove exactly like List<String> elements.
func writeDictDefinitions(result *strings.Builder, dicts *generatedDictState) {
	if dicts == nil {
		return
	}
	hashInt32Written := false
	hashStrandWritten := false
	for _, dict := range dicts.order {
		key := dict.Dict.Key
		value := dict.Dict.Value
		keySpelling := typeSpelling(key)
		valueSpelling := typeSpelling(value)
		suffix := dictSuffix(dict)
		entryName := "hex_dict_entry_" + suffix
		fmt.Fprintf(result, "\ntypedef struct %s {\n    bool active;\n    %s key;\n    %s value;\n} %s;\n", entryName, keySpelling, valueSpelling, entryName)
		fmt.Fprintf(result, "typedef struct %s {\n    %s *buckets;\n    size_t length;\n    size_t capacity;\n    uintptr_t allocator;\n} %s;\n", dict.CName, entryName, dict.CName)
		if compilerTypes.IsStrand(key) {
			hashStrandWritten = writeStrandHashHelper(result, hashStrandWritten)
			fmt.Fprintf(result, "static inline bool hex_dict_key_equal_%s(hex_strand left, hex_strand right) {\n    for (size_t index = 0; index < 32; index++) {\n        if (left.data[index] != right.data[index]) {\n            return false;\n        }\n    }\n    return true;\n}\n", suffix)
		} else {
			hashInt32Written = writeInt32HashHelper(result, hashInt32Written)
		}
		fmt.Fprintf(result, "static inline uint64_t hex_dict_probe_%s_region(%s *region, uint64_t capacity, %s key) {\n", suffix, entryName, keySpelling)
		if compilerTypes.IsStrand(key) {
			result.WriteString("    uint64_t hash = hex_hash_Strand(key);\n")
		} else {
			result.WriteString("    uint64_t hash = hex_hash_Int32(key);\n")
		}
		result.WriteString("    size_t index = hash & (capacity - 1);\n")
		if compilerTypes.IsStrand(key) {
			fmt.Fprintf(result, "    while (region[index].active && !hex_dict_key_equal_%s(region[index].key, key)) {\n        index = (index + 1) & (capacity - 1);\n    }\n", suffix)
		} else {
			result.WriteString("    while (region[index].active && region[index].key != key) {\n        index = (index + 1) & (capacity - 1);\n    }\n")
		}
		result.WriteString("    return index;\n}\n")
		fmt.Fprintf(result, "static inline uint64_t hex_dict_probe_%s(const %s *dict, %s key) {\n", suffix, dict.CName, keySpelling)
		if compilerTypes.IsStrand(key) {
			result.WriteString("    uint64_t hash = hex_hash_Strand(key);\n")
		} else {
			result.WriteString("    uint64_t hash = hex_hash_Int32(key);\n")
		}
		result.WriteString("    size_t index = hash & (dict->capacity - 1);\n")
		if compilerTypes.IsStrand(key) {
			fmt.Fprintf(result, "    while (dict->buckets[index].active && !hex_dict_key_equal_%s(dict->buckets[index].key, key)) {\n        index = (index + 1) & (dict->capacity - 1);\n    }\n", suffix)
		} else {
			result.WriteString("    while (dict->buckets[index].active && dict->buckets[index].key != key) {\n        index = (index + 1) & (dict->capacity - 1);\n    }\n")
		}
		result.WriteString("    return index;\n}\n")
		fmt.Fprintf(result, "static inline %s *hex_dict_new_%s(hex_heap h) {\n", dict.CName, suffix)
		result.WriteString("    " + dict.CName + " *header = hex_heap_raw_allocate(h.identity, sizeof(" + dict.CName + "), _Alignof(" + dict.CName + "));\n")
		result.WriteString("    header->buckets = NULL;\n    header->length = 0;\n    header->capacity = 0;\n    header->allocator = h.identity;\n")
		fmt.Fprintf(result, "    return header;\n}\n")
		fmt.Fprintf(result, "static inline void hex_dict_grow_%s(%s *dict) {\n", suffix, dict.CName)
		fmt.Fprintf(result, "    uint64_t next = dict->capacity == 0 ? 8 : dict->capacity * 2;\n")
		result.WriteString("    if (next < dict->capacity || next > SIZE_MAX / sizeof(" + entryName + ")) {\n")
		result.WriteString("        fputs(\"[Runtime Error] dictionary capacity is not representable\\n\", stderr);\n        abort();\n    }\n")
		result.WriteString("    " + entryName + " *region = hex_heap_raw_allocate(dict->allocator, next * sizeof(" + entryName + "), _Alignof(" + entryName + "));\n")
		result.WriteString("    for (size_t index = 0; index < next; index++) {\n        region[index].active = false;\n    }\n")
		fmt.Fprintf(result, "    for (size_t index = 0; index < dict->capacity; index++) {\n        if (dict->buckets[index].active) {\n            uint64_t probe = hex_dict_probe_%s_region(region, next, dict->buckets[index].key);\n            region[probe] = dict->buckets[index];\n        }\n    }\n", suffix)
		result.WriteString("    free(dict->buckets);\n    dict->buckets = region;\n    dict->capacity = next;\n}\n")
		fmt.Fprintf(result, "static inline void hex_dict_insert_%s(%s *dict, %s key, %s value) {\n", suffix, dict.CName, keySpelling, valueSpelling)
		result.WriteString("    if (dict->capacity == 0 || (dict->length + 1) * 10 >= dict->capacity * 7) {\n        hex_dict_grow_" + suffix + "(dict);\n    }\n")
		result.WriteString("    size_t index = hex_dict_probe_" + suffix + "(dict, key);\n")
		fmt.Fprintf(result, "    if (dict->buckets[index].active) {\n")
		result.WriteString("        dict->buckets[index].value = value;\n")
		result.WriteString("        return;\n    }\n")
		fmt.Fprintf(result, "    dict->buckets[index].active = true;\n    dict->buckets[index].key = key;\n")
		result.WriteString("    dict->buckets[index].value = value;\n")
		result.WriteString("    dict->length++;\n}\n")
		fmt.Fprintf(result, "static inline %s hex_dict_get_%s(const %s *dict, %s key) {\n", valueSpelling, suffix, dict.CName, keySpelling)
		fmt.Fprintf(result, "    if (dict->capacity == 0) {\n        fputs(\"[Runtime Error] dictionary key not found\\n\", stderr);\n        abort();\n    }\n")
		result.WriteString("    size_t index = hex_dict_probe_" + suffix + "(dict, key);\n")
		fmt.Fprintf(result, "    if (!dict->buckets[index].active) {\n        fputs(\"[Runtime Error] dictionary key not found\\n\", stderr);\n        abort();\n    }\n")
		result.WriteString("    return dict->buckets[index].value;\n}\n")
		fmt.Fprintf(result, "static inline bool hex_dict_contains_%s(const %s *dict, %s key) {\n", suffix, dict.CName, keySpelling)
		fmt.Fprintf(result, "    if (dict->capacity == 0) {\n        return false;\n    }\n")
		result.WriteString("    size_t index = hex_dict_probe_" + suffix + "(dict, key);\n")
		result.WriteString("    return dict->buckets[index].active;\n}\n")
		fmt.Fprintf(result, "static inline %s hex_dict_remove_%s(%s *dict, %s key) {\n", valueSpelling, suffix, dict.CName, keySpelling)
		fmt.Fprintf(result, "    if (dict->capacity == 0) {\n        fputs(\"[Runtime Error] dictionary key not found\\n\", stderr);\n        abort();\n    }\n")
		result.WriteString("    size_t index = hex_dict_probe_" + suffix + "(dict, key);\n")
		fmt.Fprintf(result, "    if (!dict->buckets[index].active) {\n        fputs(\"[Runtime Error] dictionary key not found\\n\", stderr);\n        abort();\n    }\n")
		result.WriteString("    " + valueSpelling + " value = dict->buckets[index].value;\n")
		result.WriteString("    dict->buckets[index].active = false;\n    dict->length--;\n    return value;\n}\n")
		fmt.Fprintf(result, "static inline void hex_dict_free_%s(hex_heap h, %s *dict) {\n", suffix, dict.CName)
		result.WriteString("    if (dict == NULL || dict->allocator != h.identity) {\n")
		result.WriteString("        fputs(\"[Runtime Error] deallocation used the wrong allocator\\n\", stderr);\n        abort();\n    }\n")
		result.WriteString("    free(dict->buckets);\n    free(dict);\n}\n")
	}
}

// writeInt32HashHelper emits the Int32 hash once per header.
func writeInt32HashHelper(result *strings.Builder, written bool) bool {
	if written {
		return true
	}
	result.WriteString("\nstatic inline uint64_t hex_hash_Int32(int32_t key) {\n")
	result.WriteString("    uint64_t x = (size_t)(uint32_t)key + 0x9E3779B97F4A7C15ULL;\n")
	result.WriteString("    x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9ULL;\n")
	result.WriteString("    x = (x ^ (x >> 27)) * 0x94D049BB133111EBULL;\n")
	result.WriteString("    return x ^ (x >> 31);\n}\n")
	return true
}

// writeStrandHashHelper emits the Strand FNV-1a hash once per header. The
// full 32 zero-padded bytes are hashed; the NUL-free payload guarantees the
// padding does not collide distinct values.
func writeStrandHashHelper(result *strings.Builder, written bool) bool {
	if written {
		return true
	}
	result.WriteString("\nstatic inline uint64_t hex_hash_Strand(hex_strand key) {\n")
	result.WriteString("    uint64_t hash = 14695981039346656037ULL;\n")
	result.WriteString("    for (size_t index = 0; index < 32; index++) {\n")
	result.WriteString("        hash ^= key.data[index];\n        hash *= 1099511628211ULL;\n")
	result.WriteString("    }\n")
	result.WriteString("    return hash;\n}\n")
	return true
}
