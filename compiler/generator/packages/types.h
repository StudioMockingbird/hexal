#ifndef HEXAL_TYPES_H
#define HEXAL_TYPES_H

#include "hexal.h"
{{range .Includes}}#include "{{.}}"
{{end}}
{{.Definitions}}
#endif
