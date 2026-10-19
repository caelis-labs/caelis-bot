#ifndef BOT_CARE_H
#define BOT_CARE_H
#include <stdint.h>
char *bot_care_sample(void);
char *bot_care_encode(int awake, int lock_known, int locked, int console, int login,
                      double idle, uint64_t epoch, const char *application);
#endif
