#ifndef HEXAL_H
#define HEXAL_H
{{if .Includes}}
{{range .Includes}}#include <{{.}}>
{{end}}
{{end}}{{if .SizeAsserts}}{{range .SizeAsserts}}static_assert({{.}} <= SIZE_MAX, "Size literal {{.}} requires a size_t target wide enough");
{{end}}{{end}}{{if .Eos}}
typedef uint8_t hex_eos;
{{end}}{{if .TrapDeclared}}
[[noreturn]] void hex_runtime_trap(const char *message);
{{end}}
{{.FamilyContent}}
#endif
