#include "main.h"

static const char hello_world_text[] = "Hello, world!\n";

const char *hello_world(void) {
    return hello_world_text;
}

size_t hello_world_length(void) {
    return sizeof(hello_world_text) - 1;
}
