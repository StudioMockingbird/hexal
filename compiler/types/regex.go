package types

import "hexal/compiler/specdata"

// std/regex's data model: the owning compiled-pattern handle, the half-open
// byte Span record, and the Match record owning its capture List. None has
// equality, ordering, printing, or Dict-key eligibility; Span is an ordinary
// comparable value struct.

var regexModelData = buildRegexModel()

type regexModeler struct {
	pattern       Type
	span          Type
	match         Type
	captureList   Type
	captureMember Type
}

// RegexPatternType is the owning compiled-pattern handle.
func RegexPatternType() Type { return regexModelData.pattern }

// RegexSpanType is the half-open byte-range value struct.
func RegexSpanType() Type { return regexModelData.span }

// RegexMatchType is the owning Match value struct.
func RegexMatchType() Type { return regexModelData.match }

// RegexCaptureListType is the capture List type the Match value owns.
func RegexCaptureListType() Type { return regexModelData.captureList }
func RegexCaptureType() Type     { return regexModelData.captureMember }

// IsRegexSpan, IsRegexMatch, IsRegexPattern, and IsRegexCaptureMember report
// whether a type is one of the std/regex records whose C definition lives in
// the core hexal/regex.h artifact rather than a module header, so a shared
// component spelling one must name that header.
func IsRegexSpan(typ Type) bool {
	return typ.identity != nil && typ.identity == regexModelData.span.identity
}

func IsRegexMatch(typ Type) bool {
	return typ.identity != nil && typ.identity == regexModelData.match.identity
}

func IsRegexPattern(typ Type) bool {
	return typ.identity != nil && typ.identity == regexModelData.pattern.identity
}

func IsRegexCaptureMember(typ Type) bool {
	return typ.identity != nil && typ.identity == regexModelData.captureMember.identity
}

func buildRegexModel() regexModeler {
	pattern := Type{
		Name:         "Pattern",
		CName:        "hex_regex_pattern",
		CanonicalKey: canonicalNominalKey("Pattern", ""),
		identity:     newTypeIdentity(),
	}
	span := builtinObject("Span", "hex_t_Span", []ObjectMember{
		{Name: "start", Type: SizeType, Use: NewTypeUse(SizeType)},
		{Name: "end", Type: SizeType, Use: NewTypeUse(SizeType)},
	})
	captureMember := builtinOptionalHandleUnion(span)
	captureList := Type{
		Name:         "List<" + captureMember.Name + ">",
		CName:        "hex_list_regex_capture",
		CanonicalKey: "list:" + captureMember.CanonicalKey,
		List:         &ListInfo{Element: captureMember},
		identity:     signatureIdentityCarrier("list:" + captureMember.CanonicalKey),
	}
	match := builtinObject("Match", "hex_t_Match", []ObjectMember{
		{Name: "whole", Type: span, Use: NewTypeUse(span)},
		{Name: "captures", Type: captureList, Use: NewTypeUse(captureList)},
	})
	_ = specdata.TypeRegexPattern
	return regexModeler{pattern: pattern, span: span, match: match, captureList: captureList, captureMember: captureMember}
}
