package integration

import (
	"regexp"
	"strings"
	"testing"

	"hexal/compiler"
	"hexal/compiler/specdata"
)

// Each registered method's RuntimeSymbol must be a symbol the generator
// actually emits. A record that names a symbol no program produces would let a
// consumer switch to the registry and silently lower to nothing, so every
// non-empty symbol is checked against the generated C of a program that
// invokes the method.
//
// The map is keyed "Owner.Name". A method with an empty RuntimeSymbol lowers to
// a field read or a constant and needs no program; the record's own comment
// says why. A method with a symbol but no entry here fails the test, so a new
// record cannot arrive unverified.

const arraySliceProgram = "fun demo() do\n" +
	"    let mut data: Array<Int32, 4> = [1, 2, 3, 4]\n" +
	"    let view: Slice<Int32> = data.slice(0, 2)\n" +
	"    let mut_view: Slice<mut Int32> = data.mut_slice(0, 2)\n" +
	"    mut_view[0] = 9\n" +
	"    let count: Size = data.length()\n" +
	"    let rebound: Slice<Int32> = view.slice(0, 1)\n" +
	"    let total: Int32 = rebound[0]\n" +
	"    unsafe do\n" +
	"        let address: Ptr<Int32> | Nil = view.pointer()\n" +
	"    end\n" +
	"    if count == 0 then\n" +
	"        let spare: Int32 = total\n" +
	"    end\n" +
	"end"

const listMethodsProgram = "fun demo(h: Heap) do\n" +
	"    let values: List<Int32> = List<Int32>(h)\n" +
	"    defer values.free(h)\n" +
	"    values.push(1)\n" +
	"    let count: Size = values.length()\n" +
	"    let view: Slice<Int32> = values.slice(0, 1)\n" +
	"    let mut_view: Slice<mut Int32> = values.mut_slice(0, 1)\n" +
	"    mut_view[0] = 2\n" +
	"    let first: Int32 = view[0]\n" +
	"    let last: Int32 = values.pop()\n" +
	"    values.clear()\n" +
	"    if count == 0 then\n" +
	"        let spare: Int32 = first + last\n" +
	"    end\n" +
	"end"

const dictMethodsProgram = "fun demo(h: Heap) do\n" +
	"    let scores: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n" +
	"    defer scores.free(h)\n" +
	"    scores.insert(1, 10)\n" +
	"    let present: Bool = scores.contains(1)\n" +
	"    let first: Int32 = scores.get(1)\n" +
	"    let found: Int32 | Nil = scores.find(1)\n" +
	"    let removed: Int32 = scores.remove(1)\n" +
	"    let count: Size = scores.length()\n" +
	"    if found == nil then\n" +
	"        let spare: Int32 = first + removed\n" +
	"    end\n" +
	"    if present then\n" +
	"        let spare2: Size = count\n" +
	"    end\n" +
	"end"

const channelTaskProgram = "fun produce(ch: Channel<Int32>): Bool do\n" +
	"    ch.send(1)\n" +
	"    ch.close()\n" +
	"    return true\n" +
	"end\n" +
	"fun run(): Int32 | Error do\n" +
	"    let h: Heap = Heap()\n" +
	"    let ch: Channel<Int32> = try Channel<Int32>(h, 4)\n" +
	"    defer ch.free(h)\n" +
	"    let producer: Task<Bool> = try spawn produce(ch)\n" +
	"    let done: Bool = producer.join()\n" +
	"    let step: Int32 | EoS = ch.receive()\n" +
	"    let length: Size = ch.length()\n" +
	"    let capacity: Size = ch.capacity()\n" +
	"    let closed: Bool = ch.is_closed()\n" +
	"    if done and closed then\n" +
	"        return 0\n" +
	"    end\n" +
	"    if step is EoS then\n" +
	"        if length <= capacity then\n" +
	"            return 0\n" +
	"        end\n" +
	"    end\n" +
	"    return 0\n" +
	"end"

const taskDetachProgram = "fun worker(): Bool do\n" +
	"    return true\n" +
	"end\n" +
	"fun run(): Int32 | Error do\n" +
	"    let t: Task<Bool> = try spawn worker()\n" +
	"    t.detach()\n" +
	"    return 0\n" +
	"end"

const atomicMethodsProgram = "fun run(): Bool do\n" +
	"    let counter: Atomic<Int32> = Atomic<Int32>(0)\n" +
	"    let old: Int32 = counter.fetch_add(1)\n" +
	"    counter.fetch_sub(1)\n" +
	"    counter.store(5)\n" +
	"    let loaded: Int32 = counter.load()\n" +
	"    let swapped: Int32 = counter.exchange(6)\n" +
	"    let expected: Bool = counter.compare_exchange(6, 7)\n" +
	"    counter.store(old + loaded + swapped)\n" +
	"    return expected\n" +
	"end"

const stashMethodsProgram = "type Node is struct value: Int32 end\n" +
	"fun demo() do\n" +
	"    let stash = Stash<Node>()\n" +
	"    defer stash.destroy()\n" +
	"    let node: Ptr<mut Node> = stash.allocate(Node(value = 1))\n" +
	"    stash.reset()\n" +
	"    let node2: Ptr<mut Node> = stash.allocate(Node(value = 2))\n" +
	"end"

const poolMethodsProgram = "type Node is struct value: Int32 end\n" +
	"fun demo() do\n" +
	"    let pool = Pool<Node>(4)\n" +
	"    defer pool.destroy()\n" +
	"    let node: Ptr<mut Node> = pool.allocate(Node(value = 1))\n" +
	"    pool.free(node)\n" +
	"end"

const mutexMethodsProgram = "fun run(): Int32 | Error do\n" +
	"    let h: Heap = Heap()\n" +
	"    let m: Mutex = try Mutex(h)\n" +
	"    defer m.free(h)\n" +
	"    m.lock()\n" +
	"    m.unlock()\n" +
	"    return 0\n" +
	"end"

const stringMethodsProgram = "fun demo(h: Heap) do\n" +
	"    let text: String = \"hello\".copy(h)\n" +
	"    defer text.free(h)\n" +
	"    let length: Size = text.length()\n" +
	"    let runes: Size = text.rune_length()\n" +
	"    let graphemes: Size = text.grapheme_length()\n" +
	"    let bytes_cursor: ByteCursor = text.byte_cursor()\n" +
	"    let runes_cursor: RuneCursor = text.rune_cursor()\n" +
	"    let graphemes_cursor: GraphemeCursor = text.grapheme_cursor()\n" +
	"    let raw: Slice<UInt8> = text.bytes()\n" +
	"    let part: Slice<UInt8> = text.slice(0, 1)\n" +
	"    let folded: String | Error = text.casefold(h)\n" +
	"    let normalized: String | Error = text.normalize(h, NormalizationForm.NFC())\n" +
	"    let joined: String | Error = text.concat(h, raw)\n" +
	"    unsafe do\n" +
	"        let pointer: Ptr<Byte> = text.c_pointer()\n" +
	"    end\n" +
	"    if length == 0 then\n" +
	"        let spare: UInt8 = part[0]\n" +
	"    end\n" +
	"    let spare2: ByteCursor = bytes_cursor\n" +
	"    let spare3: RuneCursor = runes_cursor\n" +
	"    let spare4: GraphemeCursor = graphemes_cursor\n" +
	"    let spare5: String | Error = folded\n" +
	"    let spare6: String | Error = normalized\n" +
	"    let spare7: String | Error = joined\n" +
	"    let spare8: Size = runes + graphemes\n" +
	"end"

const inlineStringMethodsProgram = "fun demo(h: Heap) do\n" +
	"    let label: String<16> = \"hexal\"\n" +
	"    let length: Size = label.length()\n" +
	"    let raw: Slice<UInt8> = label.bytes()\n" +
	"    let part: Slice<UInt8> = label.slice(0, 1)\n" +
	"    let joined: String | Error = label.concat(h, raw)\n" +
	"    let wide: String<32> = label.widen<32>()\n" +
	"    if length == 0 then\n" +
	"        let spare: UInt8 = part[0]\n" +
	"    end\n" +
	"    let spare2: String | Error = joined\n" +
	"    let spare3: String<32> = wide\n" +
	"end"

// methodSources binds one program to each registered method whose record names
// a runtime symbol.
var methodSources = map[string]string{
	"Array.slice":             arraySliceProgram,
	"Array.mut_slice":         arraySliceProgram,
	"Slice.slice":             arraySliceProgram,
	"Slice.pointer":           arraySliceProgram,
	"List.push":               listMethodsProgram,
	"List.pop":                listMethodsProgram,
	"List.clear":              listMethodsProgram,
	"List.free":               listMethodsProgram,
	"List.slice":              listMethodsProgram,
	"List.mut_slice":          listMethodsProgram,
	"Dict.insert":             dictMethodsProgram,
	"Dict.get":                dictMethodsProgram,
	"Dict.find":               dictMethodsProgram,
	"Dict.remove":             dictMethodsProgram,
	"Dict.contains":           dictMethodsProgram,
	"Dict.free":               dictMethodsProgram,
	"Task.join":               channelTaskProgram,
	"Task.detach":             taskDetachProgram,
	"Channel.send":            channelTaskProgram,
	"Channel.receive":         channelTaskProgram,
	"Channel.close":           channelTaskProgram,
	"Channel.free":            channelTaskProgram,
	"Channel.length":          channelTaskProgram,
	"Channel.capacity":        channelTaskProgram,
	"Channel.is_closed":       channelTaskProgram,
	"Atomic.load":             atomicMethodsProgram,
	"Atomic.store":            atomicMethodsProgram,
	"Atomic.exchange":         atomicMethodsProgram,
	"Atomic.fetch_add":        atomicMethodsProgram,
	"Atomic.fetch_sub":        atomicMethodsProgram,
	"Atomic.compare_exchange": atomicMethodsProgram,
	"Stash.allocate":          stashMethodsProgram,
	"Stash.reset":             stashMethodsProgram,
	"Stash.destroy":           stashMethodsProgram,
	"Pool.allocate":           poolMethodsProgram,
	"Pool.free":               poolMethodsProgram,
	"Pool.destroy":            poolMethodsProgram,
	"String.rune_length":      stringMethodsProgram,
	"String.grapheme_length":  stringMethodsProgram,
	"String.byte_cursor":      stringMethodsProgram,
	"String.rune_cursor":      stringMethodsProgram,
	"String.grapheme_cursor":  stringMethodsProgram,
	"String.bytes":            stringMethodsProgram,
	"String.slice":            stringMethodsProgram,
	"String.casefold":         stringMethodsProgram,
	"String.normalize":        stringMethodsProgram,
	"String.copy":             stringMethodsProgram,
	"String.concat":           stringMethodsProgram,
	"String.free":             stringMethodsProgram,
	"InlineString.bytes":      inlineStringMethodsProgram,
	"InlineString.slice":      inlineStringMethodsProgram,
	"InlineString.concat":     inlineStringMethodsProgram,
	"InlineString.widen":      inlineStringMethodsProgram,
	"Mutex.lock":              mutexMethodsProgram,
	"Mutex.unlock":            mutexMethodsProgram,
	"Mutex.free":              mutexMethodsProgram,
}

func TestBuiltinMethodRuntimeSymbolsAppearInGeneratedC(t *testing.T) {
	generated := make(map[string]string)
	compile := func(source string) string {
		t.Helper()
		if text, done := generated[source]; done {
			return text
		}
		result := compileSource(source)
		if result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("method symbol program failed to compile: %v\n--- source ---\n%s", result.Stderr, source)
		}
		var builder strings.Builder
		for _, key := range sortedKeys(result.Files) {
			builder.WriteString(result.Files[key])
			builder.WriteByte('\n')
		}
		text := builder.String()
		generated[source] = text
		return text
	}
	for _, method := range specdata.Methods() {
		key := symbolOwnerName(method.Owner) + "." + method.Name
		if method.RuntimeSymbol == "" {
			continue
		}
		source, ok := methodSources[key]
		if !ok {
			t.Errorf("method %s records runtime symbol %q but the test has no program invoking it", key, method.RuntimeSymbol)
			continue
		}
		if !runtimeSymbolPattern(method.RuntimeSymbol).MatchString(compile(source)) {
			t.Errorf("method %s runtime symbol %q does not appear in the generated C of its program", key, method.RuntimeSymbol)
		}
	}
}

// symbolOwnerName mirrors the registry's owner rendering for a test-side key.
func symbolOwnerName(owner specdata.TypePattern) string {
	if owner.Constructor != "" {
		return string(owner.Constructor)
	}
	return string(owner.Exact)
}

// runtimeSymbolPattern turns one recorded symbol into a matcher: the generator
// substitutes the per-specialization suffix for %s, so each placeholder spans
// one identifier-shaped run.
func runtimeSymbolPattern(symbol string) *regexp.Regexp {
	quoted := regexp.QuoteMeta(symbol)
	quoted = strings.ReplaceAll(quoted, "%s", `[A-Za-z0-9_]+`)
	return regexp.MustCompile(quoted)
}
