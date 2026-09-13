#ifndef HEXAL_EVENT_H
#define HEXAL_EVENT_H

#include "hexal/concurrency.h"

typedef void (*hex_event_work_entry)(void *context);
typedef void (*hex_event_work_failure)(void *context);
typedef void (*hex_event_timer_entry)(void *context);
typedef void (*hex_event_poll_entry)(void *context, int status, int events);
typedef void (*hex_event_dns_entry)(void *context, void *result, int status);
void hex_event_runtime_init(void);
void hex_event_runtime_shutdown(void);
void hex_event_work_call(hex_event_work_entry entry, hex_event_work_failure failure, void *context);
void hex_event_timer_wait(uint64_t timeout_ms, hex_event_timer_entry entry, hex_event_work_failure failure, void *context);
void hex_event_poll_wait(intptr_t descriptor, int events, hex_event_poll_entry entry, void *context);
void hex_event_dns_lookup(const char *node, const char *service, int family, hex_event_dns_entry entry, hex_event_work_failure failure, void *context);
uint64_t hex_event_idle_time(void);

#endif
