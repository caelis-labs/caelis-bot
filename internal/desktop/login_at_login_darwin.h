#ifndef CAELIS_LOGIN_AT_LOGIN_DARWIN_H
#define CAELIS_LOGIN_AT_LOGIN_DARWIN_H

int bot_login_item_status(const char *expected_id);
int bot_login_item_set(const char *expected_id, int enabled, char **error_message);
int bot_login_item_open_settings(void);
void bot_track_login_launch(void);
int bot_launched_at_login(void);

#endif
