//go:build darwin && cgo

#pragma once
#include <stdbool.h>

void *task_ghostty_open(const char *command);
// Retained common instance owner, valid before LaunchServices returns a PID.
void *task_ghostty_open_instance(void *handle);
int task_ghostty_open_poll(void *handle, int *pid, long *error);
void task_ghostty_open_release(void *handle);

#ifdef __OBJC__
#import <Cocoa/Cocoa.h>
// Also used by the native contract fixture; none of these send events.
NSWorkspaceOpenConfiguration *task_ghostty_configuration(void);
NSAppleEventDescriptor *task_ghostty_create_event(pid_t pid, NSString *command);
bool task_ghostty_dictionary_supports_window(NSXMLDocument *dictionary);
BOOL task_ghostty_show(NSRunningApplication *app);
#endif
