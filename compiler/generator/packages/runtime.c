/* Runtime support: the one program-wide diagnostic trap. */
#include "hexal.h"

[[noreturn]] void hex_runtime_trap(const char *message) {
    fputs(message, stderr);
    abort();
}
