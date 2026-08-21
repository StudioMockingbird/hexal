#ifndef HEXAL_EQUALITY_H
#define HEXAL_EQUALITY_H

#include "hexal.h"
{{range .Includes}}#include "{{.}}"
{{end}}
#include <string.h>
#include <stdlib.h>

{{range .Helpers}}
{{.Body}}
{{end}}

#endif
