#ifndef BOT_PRESENCE_H
#define BOT_PRESENCE_H
#include <stdint.h>
char *bot_presence_sample(void);
char *bot_presence_encode(int awake, int lock_known, int locked, int console, int login,
                      double idle, const char *application);
#endif
