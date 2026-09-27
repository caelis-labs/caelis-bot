//go:build darwin && cgo
#pragma once
#include <stdint.h>
void *bot_terminal_instance_open(const char *bundle, const char *script);
void *bot_terminal_instance_adopt(int pid, const char *bundle);
int bot_terminal_instance_ready(void *handle);
int bot_terminal_instance_open_document(void *handle, const char *script);
// 1 pending, 2 replied, 3 cancelled, 4 timed out, -2 permission,
// -3 unsupported, -1 failed, 0 no document event (optional launch adapter).
int bot_terminal_instance_document_result(void *handle);
// State: 0 unknown, 1 exited, 2 hidden, 3 background, 4 foreground.
int bot_terminal_instance_observe(void *handle, int *state, int *pid, uint64_t *birth);
// Commands: 1 normal quit, 2 show/activate, 3 hide. No force termination.
int bot_terminal_instance_apply(void *handle, int command);
int bot_terminal_instance_request_close(void *handle);
// 1 awaiting reply, 2 accepted (still observe exit), 3 user cancelled,
// 4 reply timed out, -2 permission, -1 failure, 0 not requested.
int bot_terminal_instance_close_result(void *handle);
void bot_terminal_instance_release(void *handle);
uint64_t bot_terminal_input_epoch(void);
// Receipt client only: 1 same live process, 0 proven exit/reuse, -1 unknown.
int bot_terminal_client_state(int pid, uint64_t expected_birth, uint64_t *birth);
