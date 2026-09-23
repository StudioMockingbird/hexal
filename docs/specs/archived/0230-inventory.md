# RFC 0230 Inventory Ledger

Companion to RFC 0230. One row per package-level `const`/`var` declaration in
the compiler and driver tree, with its classification and target owner.

- Generated: 2026-09-22
- Scope: `compiler/`, `internal/`, `lib/`, `cmd/`, `workbench/`, non-test Go
  files only
- Method: `go/ast` walk over every `GenDecl` of kind `const` or `var` at
  package scope. Reproducible; the extractor is not checked in because it is a
  one-shot audit tool, but the walk is three dozen lines and the row set below
  is its complete output.

## Result

**476 declarations across 64 files. Ten are tunable policy.**

| Classification | Count | Target owner | Moves? |
|---|---:|---|---|
| algorithm data | 282 | owning package | no |
| owning phase (message/diagnostic strings) | 102 | owning package | no |
| language fact | 60 | `compiler/types`, `compiler/specdata` | mostly no |
| driver fact | 12 | `internal/driver` | no |
| **tunable** | **10** | **`compiler/config`** | **yes** |
| mutable state | 6 | resolve individually | see below |
| generated-C fact | 3 | `compiler/generator` | no |
| ABI fact | 1 | `compiler/config` | yes |

### What this means for the arc

RFC 0228's premise is that tunable configuration is scattered across many
packages. The ledger confirms the scattering but not the volume: **2.1% of
package-level declarations are configuration.** The other 97.9% are internal
representation — `iota` enums for checked-tree node kinds, lexer token kinds,
builtin type identities, and diagnostic wording — and must *not* move, because
moving them would turn implementation mechanics into public policy, which
RFC 0228 itself forbids.

So `compiler/config` is a small, well-defined package of eleven values, not a
large migration. The requirement to classify all 476 is still worth having
done once, because it is what proves the other 465 stay put.

## The eleven that move

| Symbol | Current location | Classification |
|---|---|---|
| `RuntimeABIVersion` | `compiler/runtimeabi.go:9` | ABI fact |
| `configPageSize` | `compiler/project.go:32` | tunable |
| `maxSyntaxDepth` | `compiler/parser/parser.go:56` | tunable |
| `maxInterpolationDepth` | `compiler/lexer/lexer.go:443` | tunable |
| `DefaultTaskStackReserve` | `compiler/generator/concurrency_component.go:16` | tunable |
| `DefaultTaskStackCommit` | `compiler/generator/concurrency_component.go:17` | tunable |
| `MaxInlineStringCapacity` | `compiler/types/collections.go:116` | tunable |
| `ErrorHeaderCapacity` | `compiler/types/collections.go:157` | tunable |
| `ErrorMessageCapacity` | `compiler/types/collections.go:158` | tunable |
| `inspectionByteLimit` | `internal/driver/frontend.go:37` | tunable |
| `inspectionTimeout` | `internal/driver/frontend.go:38` | tunable |

Two notes on these. `MaxInlineStringCapacity` is RFC 0224's 4096 bound and is
the one entry whose *name* already follows RFC 0228's unit rule. The three
`compiler/types` entries move their **value** to config while their derived
type values (`ErrorHeaderText`, `ErrorMessageText`, the inline-string
constructor) stay in `types`, because those are type identity.

## The six mutable exported declarations

RFC 0228 forbids exported mutable maps, slices, and registries. Six exist.
Each needs a decision, not a move.

| Symbol | Location | Shape | Note |
|---|---|---|---|
| `corelib.Modules` | `compiler/corelib/corelib.go:58` | `map[string]Module` | the real one: a rewritable core-library registry. RFC 0229 slice 3 replaces it |
| `types.ErrorKindVariantNames` | `compiler/types/error_kind.go:10` | slice | ordered variant names; freeze as a fixed-size array or unexport behind an accessor |
| `backend.RequiredHeaders` | `internal/backend/facilities.go:12` | slice | driver fact; unexport or return a copy |
| `backend.RequiredFacilities` | `internal/backend/facilities.go:22` | slice | same |
| `snippets.RequiredReservedWords` | `workbench/snippets/catalog.go:54` | slice | workbench tooling, outside the compiler; lowest priority |
| `snippets.RequiredFeatures` | `workbench/snippets/catalog.go:63` | slice | same |

### Name commitments

**None.** `go.mod` declares `module hexal`, which is not a fetchable path, and
the only importers of `hexal/compiler` are `internal/driver` and in-module
tests. Every exported name in this ledger is movable, so no row needs a
pinned/movable column and no slice needs a name-compatibility audit. See
RFC 0230 Decision 11.

**A `var` is not automatically mutable state.** The other 48 exported `var`s
in `compiler/types` hold builtin `Type` struct values and are `var` only
because Go cannot declare a struct `const`. They are type identity and stay
where they are. Only the six above hold a map or slice a caller can rewrite.

## Classification rules

Applied mechanically, in this order:

1. `RuntimeABIVersion` -> ABI fact, `compiler/config`.
2. One of the six mutable exports -> mutable state, resolve individually.
3. On the tunable allowlist above -> tunable, `compiler/config`.
4. Declared in an `iota` block, or valueless in one -> algorithm data, stays.
5. A string literal -> owning phase, stays with the phase that emits it.
6. Otherwise by package: `types` -> language fact; `corelib` -> language fact
   moving to `specdata`; `driver`/`backend`/`version` -> driver fact;
   `generator` -> generated-C fact; `workbench`/`snippets`/`lib` -> tooling.

Rule 4 is what keeps 282 declarations in place: a checked-tree node kind or a
token kind is the shape of an internal representation, not a policy anyone
tunes. Rule 5 keeps diagnostic wording with its owning phase, per RFC 0230
Decision 7.

## Full row set

| Symbol | Location | Shape | Classification | Target owner |
|---|---|---|---|---|
| `MatchScalarTag` (exported) | `compiler/checker/adt.go:923` | other | algorithm data | compiler/checker |
| `entrypointLogicalKey` | `compiler/checker/checker.go:375` | string | owning phase | compiler/checker |
| `canonicalEntrypoint` | `compiler/checker/checker.go:376` | string | owning phase | compiler/checker |
| `removedTextTypeHints` | `compiler/checker/inline_strings.go:67` | map | algorithm data | compiler/checker |
| `layoutBuiltins` | `compiler/checker/layout.go:14` | map | algorithm data | compiler/checker |
| `InvalidOperand` (exported) | `compiler/checker/operands.go:24` | enum | algorithm data | compiler/checker |
| `ConstantOperand` (exported) | `compiler/checker/operands.go:25` | enum | algorithm data | compiler/checker |
| `VariableOperand` (exported) | `compiler/checker/operands.go:26` | enum | algorithm data | compiler/checker |
| `ObjectOperand` (exported) | `compiler/checker/operands.go:27` | enum | algorithm data | compiler/checker |
| `ExpressionOperand` (exported) | `compiler/checker/operands.go:28` | enum | algorithm data | compiler/checker |
| `InvalidExpression` (exported) | `compiler/checker/operands.go:43` | enum | algorithm data | compiler/checker |
| `VariableExpression` (exported) | `compiler/checker/operands.go:44` | enum | algorithm data | compiler/checker |
| `AddressOfExpression` (exported) | `compiler/checker/operands.go:45` | enum | algorithm data | compiler/checker |
| `DereferenceExpression` (exported) | `compiler/checker/operands.go:46` | enum | algorithm data | compiler/checker |
| `MemberExpression` (exported) | `compiler/checker/operands.go:47` | enum | algorithm data | compiler/checker |
| `ObjectExpression` (exported) | `compiler/checker/operands.go:48` | enum | algorithm data | compiler/checker |
| `ConstantExpression` (exported) | `compiler/checker/operands.go:49` | enum | algorithm data | compiler/checker |
| `UnaryOperationExpression` (exported) | `compiler/checker/operands.go:50` | enum | algorithm data | compiler/checker |
| `BinaryOperationExpression` (exported) | `compiler/checker/operands.go:51` | enum | algorithm data | compiler/checker |
| `FunctionReferenceExpression` (exported) | `compiler/checker/operands.go:54` | enum | algorithm data | compiler/checker |
| `ForeignFunctionReferenceExpression` (exported) | `compiler/checker/operands.go:58` | enum | algorithm data | compiler/checker |
| `ForeignConstantExpression` (exported) | `compiler/checker/operands.go:62` | enum | algorithm data | compiler/checker |
| `ForeignGlobalExpression` (exported) | `compiler/checker/operands.go:66` | enum | algorithm data | compiler/checker |
| `CallExpression` (exported) | `compiler/checker/operands.go:69` | enum | algorithm data | compiler/checker |
| `MethodCallExpression` (exported) | `compiler/checker/operands.go:74` | enum | algorithm data | compiler/checker |
| `NilExpression` (exported) | `compiler/checker/operands.go:77` | enum | algorithm data | compiler/checker |
| `EosExpression` (exported) | `compiler/checker/operands.go:80` | enum | algorithm data | compiler/checker |
| `NullTestExpression` (exported) | `compiler/checker/operands.go:85` | enum | algorithm data | compiler/checker |
| `UnionInjectionExpression` (exported) | `compiler/checker/operands.go:88` | enum | algorithm data | compiler/checker |
| `UnionWidenExpression` (exported) | `compiler/checker/operands.go:90` | enum | algorithm data | compiler/checker |
| `UnionTestExpression` (exported) | `compiler/checker/operands.go:92` | enum | algorithm data | compiler/checker |
| `UnionPayloadExpression` (exported) | `compiler/checker/operands.go:94` | enum | algorithm data | compiler/checker |
| `UnionEqualityExpression` (exported) | `compiler/checker/operands.go:96` | enum | algorithm data | compiler/checker |
| `HeapAllocateExpression` (exported) | `compiler/checker/operands.go:98` | enum | algorithm data | compiler/checker |
| `HeapAllocateAlignedExpression` (exported) | `compiler/checker/operands.go:104` | enum | algorithm data | compiler/checker |
| `HeapFreeExpression` (exported) | `compiler/checker/operands.go:106` | enum | algorithm data | compiler/checker |
| `AdtConstructExpression` (exported) | `compiler/checker/operands.go:108` | enum | algorithm data | compiler/checker |
| `AdtPayloadExpression` (exported) | `compiler/checker/operands.go:110` | enum | algorithm data | compiler/checker |
| `MatchExpression` (exported) | `compiler/checker/operands.go:112` | enum | algorithm data | compiler/checker |
| `ArrayLiteralExpression` (exported) | `compiler/checker/operands.go:115` | enum | algorithm data | compiler/checker |
| `IndexExpression` (exported) | `compiler/checker/operands.go:119` | enum | algorithm data | compiler/checker |
| `CollectionMethodCallExpression` (exported) | `compiler/checker/operands.go:123` | enum | algorithm data | compiler/checker |
| `CollectionSliceExpression` (exported) | `compiler/checker/operands.go:127` | enum | algorithm data | compiler/checker |
| `StringLiteralExpression` (exported) | `compiler/checker/operands.go:131` | enum | algorithm data | compiler/checker |
| `StringMethodCallExpression` (exported) | `compiler/checker/operands.go:137` | enum | algorithm data | compiler/checker |
| `StringFromBytesExpression` (exported) | `compiler/checker/operands.go:141` | enum | algorithm data | compiler/checker |
| `StringInterpolateExpression` (exported) | `compiler/checker/operands.go:146` | enum | algorithm data | compiler/checker |
| `InlineStringConstructExpression` (exported) | `compiler/checker/operands.go:151` | enum | algorithm data | compiler/checker |
| `TextCoerceExpression` (exported) | `compiler/checker/operands.go:158` | enum | algorithm data | compiler/checker |
| `ListNewExpression` (exported) | `compiler/checker/operands.go:161` | enum | algorithm data | compiler/checker |
| `DictNewExpression` (exported) | `compiler/checker/operands.go:164` | enum | algorithm data | compiler/checker |
| `BitCastExpression` (exported) | `compiler/checker/operands.go:167` | enum | algorithm data | compiler/checker |
| `EndianConversionExpression` (exported) | `compiler/checker/operands.go:173` | enum | algorithm data | compiler/checker |
| `TryExpression` (exported) | `compiler/checker/operands.go:180` | enum | algorithm data | compiler/checker |
| `PrintExpression` (exported) | `compiler/checker/operands.go:183` | enum | algorithm data | compiler/checker |
| `DeepEqualityExpression` (exported) | `compiler/checker/operands.go:189` | enum | algorithm data | compiler/checker |
| `StringCompareExpression` (exported) | `compiler/checker/operands.go:193` | enum | algorithm data | compiler/checker |
| `WideningExpression` (exported) | `compiler/checker/operands.go:197` | enum | algorithm data | compiler/checker |
| `ConversionExpression` (exported) | `compiler/checker/operands.go:201` | enum | algorithm data | compiler/checker |
| `SpawnExpression` (exported) | `compiler/checker/operands.go:205` | enum | algorithm data | compiler/checker |
| `TaskYieldExpression` (exported) | `compiler/checker/operands.go:208` | enum | algorithm data | compiler/checker |
| `TaskMethodCallExpression` (exported) | `compiler/checker/operands.go:212` | enum | algorithm data | compiler/checker |
| `ChannelConstructorExpression` (exported) | `compiler/checker/operands.go:216` | enum | algorithm data | compiler/checker |
| `ChannelMethodCallExpression` (exported) | `compiler/checker/operands.go:220` | enum | algorithm data | compiler/checker |
| `MutexConstructorExpression` (exported) | `compiler/checker/operands.go:223` | enum | algorithm data | compiler/checker |
| `MutexMethodCallExpression` (exported) | `compiler/checker/operands.go:226` | enum | algorithm data | compiler/checker |
| `AtomicConstructorExpression` (exported) | `compiler/checker/operands.go:230` | enum | algorithm data | compiler/checker |
| `AtomicMethodCallExpression` (exported) | `compiler/checker/operands.go:234` | enum | algorithm data | compiler/checker |
| `StashConstructorExpression` (exported) | `compiler/checker/operands.go:238` | enum | algorithm data | compiler/checker |
| `StashMethodCallExpression` (exported) | `compiler/checker/operands.go:244` | enum | algorithm data | compiler/checker |
| `PoolConstructorExpression` (exported) | `compiler/checker/operands.go:248` | enum | algorithm data | compiler/checker |
| `PoolMethodCallExpression` (exported) | `compiler/checker/operands.go:254` | enum | algorithm data | compiler/checker |
| `LayoutExpression` (exported) | `compiler/checker/operands.go:258` | enum | algorithm data | compiler/checker |
| `VolatileReadExpression` (exported) | `compiler/checker/operands.go:262` | enum | algorithm data | compiler/checker |
| `VolatileWriteExpression` (exported) | `compiler/checker/operands.go:267` | enum | algorithm data | compiler/checker |
| `PointerOffsetExpression` (exported) | `compiler/checker/operands.go:272` | enum | algorithm data | compiler/checker |
| `PointerIndexExpression` (exported) | `compiler/checker/operands.go:277` | enum | algorithm data | compiler/checker |
| `PointerCastExpression` (exported) | `compiler/checker/operands.go:282` | enum | algorithm data | compiler/checker |
| `SliceBridgeExpression` (exported) | `compiler/checker/operands.go:287` | enum | algorithm data | compiler/checker |
| `StreamConstructorExpression` (exported) | `compiler/checker/operands.go:292` | enum | algorithm data | compiler/checker |
| `BytesOverExpression` (exported) | `compiler/checker/operands.go:296` | enum | algorithm data | compiler/checker |
| `StreamMethodCallExpression` (exported) | `compiler/checker/operands.go:302` | enum | algorithm data | compiler/checker |
| `TimeExpression` (exported) | `compiler/checker/operands.go:310` | enum | algorithm data | compiler/checker |
| `FunctionLiteralExpression` (exported) | `compiler/checker/operands.go:318` | enum | algorithm data | compiler/checker |
| `ErrorHeaderExpression` (exported) | `compiler/checker/operands.go:321` | enum | algorithm data | compiler/checker |
| `ErrorKindHeaderExpression` (exported) | `compiler/checker/operands.go:325` | enum | algorithm data | compiler/checker |
| `ModuleValueExpression` (exported) | `compiler/checker/operands.go:333` | enum | algorithm data | compiler/checker |
| `NetworkExpression` (exported) | `compiler/checker/operands.go:347` | enum | algorithm data | compiler/checker |
| `CorelibCallExpression` (exported) | `compiler/checker/operands.go:355` | enum | algorithm data | compiler/checker |
| `InvalidOperator` (exported) | `compiler/checker/operands.go:366` | enum | algorithm data | compiler/checker |
| `NegateOperator` (exported) | `compiler/checker/operands.go:367` | enum | algorithm data | compiler/checker |
| `LogicalNotOperator` (exported) | `compiler/checker/operands.go:368` | enum | algorithm data | compiler/checker |
| `BitwiseNotOperator` (exported) | `compiler/checker/operands.go:369` | enum | algorithm data | compiler/checker |
| `AddOperator` (exported) | `compiler/checker/operands.go:370` | enum | algorithm data | compiler/checker |
| `SubtractOperator` (exported) | `compiler/checker/operands.go:371` | enum | algorithm data | compiler/checker |
| `MultiplyOperator` (exported) | `compiler/checker/operands.go:372` | enum | algorithm data | compiler/checker |
| `DivideOperator` (exported) | `compiler/checker/operands.go:373` | enum | algorithm data | compiler/checker |
| `RemainderOperator` (exported) | `compiler/checker/operands.go:374` | enum | algorithm data | compiler/checker |
| `BitwiseAndOperator` (exported) | `compiler/checker/operands.go:375` | enum | algorithm data | compiler/checker |
| `BitwiseXorOperator` (exported) | `compiler/checker/operands.go:376` | enum | algorithm data | compiler/checker |
| `BitwiseOrOperator` (exported) | `compiler/checker/operands.go:377` | enum | algorithm data | compiler/checker |
| `ShiftLeftOperator` (exported) | `compiler/checker/operands.go:378` | enum | algorithm data | compiler/checker |
| `ShiftRightOperator` (exported) | `compiler/checker/operands.go:379` | enum | algorithm data | compiler/checker |
| `EqualOperator` (exported) | `compiler/checker/operands.go:380` | enum | algorithm data | compiler/checker |
| `NotEqualOperator` (exported) | `compiler/checker/operands.go:381` | enum | algorithm data | compiler/checker |
| `LessOperator` (exported) | `compiler/checker/operands.go:382` | enum | algorithm data | compiler/checker |
| `LessEqualOperator` (exported) | `compiler/checker/operands.go:383` | enum | algorithm data | compiler/checker |
| `GreaterOperator` (exported) | `compiler/checker/operands.go:384` | enum | algorithm data | compiler/checker |
| `GreaterEqualOperator` (exported) | `compiler/checker/operands.go:385` | enum | algorithm data | compiler/checker |
| `LogicalAndOperator` (exported) | `compiler/checker/operands.go:386` | enum | algorithm data | compiler/checker |
| `LogicalOrOperator` (exported) | `compiler/checker/operands.go:387` | enum | algorithm data | compiler/checker |
| `ViewRootNone` (exported) | `compiler/checker/operands.go:595` | enum | algorithm data | compiler/checker |
| `ViewRootForeign` (exported) | `compiler/checker/operands.go:596` | enum | algorithm data | compiler/checker |
| `ViewRootBindings` (exported) | `compiler/checker/operands.go:597` | enum | algorithm data | compiler/checker |
| `DecimalRadix` (exported) | `compiler/checker/operands.go:623` | enum | algorithm data | compiler/checker |
| `HexadecimalRadix` (exported) | `compiler/checker/operands.go:624` | enum | algorithm data | compiler/checker |
| `BinaryRadix` (exported) | `compiler/checker/operands.go:625` | enum | algorithm data | compiler/checker |
| `OctalRadix` (exported) | `compiler/checker/operands.go:626` | enum | algorithm data | compiler/checker |
| `dataBinding` | `compiler/checker/scope.go:18` | enum | algorithm data | compiler/checker |
| `functionBinding` | `compiler/checker/scope.go:19` | enum | algorithm data | compiler/checker |
| `genericFunctionBinding` | `compiler/checker/scope.go:20` | enum | algorithm data | compiler/checker |
| `aliasBinding` | `compiler/checker/scope.go:21` | enum | algorithm data | compiler/checker |
| `moduleValueBinding` | `compiler/checker/scope.go:26` | enum | algorithm data | compiler/checker |
| `foreignFunctionBinding` | `compiler/checker/scope.go:31` | enum | algorithm data | compiler/checker |
| `foreignConstantBinding` | `compiler/checker/scope.go:32` | enum | algorithm data | compiler/checker |
| `foreignGlobalBinding` | `compiler/checker/scope.go:33` | enum | algorithm data | compiler/checker |
| `nameFound` | `compiler/checker/scope.go:941` | enum | algorithm data | compiler/checker |
| `nameMissing` | `compiler/checker/scope.go:942` | enum | algorithm data | compiler/checker |
| `nameModuleData` | `compiler/checker/scope.go:943` | enum | algorithm data | compiler/checker |
| `stringOriginStatic` | `compiler/checker/strings.go:24` | number | algorithm data | compiler/checker |
| `stringOriginOwned` | `compiler/checker/strings.go:25` | number | algorithm data | compiler/checker |
| `stringOriginOpaque` | `compiler/checker/strings.go:26` | number | algorithm data | compiler/checker |
| `stringTypeCallUsage` | `compiler/checker/text.go:119` | string | owning phase | compiler/checker |
| `durationUnits` | `compiler/checker/time.go:18` | map | algorithm data | compiler/checker |
| `unsafeSliceFromPointer` | `compiler/checker/unsafe.go:38` | string | owning phase | compiler/checker |
| `unsafePointerOffset` | `compiler/checker/unsafe.go:39` | string | owning phase | compiler/checker |
| `unsafePointerCast` | `compiler/checker/unsafe.go:40` | string | owning phase | compiler/checker |
| `unsafePointerIndex` | `compiler/checker/unsafe.go:41` | string | owning phase | compiler/checker |
| `unsafeSlicePointer` | `compiler/checker/unsafe.go:42` | string | owning phase | compiler/checker |
| `unsafeStringCPointer` | `compiler/checker/unsafe.go:43` | string | owning phase | compiler/checker |
| `ExitSuccess` (exported) | `compiler/compile.go:25` | number | algorithm data | compiler/compiler |
| `ExitFailure` (exported) | `compiler/compile.go:27` | number | algorithm data | compiler/compiler |
| `panicSeam` | `compiler/compile.go:75` | other | algorithm data | compiler/compiler |
| `ParamHeap` (exported) | `compiler/corelib/corelib.go:17` | enum | algorithm data | compiler/corelib |
| `ParamMutByteSlice` (exported) | `compiler/corelib/corelib.go:18` | enum | algorithm data | compiler/corelib |
| `ResultString` (exported) | `compiler/corelib/corelib.go:26` | enum | algorithm data | compiler/corelib |
| `ResultStringSlice` (exported) | `compiler/corelib/corelib.go:27` | enum | algorithm data | compiler/corelib |
| `ResultNil` (exported) | `compiler/corelib/corelib.go:28` | enum | algorithm data | compiler/corelib |
| `ResultSize` (exported) | `compiler/corelib/corelib.go:29` | enum | algorithm data | compiler/corelib |
| `Modules` (exported) | `compiler/corelib/corelib.go:58` | map | mutable state | RESOLVE (unexport or freeze) |
| `movedTypes` | `compiler/corelib/hints.go:14` | map | language fact | compiler/specdata |
| `removedNamespaces` | `compiler/corelib/hints.go:47` | map | language fact | compiler/specdata |
| `movedOperations` | `compiler/corelib/hints.go:60` | map | language fact | compiler/specdata |
| `movedConstructors` | `compiler/corelib/hints.go:82` | map | language fact | compiler/specdata |
| `runtimeTemplates` | `compiler/corelib/runtime.go:10` | enum | algorithm data | compiler/corelib |
| `packageTemplates` | `compiler/generator/components.go:21` | enum | algorithm data | compiler/generator |
| `componentTemplates` | `compiler/generator/components.go:26` | value | generated-C fact | compiler/generator |
| `taskCreationFailed` | `compiler/generator/concurrency.go:109` | string | owning phase | compiler/generator |
| `channelCreationFailed` | `compiler/generator/concurrency.go:110` | string | owning phase | compiler/generator |
| `channelSendFailed` | `compiler/generator/concurrency.go:111` | string | owning phase | compiler/generator |
| `mutexCreationFailed` | `compiler/generator/concurrency.go:112` | string | owning phase | compiler/generator |
| `DefaultTaskStackReserve` (exported) | `compiler/generator/concurrency_component.go:16` | number | tunable | compiler/config |
| `DefaultTaskStackCommit` (exported) | `compiler/generator/concurrency_component.go:17` | number | tunable | compiler/config |
| `conversionIdentity` | `compiler/generator/conversions.go:29` | enum | algorithm data | compiler/generator |
| `conversionDirect` | `compiler/generator/conversions.go:32` | enum | algorithm data | compiler/generator |
| `conversionChecked` | `compiler/generator/conversions.go:34` | enum | algorithm data | compiler/generator |
| `corelibRuntimeArguments` | `compiler/generator/corelib.go:22` | string | owning phase | compiler/generator |
| `corelibRuntimeCurrentDirectory` | `compiler/generator/corelib.go:23` | string | owning phase | compiler/generator |
| `corelibRuntimeHomeDirectory` | `compiler/generator/corelib.go:24` | string | owning phase | compiler/generator |
| `corelibRuntimeTemporaryDirectory` | `compiler/generator/corelib.go:25` | string | owning phase | compiler/generator |
| `corelibRuntimeExecutablePath` | `compiler/generator/corelib.go:26` | string | owning phase | compiler/generator |
| `corelibRuntimeAvailableParallelism` | `compiler/generator/corelib.go:27` | string | owning phase | compiler/generator |
| `corelibRuntimeEntropyFill` | `compiler/generator/corelib.go:28` | string | owning phase | compiler/generator |
| `valueName` | `compiler/generator/declarations.go:20` | enum | algorithm data | compiler/generator |
| `typeName` | `compiler/generator/declarations.go:21` | enum | algorithm data | compiler/generator |
| `memberName` | `compiler/generator/declarations.go:22` | enum | algorithm data | compiler/generator |
| `functionNameKind` | `compiler/generator/declarations.go:23` | enum | algorithm data | compiler/generator |
| `functionName` | `compiler/generator/declarations.go:24` | enum | algorithm data | compiler/generator |
| `hexalHeaderPrefix` | `compiler/generator/emission.go:15` | string | owning phase | compiler/generator |
| `entryEnvironmentName` | `compiler/generator/environment.go:13` | string | owning phase | compiler/generator |
| `fileMessageOpen` | `compiler/generator/file.go:16` | string | owning phase | compiler/generator |
| `fileMessageRead` | `compiler/generator/file.go:17` | string | owning phase | compiler/generator |
| `fileMessageWrite` | `compiler/generator/file.go:18` | string | owning phase | compiler/generator |
| `fileMessageSeek` | `compiler/generator/file.go:19` | string | owning phase | compiler/generator |
| `fileMessageFlush` | `compiler/generator/file.go:20` | string | owning phase | compiler/generator |
| `fileMessageClose` | `compiler/generator/file.go:21` | string | owning phase | compiler/generator |
| `fileMessageNotReadable` | `compiler/generator/file.go:22` | string | owning phase | compiler/generator |
| `fileMessageNotWritable` | `compiler/generator/file.go:23` | string | owning phase | compiler/generator |
| `streamMessageOpen` | `compiler/generator/io.go:20` | string | owning phase | compiler/generator |
| `streamMessageReadFailed` | `compiler/generator/io.go:21` | string | owning phase | compiler/generator |
| `streamMessageNotReadable` | `compiler/generator/io.go:22` | string | owning phase | compiler/generator |
| `streamMessageSelfRead` | `compiler/generator/io.go:23` | string | owning phase | compiler/generator |
| `streamMessageWriteFailed` | `compiler/generator/io.go:24` | string | owning phase | compiler/generator |
| `streamMessageNotWritable` | `compiler/generator/io.go:25` | string | owning phase | compiler/generator |
| `streamMessageOverlap` | `compiler/generator/io.go:26` | string | owning phase | compiler/generator |
| `streamMessageSeekFailed` | `compiler/generator/io.go:27` | string | owning phase | compiler/generator |
| `streamMessageCloseFailed` | `compiler/generator/io.go:28` | string | owning phase | compiler/generator |
| `networkMessageAddressParse` | `compiler/generator/network.go:16` | string | owning phase | compiler/generator |
| `networkMessageDnsResolve` | `compiler/generator/network.go:17` | string | owning phase | compiler/generator |
| `networkMessageTcpConnect` | `compiler/generator/network.go:18` | string | owning phase | compiler/generator |
| `networkMessageTcpListen` | `compiler/generator/network.go:19` | string | owning phase | compiler/generator |
| `networkMessageTcpAccept` | `compiler/generator/network.go:20` | string | owning phase | compiler/generator |
| `networkMessageTcpRead` | `compiler/generator/network.go:21` | string | owning phase | compiler/generator |
| `networkMessageTcpWrite` | `compiler/generator/network.go:22` | string | owning phase | compiler/generator |
| `networkMessageTcpShutdown` | `compiler/generator/network.go:23` | string | owning phase | compiler/generator |
| `networkMessageTcpNoDelay` | `compiler/generator/network.go:24` | string | owning phase | compiler/generator |
| `networkMessageTcpClose` | `compiler/generator/network.go:25` | string | owning phase | compiler/generator |
| `processMessageStart` | `compiler/generator/process.go:19` | string | owning phase | compiler/generator |
| `processMessageWait` | `compiler/generator/process.go:20` | string | owning phase | compiler/generator |
| `processMessageTerminate` | `compiler/generator/process.go:21` | string | owning phase | compiler/generator |
| `processMessageClose` | `compiler/generator/process.go:22` | string | owning phase | compiler/generator |
| `pipeMessageRead` | `compiler/generator/process.go:23` | string | owning phase | compiler/generator |
| `pipeMessageWrite` | `compiler/generator/process.go:24` | string | owning phase | compiler/generator |
| `pipeMessageShutdown` | `compiler/generator/process.go:25` | string | owning phase | compiler/generator |
| `pipeMessageClose` | `compiler/generator/process.go:26` | string | owning phase | compiler/generator |
| `ringKeepEveryGrouping` | `compiler/generator/render.go:1662` | other | generated-C fact | compiler/generator |
| `signalMessageNew` | `compiler/generator/signal.go:19` | string | owning phase | compiler/generator |
| `signalMessageNext` | `compiler/generator/signal.go:20` | string | owning phase | compiler/generator |
| `signalMessageClose` | `compiler/generator/signal.go:21` | string | owning phase | compiler/generator |
| `boundedTextTraps` | `compiler/generator/strings.go:206` | map | generated-C fact | compiler/generator |
| `textMessageInvalidUTF8` | `compiler/generator/strings.go:615` | string | owning phase | compiler/generator |
| `textMessageOverCapacity` | `compiler/generator/strings.go:616` | string | owning phase | compiler/generator |
| `terminalMessageDetection` | `compiler/generator/terminal.go:21` | string | owning phase | compiler/generator |
| `terminalMessageSizeQuery` | `compiler/generator/terminal.go:22` | string | owning phase | compiler/generator |
| `terminalMessageNotATerminal` | `compiler/generator/terminal.go:23` | string | owning phase | compiler/generator |
| `terminalMessageInvalidSize` | `compiler/generator/terminal.go:24` | string | owning phase | compiler/generator |
| `timeMessageUnavailable` | `compiler/generator/time.go:15` | string | owning phase | compiler/generator |
| `traversalWalks` | `compiler/generator/traversal_metrics_bench.go:14` | enum | algorithm data | compiler/generator |
| `traversalNodes` | `compiler/generator/traversal_metrics_bench.go:15` | enum | algorithm data | compiler/generator |
| `ByteEscapes` (exported) | `compiler/lexer/lexer.go:21` | enum | algorithm data | compiler/lexer |
| `RuneEscapes` (exported) | `compiler/lexer/lexer.go:22` | enum | algorithm data | compiler/lexer |
| `StringEscapes` (exported) | `compiler/lexer/lexer.go:23` | enum | algorithm data | compiler/lexer |
| `Identifier` (exported) | `compiler/lexer/lexer.go:125` | enum | algorithm data | compiler/lexer |
| `Colon` (exported) | `compiler/lexer/lexer.go:126` | enum | algorithm data | compiler/lexer |
| `Equal` (exported) | `compiler/lexer/lexer.go:127` | enum | algorithm data | compiler/lexer |
| `Less` (exported) | `compiler/lexer/lexer.go:128` | enum | algorithm data | compiler/lexer |
| `Greater` (exported) | `compiler/lexer/lexer.go:129` | enum | algorithm data | compiler/lexer |
| `Minus` (exported) | `compiler/lexer/lexer.go:130` | enum | algorithm data | compiler/lexer |
| `Integer` (exported) | `compiler/lexer/lexer.go:131` | enum | algorithm data | compiler/lexer |
| `HexInteger` (exported) | `compiler/lexer/lexer.go:132` | enum | algorithm data | compiler/lexer |
| `BinaryInteger` (exported) | `compiler/lexer/lexer.go:133` | enum | algorithm data | compiler/lexer |
| `OctalInteger` (exported) | `compiler/lexer/lexer.go:134` | enum | algorithm data | compiler/lexer |
| `DecimalFloat` (exported) | `compiler/lexer/lexer.go:135` | enum | algorithm data | compiler/lexer |
| `True` (exported) | `compiler/lexer/lexer.go:136` | enum | algorithm data | compiler/lexer |
| `False` (exported) | `compiler/lexer/lexer.go:137` | enum | algorithm data | compiler/lexer |
| `NilLiteral` (exported) | `compiler/lexer/lexer.go:138` | enum | algorithm data | compiler/lexer |
| `Eos` (exported) | `compiler/lexer/lexer.go:139` | enum | algorithm data | compiler/lexer |
| `Mut` (exported) | `compiler/lexer/lexer.go:140` | enum | algorithm data | compiler/lexer |
| `Type` (exported) | `compiler/lexer/lexer.go:141` | enum | algorithm data | compiler/lexer |
| `Dot` (exported) | `compiler/lexer/lexer.go:142` | enum | algorithm data | compiler/lexer |
| `LeftBrace` (exported) | `compiler/lexer/lexer.go:143` | enum | algorithm data | compiler/lexer |
| `RightBrace` (exported) | `compiler/lexer/lexer.go:144` | enum | algorithm data | compiler/lexer |
| `Comma` (exported) | `compiler/lexer/lexer.go:145` | enum | algorithm data | compiler/lexer |
| `Bang` (exported) | `compiler/lexer/lexer.go:146` | enum | algorithm data | compiler/lexer |
| `BangEqual` (exported) | `compiler/lexer/lexer.go:147` | enum | algorithm data | compiler/lexer |
| `EqualEqual` (exported) | `compiler/lexer/lexer.go:148` | enum | algorithm data | compiler/lexer |
| `LessEqual` (exported) | `compiler/lexer/lexer.go:149` | enum | algorithm data | compiler/lexer |
| `GreaterEqual` (exported) | `compiler/lexer/lexer.go:150` | enum | algorithm data | compiler/lexer |
| `Plus` (exported) | `compiler/lexer/lexer.go:151` | enum | algorithm data | compiler/lexer |
| `Star` (exported) | `compiler/lexer/lexer.go:152` | enum | algorithm data | compiler/lexer |
| `Slash` (exported) | `compiler/lexer/lexer.go:153` | enum | algorithm data | compiler/lexer |
| `Percent` (exported) | `compiler/lexer/lexer.go:154` | enum | algorithm data | compiler/lexer |
| `LeftParen` (exported) | `compiler/lexer/lexer.go:155` | enum | algorithm data | compiler/lexer |
| `RightParen` (exported) | `compiler/lexer/lexer.go:156` | enum | algorithm data | compiler/lexer |
| `LeftBracket` (exported) | `compiler/lexer/lexer.go:157` | enum | algorithm data | compiler/lexer |
| `RightBracket` (exported) | `compiler/lexer/lexer.go:158` | enum | algorithm data | compiler/lexer |
| `Amp` (exported) | `compiler/lexer/lexer.go:159` | enum | algorithm data | compiler/lexer |
| `At` (exported) | `compiler/lexer/lexer.go:160` | enum | algorithm data | compiler/lexer |
| `Caret` (exported) | `compiler/lexer/lexer.go:161` | enum | algorithm data | compiler/lexer |
| `Tilde` (exported) | `compiler/lexer/lexer.go:162` | enum | algorithm data | compiler/lexer |
| `ShiftLeft` (exported) | `compiler/lexer/lexer.go:163` | enum | algorithm data | compiler/lexer |
| `ShiftRight` (exported) | `compiler/lexer/lexer.go:164` | enum | algorithm data | compiler/lexer |
| `And` (exported) | `compiler/lexer/lexer.go:165` | enum | algorithm data | compiler/lexer |
| `Or` (exported) | `compiler/lexer/lexer.go:166` | enum | algorithm data | compiler/lexer |
| `Pipe` (exported) | `compiler/lexer/lexer.go:167` | enum | algorithm data | compiler/lexer |
| `Is` (exported) | `compiler/lexer/lexer.go:168` | enum | algorithm data | compiler/lexer |
| `StringLiteral` (exported) | `compiler/lexer/lexer.go:169` | enum | algorithm data | compiler/lexer |
| `RawStringLiteral` (exported) | `compiler/lexer/lexer.go:170` | enum | algorithm data | compiler/lexer |
| `InterpStringStart` (exported) | `compiler/lexer/lexer.go:177` | enum | algorithm data | compiler/lexer |
| `InterpText` (exported) | `compiler/lexer/lexer.go:178` | enum | algorithm data | compiler/lexer |
| `InterpOpen` (exported) | `compiler/lexer/lexer.go:179` | enum | algorithm data | compiler/lexer |
| `InterpClose` (exported) | `compiler/lexer/lexer.go:180` | enum | algorithm data | compiler/lexer |
| `InterpStringEnd` (exported) | `compiler/lexer/lexer.go:181` | enum | algorithm data | compiler/lexer |
| `Fun` (exported) | `compiler/lexer/lexer.go:182` | enum | algorithm data | compiler/lexer |
| `Struct` (exported) | `compiler/lexer/lexer.go:183` | enum | algorithm data | compiler/lexer |
| `Union` (exported) | `compiler/lexer/lexer.go:184` | enum | algorithm data | compiler/lexer |
| `Method` (exported) | `compiler/lexer/lexer.go:185` | enum | algorithm data | compiler/lexer |
| `End` (exported) | `compiler/lexer/lexer.go:186` | enum | algorithm data | compiler/lexer |
| `Return` (exported) | `compiler/lexer/lexer.go:187` | enum | algorithm data | compiler/lexer |
| `If` (exported) | `compiler/lexer/lexer.go:188` | enum | algorithm data | compiler/lexer |
| `ElseIf` (exported) | `compiler/lexer/lexer.go:189` | enum | algorithm data | compiler/lexer |
| `Else` (exported) | `compiler/lexer/lexer.go:190` | enum | algorithm data | compiler/lexer |
| `While` (exported) | `compiler/lexer/lexer.go:191` | enum | algorithm data | compiler/lexer |
| `Break` (exported) | `compiler/lexer/lexer.go:192` | enum | algorithm data | compiler/lexer |
| `Continue` (exported) | `compiler/lexer/lexer.go:193` | enum | algorithm data | compiler/lexer |
| `Defer` (exported) | `compiler/lexer/lexer.go:194` | enum | algorithm data | compiler/lexer |
| `Try` (exported) | `compiler/lexer/lexer.go:195` | enum | algorithm data | compiler/lexer |
| `Errdefer` (exported) | `compiler/lexer/lexer.go:196` | enum | algorithm data | compiler/lexer |
| `Spawn` (exported) | `compiler/lexer/lexer.go:197` | enum | algorithm data | compiler/lexer |
| `As` (exported) | `compiler/lexer/lexer.go:198` | enum | algorithm data | compiler/lexer |
| `Match` (exported) | `compiler/lexer/lexer.go:199` | enum | algorithm data | compiler/lexer |
| `Then` (exported) | `compiler/lexer/lexer.go:200` | enum | algorithm data | compiler/lexer |
| `Self` (exported) | `compiler/lexer/lexer.go:201` | enum | algorithm data | compiler/lexer |
| `For` (exported) | `compiler/lexer/lexer.go:202` | enum | algorithm data | compiler/lexer |
| `In` (exported) | `compiler/lexer/lexer.go:203` | enum | algorithm data | compiler/lexer |
| `Do` (exported) | `compiler/lexer/lexer.go:204` | enum | algorithm data | compiler/lexer |
| `Let` (exported) | `compiler/lexer/lexer.go:205` | enum | algorithm data | compiler/lexer |
| `ByteLiteral` (exported) | `compiler/lexer/lexer.go:206` | enum | algorithm data | compiler/lexer |
| `RuneLiteral` (exported) | `compiler/lexer/lexer.go:207` | enum | algorithm data | compiler/lexer |
| `Import` (exported) | `compiler/lexer/lexer.go:208` | enum | algorithm data | compiler/lexer |
| `Export` (exported) | `compiler/lexer/lexer.go:209` | enum | algorithm data | compiler/lexer |
| `Unsafe` (exported) | `compiler/lexer/lexer.go:210` | enum | algorithm data | compiler/lexer |
| `ModulePathLiteral` (exported) | `compiler/lexer/lexer.go:211` | enum | algorithm data | compiler/lexer |
| `CHeaderLiteral` (exported) | `compiler/lexer/lexer.go:216` | enum | algorithm data | compiler/lexer |
| `Ellipsis` (exported) | `compiler/lexer/lexer.go:220` | enum | algorithm data | compiler/lexer |
| `EOF` (exported) | `compiler/lexer/lexer.go:221` | enum | algorithm data | compiler/lexer |
| `keywords` | `compiler/lexer/lexer.go:224` | map | algorithm data | compiler/lexer |
| `maxInterpolationDepth` | `compiler/lexer/lexer.go:443` | number | tunable | compiler/config |
| `RelativeImportReference` (exported) | `compiler/parser/ast.go:122` | enum | algorithm data | compiler/parser |
| `StandardLibraryImportReference` (exported) | `compiler/parser/ast.go:124` | enum | algorithm data | compiler/parser |
| `CHeaderImportReference` (exported) | `compiler/parser/ast.go:127` | enum | algorithm data | compiler/parser |
| `maxSyntaxDepth` | `compiler/parser/parser.go:56` | number | tunable | compiler/config |
| `noMatchBoundary` | `compiler/parser/parser.go:78` | enum | algorithm data | compiler/parser |
| `scrutineeBoundary` | `compiler/parser/parser.go:79` | enum | algorithm data | compiler/parser |
| `armBoundary` | `compiler/parser/parser.go:80` | enum | algorithm data | compiler/parser |
| `targetProfiles` | `compiler/profile.go:29` | map | algorithm data | compiler/compiler |
| `configPageSize` | `compiler/project.go:32` | number | tunable | compiler/config |
| `RuntimeMimalloc` (exported) | `compiler/runtime_dependency.go:11` | string | owning phase | compiler/compiler |
| `RuntimeLibuv` (exported) | `compiler/runtime_dependency.go:12` | string | owning phase | compiler/compiler |
| `RuntimeUtf8proc` (exported) | `compiler/runtime_dependency.go:13` | string | owning phase | compiler/compiler |
| `RuntimeABIVersion` (exported) | `compiler/runtimeabi.go:9` | number | ABI fact | compiler/config |
| `MaxInlineStringCapacity` (exported) | `compiler/types/collections.go:116` | number | tunable | compiler/config |
| `ErrorHeaderCapacity` (exported) | `compiler/types/collections.go:157` | number | tunable | compiler/config |
| `ErrorMessageCapacity` (exported) | `compiler/types/collections.go:158` | number | tunable | compiler/config |
| `ErrorHeaderText` (exported) | `compiler/types/collections.go:163` | value | language fact | compiler/types |
| `ErrorMessageText` (exported) | `compiler/types/collections.go:165` | value | language fact | compiler/types |
| `builtinInlineStrings` | `compiler/types/collections.go:168` | slice | language fact | compiler/types |
| `PositionBinding` (exported) | `compiler/types/collections.go:267` | enum | algorithm data | compiler/types |
| `PositionObjectMember` (exported) | `compiler/types/collections.go:268` | enum | algorithm data | compiler/types |
| `PositionADTPayload` (exported) | `compiler/types/collections.go:269` | enum | algorithm data | compiler/types |
| `PositionUnionMember` (exported) | `compiler/types/collections.go:270` | enum | algorithm data | compiler/types |
| `PositionArrayElement` (exported) | `compiler/types/collections.go:271` | enum | algorithm data | compiler/types |
| `PositionSliceElement` (exported) | `compiler/types/collections.go:272` | enum | algorithm data | compiler/types |
| `PositionListElement` (exported) | `compiler/types/collections.go:273` | enum | algorithm data | compiler/types |
| `PositionDictValue` (exported) | `compiler/types/collections.go:274` | enum | algorithm data | compiler/types |
| `PositionFunctionParam` (exported) | `compiler/types/collections.go:275` | enum | algorithm data | compiler/types |
| `PositionFunctionResult` (exported) | `compiler/types/collections.go:276` | enum | algorithm data | compiler/types |
| `PositionTaskArgument` (exported) | `compiler/types/collections.go:277` | enum | algorithm data | compiler/types |
| `PositionTaskResult` (exported) | `compiler/types/collections.go:278` | enum | algorithm data | compiler/types |
| `PositionChannelElement` (exported) | `compiler/types/collections.go:279` | enum | algorithm data | compiler/types |
| `PositionPointee` (exported) | `compiler/types/collections.go:280` | enum | algorithm data | compiler/types |
| `PositionHeapAllocation` (exported) | `compiler/types/collections.go:281` | enum | algorithm data | compiler/types |
| `ErrorKindVariantNames` (exported) | `compiler/types/error_kind.go:10` | slice | mutable state | RESOLVE (unexport or freeze) |
| `errorKindHeaders` | `compiler/types/error_kind.go:41` | map | language fact | compiler/types |
| `ErrorKindType` (exported) | `compiler/types/error_kind.go:72` | value | language fact | compiler/types |
| `FileType` (exported) | `compiler/types/file.go:10` | struct | language fact | compiler/types |
| `FileModeType` (exported) | `compiler/types/file.go:18` | value | language fact | compiler/types |
| `IOType` (exported) | `compiler/types/io.go:12` | struct | language fact | compiler/types |
| `BytesType` (exported) | `compiler/types/io.go:21` | struct | language fact | compiler/types |
| `SeekType` (exported) | `compiler/types/io.go:61` | value | language fact | compiler/types |
| `StreamUnknown` (exported) | `compiler/types/io.go:81` | enum | algorithm data | compiler/types |
| `StreamReadable` (exported) | `compiler/types/io.go:82` | enum | algorithm data | compiler/types |
| `StreamWritable` (exported) | `compiler/types/io.go:83` | enum | algorithm data | compiler/types |
| `StreamReadWrite` (exported) | `compiler/types/io.go:84` | enum | algorithm data | compiler/types |
| `addressIPv4Bytes` | `compiler/types/network.go:17` | value | language fact | compiler/types |
| `addressIPv6Bytes` | `compiler/types/network.go:18` | value | language fact | compiler/types |
| `AddressType` (exported) | `compiler/types/network.go:24` | value | language fact | compiler/types |
| `TcpConnectionType` (exported) | `compiler/types/network.go:28` | struct | language fact | compiler/types |
| `TcpListenerType` (exported) | `compiler/types/network.go:35` | struct | language fact | compiler/types |
| `EnvironmentVariableType` (exported) | `compiler/types/process.go:10` | value | language fact | compiler/types |
| `EnvironmentType` (exported) | `compiler/types/process.go:13` | value | language fact | compiler/types |
| `ProcessStreamType` (exported) | `compiler/types/process.go:16` | value | language fact | compiler/types |
| `ProcessOptionsType` (exported) | `compiler/types/process.go:18` | value | language fact | compiler/types |
| `ExitStatusType` (exported) | `compiler/types/process.go:21` | value | language fact | compiler/types |
| `StartedProcessType` (exported) | `compiler/types/process.go:25` | value | language fact | compiler/types |
| `ProcessType` (exported) | `compiler/types/process.go:28` | struct | language fact | compiler/types |
| `PipeType` (exported) | `compiler/types/process.go:36` | struct | language fact | compiler/types |
| `builtinStructuralUnions` | `compiler/types/process.go:156` | enum | algorithm data | compiler/types |
| `SignalType` (exported) | `compiler/types/signal.go:8` | value | language fact | compiler/types |
| `SignalsType` (exported) | `compiler/types/signal.go:11` | struct | language fact | compiler/types |
| `TargetX86_64WindowsGNU` (exported) | `compiler/types/target.go:16` | string | owning phase | compiler/types |
| `TargetX86_64LinuxGNU` (exported) | `compiler/types/target.go:20` | string | owning phase | compiler/types |
| `TerminalSizeType` (exported) | `compiler/types/terminal.go:10` | value | language fact | compiler/types |
| `DurationType` (exported) | `compiler/types/time.go:11` | struct | language fact | compiler/types |
| `InstantType` (exported) | `compiler/types/time.go:19` | struct | language fact | compiler/types |
| `WallTimeType` (exported) | `compiler/types/time.go:27` | struct | language fact | compiler/types |
| `ScalarNone` (exported) | `compiler/types/types.go:19` | enum | algorithm data | compiler/types |
| `ScalarSignedInteger` (exported) | `compiler/types/types.go:20` | enum | algorithm data | compiler/types |
| `ScalarUnsignedInteger` (exported) | `compiler/types/types.go:21` | enum | algorithm data | compiler/types |
| `ScalarFloat` (exported) | `compiler/types/types.go:22` | enum | algorithm data | compiler/types |
| `ScalarBool` (exported) | `compiler/types/types.go:23` | enum | algorithm data | compiler/types |
| `SemanticError` (exported) | `compiler/types/types.go:186` | string | owning phase | compiler/types |
| `TypeError` (exported) | `compiler/types/types.go:187` | string | owning phase | compiler/types |
| `NameError` (exported) | `compiler/types/types.go:188` | string | owning phase | compiler/types |
| `ModuleError` (exported) | `compiler/types/types.go:189` | string | owning phase | compiler/types |
| `ConfigurationError` (exported) | `compiler/types/types.go:190` | string | owning phase | compiler/types |
| `UnknownError` (exported) | `compiler/types/types.go:191` | string | owning phase | compiler/types |
| `SyntaxError` (exported) | `compiler/types/types.go:192` | string | owning phase | compiler/types |
| `TruthinessInvalid` (exported) | `compiler/types/types.go:951` | enum | algorithm data | compiler/types |
| `TruthinessBool` (exported) | `compiler/types/types.go:952` | enum | algorithm data | compiler/types |
| `TruthinessNil` (exported) | `compiler/types/types.go:953` | enum | algorithm data | compiler/types |
| `TruthinessNullable` (exported) | `compiler/types/types.go:954` | enum | algorithm data | compiler/types |
| `TruthinessUnion` (exported) | `compiler/types/types.go:955` | enum | algorithm data | compiler/types |
| `TruthinessAlwaysTrue` (exported) | `compiler/types/types.go:956` | enum | algorithm data | compiler/types |
| `canonicalOpaqueTypes` | `compiler/types/types.go:1091` | enum | algorithm data | compiler/types |
| `Int8` (exported) | `compiler/types/types.go:1338` | value | language fact | compiler/types |
| `Int16` (exported) | `compiler/types/types.go:1339` | value | language fact | compiler/types |
| `Int32` (exported) | `compiler/types/types.go:1340` | value | language fact | compiler/types |
| `Int64` (exported) | `compiler/types/types.go:1341` | value | language fact | compiler/types |
| `UInt8` (exported) | `compiler/types/types.go:1342` | value | language fact | compiler/types |
| `UInt16` (exported) | `compiler/types/types.go:1343` | value | language fact | compiler/types |
| `UInt32` (exported) | `compiler/types/types.go:1344` | value | language fact | compiler/types |
| `UInt64` (exported) | `compiler/types/types.go:1345` | value | language fact | compiler/types |
| `Rune` (exported) | `compiler/types/types.go:1349` | value | language fact | compiler/types |
| `Float32` (exported) | `compiler/types/types.go:1350` | value | language fact | compiler/types |
| `Float64` (exported) | `compiler/types/types.go:1351` | value | language fact | compiler/types |
| `Bool` (exported) | `compiler/types/types.go:1352` | value | language fact | compiler/types |
| `Int` (exported) | `compiler/types/types.go:1356` | other | language fact | compiler/types |
| `Float` (exported) | `compiler/types/types.go:1357` | other | language fact | compiler/types |
| `UInt` (exported) | `compiler/types/types.go:1358` | other | language fact | compiler/types |
| `Nil` (exported) | `compiler/types/types.go:1360` | struct | language fact | compiler/types |
| `EoS` (exported) | `compiler/types/types.go:1369` | struct | language fact | compiler/types |
| `Unknown` (exported) | `compiler/types/types.go:1375` | struct | language fact | compiler/types |
| `Heap` (exported) | `compiler/types/types.go:1382` | struct | language fact | compiler/types |
| `StringType` (exported) | `compiler/types/types.go:1388` | struct | language fact | compiler/types |
| `SizeType` (exported) | `compiler/types/types.go:1397` | struct | language fact | compiler/types |
| `ErrorType` (exported) | `compiler/types/types.go:1408` | value | language fact | compiler/types |
| `MutexType` (exported) | `compiler/types/types.go:1413` | struct | language fact | compiler/types |
| `builtinTypes` | `compiler/types/types.go:1443` | map | language fact | compiler/types |
| `builtinUnionOrder` | `compiler/types/unions.go:205` | map | language fact | compiler/types |
| `losslessWideningTargets` | `compiler/types/widening.go:8` | map | language fact | compiler/types |
| `wideningRank` | `compiler/types/widening.go:30` | map | language fact | compiler/types |
| `clangVersionPattern` | `internal/backend/backend.go:45` | value | driver fact | internal/backend |
| `RequiredHeaders` (exported) | `internal/backend/facilities.go:12` | slice | mutable state | RESOLVE (unexport or freeze) |
| `RequiredFacilities` (exported) | `internal/backend/facilities.go:22` | slice | mutable state | RESOLVE (unexport or freeze) |
| `StageConfiguration` (exported) | `internal/driver/driver.go:29` | string | owning phase | compiler/driver |
| `StageFilesystem` (exported) | `internal/driver/driver.go:30` | string | owning phase | compiler/driver |
| `StageHexal` (exported) | `internal/driver/driver.go:31` | string | owning phase | compiler/driver |
| `StageCompile` (exported) | `internal/driver/driver.go:32` | string | owning phase | compiler/driver |
| `StageLink` (exported) | `internal/driver/driver.go:33` | string | owning phase | compiler/driver |
| `stagingBaseName` | `internal/driver/driver.go:153` | string | owning phase | compiler/driver |
| `linuxFeatureDefines` | `internal/driver/driver.go:581` | slice | driver fact | internal/driver |
| `defaultForeignDialect` | `internal/driver/foreign.go:26` | string | owning phase | compiler/driver |
| `foreignDialects` | `internal/driver/foreign.go:30` | map | driver fact | internal/driver |
| `inspectionByteLimit` | `internal/driver/frontend.go:37` | number | tunable | compiler/config |
| `inspectionTimeout` | `internal/driver/frontend.go:38` | number | tunable | compiler/config |
| `clangMinimumMajor` | `internal/driver/frontend.go:39` | number | driver fact | internal/driver |
| `inspectionBudgetMessage` | `internal/driver/frontend.go:451` | string | owning phase | compiler/driver |
| `ModeDebug` (exported) | `internal/driver/mode.go:35` | string | owning phase | compiler/driver |
| `ModeRelease` (exported) | `internal/driver/mode.go:36` | string | owning phase | compiler/driver |
| `modeOptionTable` | `internal/driver/mode.go:88` | map | driver fact | internal/driver |
| `kindTranslationUnit` | `internal/driver/normalize.go:25` | string | owning phase | compiler/driver |
| `kindTypedef` | `internal/driver/normalize.go:26` | string | owning phase | compiler/driver |
| `kindRecord` | `internal/driver/normalize.go:27` | string | owning phase | compiler/driver |
| `kindField` | `internal/driver/normalize.go:28` | string | owning phase | compiler/driver |
| `kindEnum` | `internal/driver/normalize.go:29` | string | owning phase | compiler/driver |
| `kindEnumConstant` | `internal/driver/normalize.go:30` | string | owning phase | compiler/driver |
| `kindFunction` | `internal/driver/normalize.go:31` | string | owning phase | compiler/driver |
| `kindParm` | `internal/driver/normalize.go:32` | string | owning phase | compiler/driver |
| `kindVar` | `internal/driver/normalize.go:33` | string | owning phase | compiler/driver |
| `cSpellings` | `internal/driver/normalize.go:458` | map | driver fact | internal/driver |
| `exactWidthTypedef` | `internal/driver/normalize.go:476` | map | driver fact | internal/driver |
| `scalarCSpelling` | `internal/driver/normalize.go:745` | map | driver fact | internal/driver |
| `qualifiedTriple` | `internal/driver/profile.go:21` | string | owning phase | compiler/driver |
| `driverProfiles` | `internal/driver/profile.go:34` | map | driver fact | internal/driver |
| `value` | `internal/version/version.go:14` | value | driver fact | internal/version |
| `development` | `internal/version/version.go:18` | string | owning phase | compiler/version |
| `timestampPattern` | `internal/version/version.go:20` | value | driver fact | internal/version |
| `months` | `internal/version/version.go:22` | map | driver fact | internal/version |
| `runtimePacks` | `lib/pack.go:14` | enum | algorithm data | compiler/lib |
| `workbenchAddress` | `workbench/main.go:22` | string | owning phase | compiler/workbench |
| `indexHTML` | `workbench/main.go:29` | enum | algorithm data | compiler/workbench |
| `indexPath` | `workbench/main.go:33` | string | owning phase | compiler/workbench |
| `categoryFiles` | `workbench/snippets/catalog.go:15` | enum | algorithm data | workbench/snippets |
| `RequiredReservedWords` (exported) | `workbench/snippets/catalog.go:54` | slice | mutable state | RESOLVE (unexport or freeze) |
| `RequiredFeatures` (exported) | `workbench/snippets/catalog.go:63` | slice | mutable state | RESOLVE (unexport or freeze) |
