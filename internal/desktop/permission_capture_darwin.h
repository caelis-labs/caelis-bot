//go:build darwin && cgo
#pragma once
void *bot_screen_permission_request(void);
int bot_screen_permission_poll(void *handle, long *error);
void bot_screen_permission_release(void *handle);
int bot_screen_capture_denied(void);
void bot_screen_capture_set_denied(int denied);
