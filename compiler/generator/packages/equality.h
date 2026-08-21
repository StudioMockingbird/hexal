#ifndef HEXAL_EQUALITY_H
#define HEXAL_EQUALITY_H

#include "hexal.h"
{{range .Includes}}#include "{{.}}"
{{end}}
{{if .NeedStddef}}#include <stddef.h>
{{end}}{{if .NeedString}}#include <string.h>
{{end}}{{if .NeedStdlib}}#include <stdlib.h>
{{end}}

{{range .Helpers}}
{{.Body}}
{{end}}

#endif
