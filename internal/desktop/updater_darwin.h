#include <stdbool.h>
int bot_updater_start(void);
int bot_updater_check(void);
bool bot_updater_automatic(void);
void bot_updater_set_automatic(bool enabled);
bool bot_updater_waiting(void);
bool bot_updater_claim(void);
bool bot_updater_finish(void);
void bot_updater_fail(const char *message);
void bot_updater_quit_after_failure(void);
void bot_updater_stop(void);
