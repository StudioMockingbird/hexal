# Hexal Grammar

This document records the grammar implemented by the current compiler slice.
Whitespace and comments may separate tokens; statements do not require a
terminator.

```ebnf
program                  = { top-level-item } ;
identifier               = ASCII-letter
                           , { ASCII-letter | decimal-digit | "_" } ;
top-level-item           = type-declaration | statement ;
type-declaration         = "type" , identifier , [ generic-parameter-list ]
                           , "=" , type-definition-expression ;
generic-parameter-list   = "<" , identifier , { "," , identifier } , ">" ;
generic-type             = identifier , type-argument-list ;
type-argument-list       = "<" , type-expression
                           , { "," , type-expression } , ">" ;
statement                = declaration | assignment | call-statement
                            | return-statement | if-statement
                            | while-statement | for-statement
                            | "break" | "continue" | defer-statement
                            | errdefer-statement ;
declaration              = [ "mut" ] , identifier , ":" , type-expression
                            , "=" , expression ;
assignment               = assignment-target , "=" , expression ;
assignment-target        = place-expression ;
call-statement            = call-expression ;
defer-statement           = "defer" , user-expression ;
errdefer-statement        = "errdefer" , user-expression ;
user-expression           = expression ;
call-expression           = postfix-expression , "(" , [ expression
                            , { "," , expression } ] , ")" ;
return-statement          = "return" , [ expression ] ;
if-statement              = "if" , expression , block
                            , { "elseif" , expression , block }
                            , [ "else" , block ] , "end" ;
while-statement           = "while" , expression , "do" , block , "end" ;
for-statement             = "for" , for-binders , "in" , expression
                            , "do" , block , "end" ;
for-binders               = identifier
                            | identifier , "," , identifier
                            | identifier , "," , identifier , "," , identifier ;
block                    = { statement } ;

type-expression          = union-type-expression ;
union-type-expression    = primary-type-expression
                           , { "|" , primary-type-expression } ;
primary-type-expression  = identifier
                           | generic-type
                           | array-type
                           | view-type
                           | pointer-constructor , "<" , type-expression , ">"
                           | function-type-expression
                           | "(" , type-expression , ")" ;
array-type               = "Array" , "<" , type-expression
                           , "," , positive-decimal-literal , ">" ;
view-type                = "View" , "<" , type-expression , ">" ;
positive-decimal-literal = nonzero-decimal-digit
                           , { decimal-digit | "_" , decimal-digit } ;
pointer-constructor      = "Ptr" | "MutPtr" ;
function-type-expression = "Fun" , "<" , "(" , [ type-expression
                           , { "," , type-expression } ] , ")"
                           , [ ":" , type-expression ] , ">" ;
collection-type-expression = "List" , type-argument-list
                           | "Dict" , type-argument-list ;
type-definition-expression = object-type-expression
                           | type-expression ;
object-type-expression   = "{" , member-declaration
                           , { "," , member-declaration } , [ "," ] , "}" ;
member-declaration       = [ "mut" ] , identifier , ":" , type-expression ;

expression               = or-expression ;
or-expression            = and-expression
                           , { logical-or-operator , and-expression } ;
and-expression           = bitwise-or-expression
                           , { logical-and-operator , bitwise-or-expression } ;
bitwise-or-expression    = bitwise-xor-expression
                           , { "|" , bitwise-xor-expression } ;
bitwise-xor-expression   = bitwise-and-expression
                           , { "^" , bitwise-and-expression } ;
bitwise-and-expression   = equality-expression
                           , { "&" , equality-expression } ;
equality-expression      = type-test-expression
                           , { equality-operator , type-test-expression } ;
type-test-expression     = relational-expression
                           , [ "is" , type-expression ] ;
relational-expression    = shift-expression
                           , { relational-operator , shift-expression } ;
shift-expression         = additive-expression
                           , { shift-operator , additive-expression } ;
additive-expression      = multiplicative-expression
                           , { additive-operator , multiplicative-expression } ;
multiplicative-expression = unary-expression
                           , { multiplicative-operator , unary-expression } ;
unary-expression         = unary-operator , unary-expression
                         | try-expression
                         | reference-expression
                         | spawn-expression
                         | postfix-expression ;
try-expression           = "try" , unary-expression ;
reference-expression     = "ref" , place-expression ;
spawn-expression         = "spawn" , call-expression ;
place-expression         = identifier
                           , { "." , identifier | index-suffix } ;
postfix-expression       = primary-expression
                           , { "." , identifier
                             | call-arguments
                             | generic-call-suffix
                             | index-suffix } ;
generic-call-suffix      = type-argument-list , call-arguments ;
primary-expression       = identifier
                         | object-literal
                         | array-literal
                         | collection-constructor
                         | channel-constructor
                         | atomic-constructor
                         | view-bridge-call
                         | integer-literal
                         | decimal-floating-literal
                         | byte-literal
                         | rune-literal
                         | "true"
                         | "false"
                         | "nil"
                         | string-literal
                         | "(" , expression , ")" ;
string-literal           = '"' , { character | escape-sequence } , '"' ;
escape-sequence          = "\" , ( '"' | "\" | "n" | "t" | "r" | "0"
                         | "u{" , hex-digit , { hex-digit } , "}" ) ;
index-suffix             = "[" , expression , "]" ;
array-literal            = "[" , expression
                           , { "," , expression } , [ "," ] , "]" ;
collection-constructor   = ( "List" , type-argument-list
                          | "Dict" , type-argument-list )
                          , "." , "new" , call-arguments ;
channel-constructor      = "Channel" , type-argument-list , "." , "new"
                         , call-arguments ;
atomic-constructor       = "Atomic" , type-argument-list , "." , "new"
                         , call-arguments ;
view-bridge-call         = "View" , type-argument-list , "."
                         , ( "from_pointer" | "empty" ) , call-arguments ;
unary-operator            = "-" | "!" | "~" ;
additive-operator         = "+" | "-" ;
multiplicative-operator  = "*" | "/" | "%" ;
relational-operator      = "<" | "<=" | ">" | ">=" ;
equality-operator        = "==" | "!=" ;
logical-and-operator     = "and" ;
logical-or-operator      = "or" ;
shift-operator            = "<<" | ">>" ;
operator-token            = unary-operator
                           | additive-operator
                           | multiplicative-operator
                           | relational-operator
                           | equality-operator
                           | logical-and-operator
                           | logical-or-operator
                           | shift-operator ;
grouping-token            = "(" | ")" ;
object-literal            = identifier , [ type-argument-list ] , "{"
                           , [ member-initializer
                             , { "," , member-initializer }
                             , [ "," ] ] , "}" ;
member-initializer        = identifier , "=" , expression ;

integer-literal          = decimal-integer
                           | hexadecimal-integer
                           | binary-integer
                           | octal-integer ;
decimal-integer          = "0"
                           | nonzero-decimal-digit
                             , { decimal-digit | "_" , decimal-digit } ;
hexadecimal-integer      = "0x" , hex-digit
                             , { hex-digit | "_" , hex-digit } ;
binary-integer            = "0b" , binary-digit
                             , { binary-digit | "_" , binary-digit } ;
octal-integer             = "0o" , octal-digit
                             , { octal-digit | "_" , octal-digit } ;

decimal-floating-literal  = decimal-integer , "." , decimal-digit-sequence
                              , [ exponent-part ]
                            | decimal-integer , exponent-part ;

byte-literal             = "b" , "'" , byte-literal-body , "'" ;
rune-literal             = "'" , rune-literal-body , "'" ;
byte-literal-body        = byte-source-character | byte-escape ;
rune-literal-body        = rune-source-character | rune-escape ;
byte-escape              = "\" , ( "\" | "'" | "n" | "r" | "t" | "0"
                         | "x" , hex-digit , hex-digit ) ;
rune-escape              = "\" , ( "\" | "'" | '"' | "n" | "r" | "t" | "0"
                         | "u{" , hex-digit , { hex-digit } , "}" ) ;
exponent-part             = ( "e" | "E" ) , [ "+" | "-" ]
                             , decimal-digit-sequence ;
decimal-digit-sequence    = decimal-digit
                             , { decimal-digit | "_" , decimal-digit } ;

nonzero-decimal-digit     = "1" | "2" | "3" | "4" | "5"
                           | "6" | "7" | "8" | "9" ;
decimal-digit             = "0" | nonzero-decimal-digit ;
binary-digit              = "0" | "1" ;
octal-digit               = "0" | "1" | "2" | "3" | "4" | "5" | "6" | "7" ;
hex-digit                 = decimal-digit
                           | "a" | "b" | "c" | "d" | "e" | "f"
                           | "A" | "B" | "C" | "D" | "E" | "F" ;
```

The built-in scalar names are `Bool`, `UInt8`, `UInt16`, `UInt32`, `UInt64`,
`Int8`, `Int16`, `Int32`, `Int64`, `Float32`, and `Float64`. `Byte` is a
transparent source alias of `UInt8` and accepts `b'...'` byte literals. `Nil`
is the singleton null type and `Unknown` is
the incomplete pointee type; both are type-position identifiers that cannot be
redeclared. `nil` is a reserved literal keyword. `mut`, `ref`, `type`, `and`,
`or`, `is`, `try`, and `errdefer` are reserved words. A union is structural:
members are flattened,
duplicates are removed, and member order is retained only for contextual
initializer priority.
`Ptr` and `MutPtr` are the two pointer type constructors, recognized in type
position only; they cannot be redeclared. `Fun` is the function-pointer type
constructor. Dotted names are parsed generically and resolved by the checker.
`.value` is the built-in pointer deref member; object members such as `value`
and `addr` are ordinary members. `.addr` on a scalar or pointer is rejected by
the checker with a migration diagnostic; address-taking uses `ref`. `P | Nil`
is a nullable union; exactly one pointer-like member plus `Nil` uses the pointer
null niche, while every other multi-member union uses a tagged value.

Generic declarations follow the name: `type Box<T> = ...`, `fun identity<T>(...)`,
and `impl Box<T>.method<U>(...)`. A generic type use `Box<Int32>` is a type
expression. A balanced type-argument list is a generic call suffix only when
immediately followed by a call argument list; otherwise `<` and `>` keep their
relational meaning. A generic object literal `Box<Int32> { ... }` is parsed as
a primary expression, and `Box { ... }` infers its arguments from the expected
type.

`if`, `elseif`, `else`, `while`, `for`, `in`, `do`, `break`, `continue`,
`defer`, `errdefer`, `fun`, `impl`, `end`, `try`, and `return` are reserved
control-flow and
declaration words. The `elseif` form is one keyword; it is not parsed as
`else` followed by `if`. `eos` is a reserved literal token naming the RFC
0031 end-of-stream singleton.

`defer` accepts a complete expression and discards its result. `errdefer`
uses the same syntax and register/lifetime rules but its action runs only
when the enclosing function exits with an Error return. Every `if`,
`elseif`, and `else` branch is a cleanup scope. Every `while` and `for` body
creates a fresh cleanup scope for each iteration; deferred expressions run
when that branch or iteration exits, including through `break` and
`continue`.

`print` is a protected built-in name resolved by the checker, not a keyword
and not a grammar production: it uses ordinary call syntax. The same holds
for the compiler-owned generic method spellings `to<T>()`, `bit_cast<T>()`,
`to_le_bytes()`, `to_be_bytes()`, the type-qualified endian
constructors, and the layout intrinsics `size_of<T>()` and `align_of<T>()`;
they reuse the existing generic-call suffix.

`spawn` is a reserved prefix word whose operand is a direct call to a named
function. The protected built-in names `Task`, `Channel`, `Mutex`, `Atomic`,
`File`, `FileMode`, `Stdio`, `View`, `RuneCursor`, and `Byte` resolve before
ordinary declaration lookup and cannot be redeclared or shadowed. `View`,
`Channel`, and `Atomic` are recognized in type position; their
constructor-form calls use the productions above, `File.open` and the
`Stdio.stdin`/`stdout`/`stderr` forms are ordinary qualified calls, and the
checker dispatches each type-qualified form explicitly.

A byte literal contains exactly one byte; a direct source character must be
printable one-byte ASCII. A rune literal contains exactly one Unicode scalar.
String literals may contain an escaped NUL.

`do` is the mandatory boundary between a loop header and its body for both
loop kinds: `while condition do ... end` and `for binders in source do ...
end`. `for` iterates Array, View, List, String, Strand, Dict, and Stream
sources; the optional first binder is a `Size` index, followed by the
value binder (sequences and Streams), the key and value binders (Dicts), or
the rune binder (text).

Identifiers are case-sensitive, must begin with an ASCII letter, and may use
digits or underscores after that first letter. All reserved words are excluded
after lexing. Boolean literals are also keyword tokens.
`main`, C keywords, and C macro spellings remain valid Hexal identifiers.

The operator tokens are maximal-munch lexed: `!=`, `==`, `<=`, `>=`, `<<`,
and `>>` are single tokens, never a pair of punctuation tokens. `<` and `>`
are also used by `Ptr<...>` type expressions. While parsing nested type
arguments the parser may consume one `>>` token as two consecutive `>`
generic closers; a `>>` in expression position is never treated as two
relational operators. `|` is a union separator in type parsing and bitwise
or in expression parsing; parser context separates those grammars. `~`, `&`,
`^`, `|`, `<<`, and `>>` are operator tokens, not reserved identifiers.
`-` is both prefix and infix; grammar position,
not whitespace, determines which use applies. Parentheses group both value and
type expressions; a type-test `is` binds tighter than `==` and `!=` and cannot
be chained without an enclosing expression.

`try` is a prefix operator on a unary expression. It binds like the other
prefix unary operators and is valid only inside a function whose result
accepts Error.

Comments may separate any tokens. `--` runs to the end of the line and
`--[ ... ]--` is a multiline comment. The existing `---` line-comment spelling
is also consumed as a `--` comment before `-` tokens are considered.

Decimal integer values other than zero cannot have redundant leading zeros.
Base prefixes are lowercase, and separators are allowed only between digits
of the same digit sequence. `.5`, `1.`, hexadecimal floating syntax, and
numeric suffixes are not grammar forms.
Expression-side `mut` is removed; `mut` appears only before a binding name or
an object member name. `ref` accepts only a place expression, never a literal
or a parenthesized value.
