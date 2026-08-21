#ifndef HEXAL_NUMERIC_H
#define HEXAL_NUMERIC_H

#include "hexal.h"
{{if .NeedArray}}#include "hexal/array.h"
{{end}}
{{range .Conversions}}
{{.Body}}
{{end}}{{range .Divisions}}
{{.Body}}
{{end}}{{range .Shifts}}
{{.Body}}
{{end}}{{range .BitCasts}}
{{.Body}}
{{end}}{{range .Endians}}
{{.Body}}
{{end}}
#endif
