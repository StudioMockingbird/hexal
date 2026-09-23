package specdata

import (
	"fmt"
	"sort"
	"strings"
)

// Cross-domain integrity is the layer above each domain's own validator. Every
// domain validates its own records; this pass validates the references those
// records make into other domains, from one place, so a domain that loses its
// own reference check cannot leave the gap silent. Three properties have no
// single owner and live here: the cross-domain reference graph, runtime-symbol
// ownership, and ABI-record completeness.
//
// It reads records through a value, so a test can supply crafted ones and
// Validate can supply the package registries. Validate calls it last: a domain
// must be internally sound before its references mean anything.

// integrityNode names one record or identity in the cross-domain graph. The
// domain separates identifier spaces that overlap by string, such as a TypeID
// naming both a constructor family and a concrete type.
type integrityNode struct {
	domain string
	id     string
}

// String renders a node for a cycle report.
func (node integrityNode) String() string {
	return node.domain + " " + node.id
}

// integrityEdge is one reference from a record to the record or identity it
// names. An edge whose target does not resolve is a missing-reference defect,
// so edges exist only between resolvable nodes.
type integrityEdge struct {
	from integrityNode
	to   integrityNode
}

func nodeComponent(id ComponentID) integrityNode {
	return integrityNode{domain: "component", id: string(id)}
}

func nodeDependency(id DependencyID) integrityNode {
	return integrityNode{domain: "dependency", id: string(id)}
}

func nodeTarget(id TargetID) integrityNode {
	return integrityNode{domain: "target", id: string(id)}
}

func nodeConstructor(id TypeID) integrityNode {
	return integrityNode{domain: "constructor", id: string(id)}
}

func nodeConcrete(id TypeID) integrityNode {
	return integrityNode{domain: "concrete type", id: string(id)}
}

func nodeErrorKind(id ErrorKindID) integrityNode {
	return integrityNode{domain: "error kind", id: string(id)}
}

func nodeMethod(owner, name string) integrityNode {
	return integrityNode{domain: "method", id: owner + "." + name}
}

func nodeCoreType(module CoreModuleID, name string) integrityNode {
	return integrityNode{domain: "core type", id: string(module) + "." + name}
}

func nodeCoreFunction(module CoreModuleID, name string) integrityNode {
	return integrityNode{domain: "core function", id: string(module) + "." + name}
}

func nodeScalarSet(target TargetID) integrityNode {
	return integrityNode{domain: "scalar set", id: string(target)}
}

func nodeScalar(target TargetID, spelling string) integrityNode {
	return integrityNode{domain: "scalar", id: string(target) + " " + spelling}
}

func nodeOperator(id OperatorID) integrityNode {
	return integrityNode{domain: "operator", id: string(id)}
}

func nodeRank(id TypeID) integrityNode {
	return integrityNode{domain: "rank", id: string(id)}
}

func nodeWidening(from, to TypeID) integrityNode {
	return integrityNode{domain: "widening", id: string(from) + "->" + string(to)}
}

func nodeConversion(from, to TypeID) integrityNode {
	return integrityNode{domain: "conversion", id: string(from) + "->" + string(to)}
}

// domainRecords is the slice set one cross-domain pass reads. It exists so a
// test can hand the pass crafted records without mutating the package's own
// registries; Validate supplies the real ones.
type domainRecords struct {
	dependencies []DependencySpec
	components   []ComponentSpec
	targets      []TargetFacts
	constructors []TypeConstructorSpec
	concretes    []TypeID
	methods      []MethodSpec
	modules      []CoreModule
	errorKinds   []ErrorKindSpec
	scalars      []TargetScalars
	operators    []OperatorSpec
	ranks        []NumericRankSpec
	widenings    []WideningSpec
	conversions  []ConversionSpec
}

// hasDependency reports whether the record set declares a native input.
func (records domainRecords) hasDependency(id DependencyID) bool {
	for _, dependency := range records.dependencies {
		if dependency.ID == id {
			return true
		}
	}
	return false
}

// component resolves one component record.
func (records domainRecords) component(id ComponentID) (ComponentSpec, bool) {
	for _, component := range records.components {
		if component.ID == id {
			return component, true
		}
	}
	return ComponentSpec{}, false
}

// hasComponent reports whether the record set declares a component.
func (records domainRecords) hasComponent(id ComponentID) bool {
	_, ok := records.component(id)
	return ok
}

// target resolves one target record.
func (records domainRecords) target(id TargetID) (TargetFacts, bool) {
	for _, facts := range records.targets {
		if facts.ID == id {
			return facts, true
		}
	}
	return TargetFacts{}, false
}

// hasTarget reports whether the record set declares a target.
func (records domainRecords) hasTarget(id TargetID) bool {
	_, ok := records.target(id)
	return ok
}

// hasConcrete reports whether the record set declares a concrete type identity.
func (records domainRecords) hasConcrete(id TypeID) bool {
	for _, candidate := range records.concretes {
		if candidate == id {
			return true
		}
	}
	return false
}

// constructor resolves one type-constructor record.
func (records domainRecords) constructor(id TypeID) (TypeConstructorSpec, bool) {
	for _, spec := range records.constructors {
		if spec.ID == id {
			return spec, true
		}
	}
	return TypeConstructorSpec{}, false
}

// hasConstructor reports whether the record set declares a constructor family.
func (records domainRecords) hasConstructor(id TypeID) bool {
	_, ok := records.constructor(id)
	return ok
}

// validateCrossDomain runs the cross-domain checks over the real registries.
func validateCrossDomain() error {
	return validateDomainSet(domainRecords{
		dependencies: dependencyRegistry,
		components:   componentRegistry,
		targets:      targetFacts,
		constructors: typeConstructors,
		concretes:    concreteTypeIDs,
		methods:      methods,
		modules:      coreModules,
		errorKinds:   errorKinds,
		scalars:      targetScalars,
		operators:    operators,
		ranks:        numericRanks,
		widenings:    wideningPairs,
		conversions:  conversions,
	})
}

// validateDomainSet reports the first cross-domain defect: an unresolved
// reference, a reference cycle, a runtime symbol whose sharers disagree about
// an owning fact, or an ABI-visible record that cannot carry its ABI role.
func validateDomainSet(records domainRecords) error {
	edges, err := records.references()
	if err != nil {
		return err
	}
	if cycle := findReferenceCycle(edges); cycle != nil {
		return fmt.Errorf("specdata/integrity: cross-domain reference cycle: %s", renderCycle(cycle))
	}
	if err := validateSymbolOwnership(records); err != nil {
		return err
	}
	return validateABICompleteness(records)
}

// references resolves every identifier one domain names in another and returns
// the resulting graph edges. It reports the first reference that names nothing
// the target domain declares. The edges are also the graph the cycle check
// walks, so one enumeration defines both checks.
func (records domainRecords) references() ([]integrityEdge, error) {
	var edges []integrityEdge

	for _, component := range records.components {
		if component.ID == "" {
			continue
		}
		from := nodeComponent(component.ID)
		for _, dependency := range component.RuntimeDependencies {
			if !records.hasDependency(dependency) {
				return nil, fmt.Errorf("specdata/integrity: component %q names unknown dependency %q", component.ID, dependency)
			}
			edges = append(edges, integrityEdge{from: from, to: nodeDependency(dependency)})
		}
	}

	for _, spec := range records.constructors {
		if spec.ID == "" {
			continue
		}
		if !records.hasComponent(spec.Facts.Component) {
			return nil, fmt.Errorf("specdata/integrity: type %q demands unknown component %q", spec.ID, spec.Facts.Component)
		}
		edges = append(edges, integrityEdge{from: nodeConstructor(spec.ID), to: nodeComponent(spec.Facts.Component)})
	}

	for _, method := range records.methods {
		where := "method " + ownerName(method.Owner) + "." + method.Name
		from := nodeMethod(ownerName(method.Owner), method.Name)
		switch {
		case method.Owner.Constructor != "":
			if !records.hasConstructor(method.Owner.Constructor) {
				return nil, fmt.Errorf("specdata/integrity: %s names unknown constructor %q", where, method.Owner.Constructor)
			}
			edges = append(edges, integrityEdge{from: from, to: nodeConstructor(method.Owner.Constructor)})
		case method.Owner.Exact != "":
			if !records.hasConcrete(method.Owner.Exact) {
				return nil, fmt.Errorf("specdata/integrity: %s names unknown concrete type %q", where, method.Owner.Exact)
			}
			edges = append(edges, integrityEdge{from: from, to: nodeConcrete(method.Owner.Exact)})
		}
		if !records.hasComponent(method.Component) {
			return nil, fmt.Errorf("specdata/integrity: %s demands unknown component %q", where, method.Component)
		}
		edges = append(edges, integrityEdge{from: from, to: nodeComponent(method.Component)})
		for index, parameter := range method.Parameters {
			var err error
			edges, err = records.appendTypeRef(edges, from, fmt.Sprintf("%s parameter %d", where, index), parameter.Type)
			if err != nil {
				return nil, err
			}
		}
		var err error
		edges, err = records.appendTypeRef(edges, from, where+" result", method.Result.Type)
		if err != nil {
			return nil, err
		}
	}

	for _, module := range records.modules {
		for _, export := range module.Types {
			from := nodeCoreType(module.ID, export.Name)
			id := TypeID(export.TypeID)
			switch {
			case records.hasConcrete(id):
				edges = append(edges, integrityEdge{from: from, to: nodeConcrete(id)})
			case records.hasConstructor(id):
				edges = append(edges, integrityEdge{from: from, to: nodeConstructor(id)})
			default:
				return nil, fmt.Errorf("specdata/integrity: module %q type %q names unknown type identifier %q", module.ID, export.Name, export.TypeID)
			}
		}
		for _, function := range module.Functions {
			if function.Runtime == "" {
				// A builtin routes to a checker operation that owns its
				// runtime demand, so its record names no component.
				continue
			}
			from := nodeCoreFunction(module.ID, function.Name)
			for _, component := range function.Components {
				if !records.hasComponent(component) {
					return nil, fmt.Errorf("specdata/integrity: function %q.%q names unknown component %q", module.ID, function.Name, component)
				}
				edges = append(edges, integrityEdge{from: from, to: nodeComponent(component)})
			}
		}
	}

	for _, kind := range records.errorKinds {
		if kind.HeaderType == "" {
			continue
		}
		if !records.hasConstructor(kind.HeaderType) {
			return nil, fmt.Errorf("specdata/integrity: error kind %q names unknown header constructor %q", kind.ID, kind.HeaderType)
		}
		edges = append(edges, integrityEdge{from: nodeErrorKind(kind.ID), to: nodeConstructor(kind.HeaderType)})
	}

	for _, set := range records.scalars {
		if !records.hasTarget(set.Target) {
			return nil, fmt.Errorf("specdata/integrity: scalar mapping set names unknown target %q", set.Target)
		}
		edges = append(edges, integrityEdge{from: nodeScalarSet(set.Target), to: nodeTarget(set.Target)})
		for _, mapping := range set.Mappings {
			if !records.hasConcrete(mapping.HexalType) {
				return nil, fmt.Errorf("specdata/integrity: target %q spelling %q names unknown concrete type %q", set.Target, mapping.CSpelling, mapping.HexalType)
			}
			edges = append(edges, integrityEdge{from: nodeScalar(set.Target, mapping.CSpelling), to: nodeConcrete(mapping.HexalType)})
		}
	}

	for _, operator := range records.operators {
		if operator.ID == "" {
			continue
		}
		from := nodeOperator(operator.ID)
		for _, operand := range operator.Operands {
			if !records.hasConcrete(operand) {
				return nil, fmt.Errorf("specdata/integrity: operator %q names unknown operand type %q", operator.ID, operand)
			}
			edges = append(edges, integrityEdge{from: from, to: nodeConcrete(operand)})
		}
	}
	for _, rank := range records.ranks {
		if !records.hasConcrete(rank.ID) {
			return nil, fmt.Errorf("specdata/integrity: numeric rank names unknown concrete type %q", rank.ID)
		}
		edges = append(edges, integrityEdge{from: nodeRank(rank.ID), to: nodeConcrete(rank.ID)})
	}
	for _, pair := range records.widenings {
		from := nodeWidening(pair.From, pair.To)
		for _, id := range []TypeID{pair.From, pair.To} {
			if !records.hasConcrete(id) {
				return nil, fmt.Errorf("specdata/integrity: widening %q to %q names unknown concrete type %q", pair.From, pair.To, id)
			}
			edges = append(edges, integrityEdge{from: from, to: nodeConcrete(id)})
		}
	}
	for _, conversion := range records.conversions {
		from := nodeConversion(conversion.From, conversion.To)
		for _, id := range []TypeID{conversion.From, conversion.To} {
			if !records.hasConcrete(id) {
				return nil, fmt.Errorf("specdata/integrity: conversion %q to %q names unknown concrete type %q", conversion.From, conversion.To, id)
			}
			edges = append(edges, integrityEdge{from: from, to: nodeConcrete(id)})
		}
	}

	return edges, nil
}

// appendTypeRef resolves one method type reference and appends its edges. A
// concrete or applied identity crosses domains; a parameter, method type
// argument, and the abstract integer class name no cross-domain record. The
// method's owner context is not needed here, because only the named identities
// are checked; parameter range and argument arity stay with validateMethods.
func (records domainRecords) appendTypeRef(edges []integrityEdge, from integrityNode, where string, ref TypeRef) ([]integrityEdge, error) {
	switch ref.Kind {
	case RefConcrete:
		if ref.TypeID == "" {
			// The void result names no identity.
			return edges, nil
		}
		if !records.hasConcrete(ref.TypeID) {
			return nil, fmt.Errorf("specdata/integrity: %s references unknown concrete type %q", where, ref.TypeID)
		}
		return append(edges, integrityEdge{from: from, to: nodeConcrete(ref.TypeID)}), nil
	case RefApply:
		if !records.hasConstructor(ref.Constructor) {
			return nil, fmt.Errorf("specdata/integrity: %s references unknown constructor %q", where, ref.Constructor)
		}
		edges = append(edges, integrityEdge{from: from, to: nodeConstructor(ref.Constructor)})
	case RefUnion, RefPointer:
		// The structural forms carry no identity of their own.
	default:
		return edges, nil
	}
	for _, argument := range ref.Arguments {
		var err error
		edges, err = records.appendTypeRef(edges, from, where, argument)
		if err != nil {
			return nil, err
		}
	}
	return edges, nil
}

// findReferenceCycle reports one directed cycle in the reference graph, or nil
// when the graph is acyclic. Depth-first colouring is enough because the graph
// is tiny and a single cycle is all a maintainer needs to locate the edge that
// closed it.
func findReferenceCycle(edges []integrityEdge) []integrityNode {
	order := make([]integrityNode, 0, len(edges))
	adjacency := make(map[integrityNode][]integrityNode)
	seen := make(map[integrityNode]bool)
	for _, edge := range edges {
		if !seen[edge.from] {
			seen[edge.from] = true
			order = append(order, edge.from)
		}
		if !seen[edge.to] {
			seen[edge.to] = true
			order = append(order, edge.to)
		}
		adjacency[edge.from] = append(adjacency[edge.from], edge.to)
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[integrityNode]int, len(order))
	var stack []integrityNode
	var cycle []integrityNode
	var visit func(integrityNode) bool
	visit = func(node integrityNode) bool {
		color[node] = gray
		stack = append(stack, node)
		for _, next := range adjacency[node] {
			switch color[next] {
			case gray:
				for index, candidate := range stack {
					if candidate == next {
						cycle = append(cycle, stack[index:]...)
						cycle = append(cycle, next)
						return true
					}
				}
				return true
			case white:
				if visit(next) {
					return true
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[node] = black
		return false
	}
	for _, node := range order {
		if color[node] == white && visit(node) {
			return cycle
		}
	}
	return nil
}

// renderCycle renders a cycle path for a diagnostic.
func renderCycle(cycle []integrityNode) string {
	parts := make([]string, len(cycle))
	for index, node := range cycle {
		parts[index] = node.String()
	}
	return strings.Join(parts, " -> ")
}

// symbolOwnership is one runtime symbol's recorded owning facts: the sorted
// component set its emitted code needs and whether it can fail.
type symbolOwnership struct {
	owner      string
	components string
	fallible   bool
}

// validateSymbolOwnership rejects a runtime symbol shared by records that
// disagree about its owning facts. Sharing is legal only when one helper serves
// both records: the same component set and the same failure behavior. A
// disagreement means one record's code would be emitted where the other's
// contract expects something else.
func validateSymbolOwnership(records domainRecords) error {
	owners := make(map[string]symbolOwnership)
	record := func(symbol, owner string, components []ComponentID, fallible bool) error {
		if symbol == "" {
			return nil
		}
		key := componentsKey(components)
		prior, ok := owners[symbol]
		if !ok {
			owners[symbol] = symbolOwnership{owner: owner, components: key, fallible: fallible}
			return nil
		}
		if prior.components != key {
			return fmt.Errorf("specdata/integrity: runtime symbol %q is shared by %s and %s with different components", symbol, prior.owner, owner)
		}
		if prior.fallible != fallible {
			return fmt.Errorf("specdata/integrity: runtime symbol %q is shared by %s and %s with different failure modes", symbol, prior.owner, owner)
		}
		return nil
	}
	for _, method := range records.methods {
		owner := "method " + ownerName(method.Owner) + "." + method.Name
		if err := record(method.RuntimeSymbol, owner, []ComponentID{method.Component}, method.Failure == FailureChecked); err != nil {
			return err
		}
	}
	for _, module := range records.modules {
		for _, function := range module.Functions {
			owner := "function " + string(module.ID) + "." + function.Name
			if err := record(function.Runtime, owner, function.Components, function.ErrorBehavior == ErrorFallible); err != nil {
				return err
			}
		}
	}
	return nil
}

// componentsKey is the comparable form of a component demand set. Sorting makes
// two records naming the same set in different orders agree.
func componentsKey(ids []ComponentID) string {
	if len(ids) == 0 {
		return ""
	}
	sorted := make([]string, len(ids))
	for index, id := range ids {
		sorted[index] = string(id)
	}
	sort.Strings(sorted)
	return strings.Join(sorted, ",")
}

// validateABICompleteness rejects an ABI-visible record that cannot carry its
// ABI role. The record fields make "visible" and "required" decidable without a
// rule invented outside the registry:
//
//   - A target is ABI-visible because its C scalar mappings are target
//     qualified; its foreign-ABI record is exactly one non-empty mapping set,
//     and it carries the widths that record is read against.
//   - A type, method, or runtime function is ABI-visible when it demands a
//     runtime component; that component must own a generated file, or the C
//     representation or callable the demand names does not exist. Each is
//     checked at its own demand site so the message names the demander.
//
// The error-kind payload header needs no separate check: the reference pass
// proves it is a constructor, and the constructor's own demand above proves
// that constructor is ABI-visible.
//
// A scalar fact such as Representation or CopyMode cannot be "incomplete",
// because each is a non-optional enum whose zero value is itself a declared
// member; only a missing reference or an empty collection can be.
func validateABICompleteness(records domainRecords) error {
	for _, facts := range records.targets {
		if facts.ID == "" {
			continue
		}
		sets := 0
		for _, set := range records.scalars {
			if set.Target == facts.ID {
				sets++
			}
		}
		switch {
		case sets == 0:
			return fmt.Errorf("specdata/integrity: target %q has no foreign-ABI scalar mapping record", facts.ID)
		case sets > 1:
			return fmt.Errorf("specdata/integrity: target %q has %d foreign-ABI scalar mapping records, want one", facts.ID, sets)
		}
	}
	for _, set := range records.scalars {
		if len(set.Mappings) == 0 {
			return fmt.Errorf("specdata/integrity: target %q has an empty foreign-ABI scalar mapping record", set.Target)
		}
		if facts, ok := records.target(set.Target); ok && (facts.PointerWidth <= 0 || facts.SizeWidth <= 0) {
			return fmt.Errorf("specdata/integrity: target %q is ABI-visible but carries no pointer or size width", set.Target)
		}
	}
	for _, spec := range records.constructors {
		if spec.ID == "" {
			continue
		}
		component, ok := records.component(spec.Facts.Component)
		if !ok {
			continue
		}
		if len(component.Files) == 0 {
			return fmt.Errorf("specdata/integrity: type %q demands component %q, which owns no ABI file", spec.ID, spec.Facts.Component)
		}
	}
	for _, method := range records.methods {
		component, ok := records.component(method.Component)
		if !ok {
			continue
		}
		if len(component.Files) == 0 {
			return fmt.Errorf("specdata/integrity: method %s.%s demands component %q, which owns no ABI file", ownerName(method.Owner), method.Name, method.Component)
		}
	}
	for _, module := range records.modules {
		for _, function := range module.Functions {
			if function.Runtime == "" {
				continue
			}
			for _, id := range function.Components {
				component, ok := records.component(id)
				if !ok {
					continue
				}
				if len(component.Files) == 0 {
					return fmt.Errorf("specdata/integrity: function %q.%q demands component %q, which owns no ABI file", module.ID, function.Name, id)
				}
			}
		}
	}
	return nil
}
