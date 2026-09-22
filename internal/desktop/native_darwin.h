#include <stdint.h>
typedef struct { double x,y,width,height; } BotRect;
void *bot_create(void *pet, void *panel, void *bubble, void *history, void *prop, uintptr_t handle, unsigned char *icon, int iconLength);
void bot_bubble(void *host, int visible);
int bot_screens(BotRect *rects, int capacity);
void bot_apply(void *host, double x, double y, double scale, int visible);
void bot_panel(void *host, int visible);
void bot_toggle_panel(void *host);
void bot_panel_height(void *host, int height);
void bot_mask(void *host, unsigned char *mask, int length);
void bot_destroy(void *host);

void bot_expand_bubble(void *host, int expanded);
void bot_bubble_height(void *host, int height);

void bot_gesture(void *host, char *action);
void bot_notify(void *host, char *identifier, char *title, char *body, int reminder);
int bot_notification_status(void *host);
void bot_configure_notifications(void *host);
int bot_trash_path(char *path);

void bot_style_settings(void *window);

void bot_activity(void *host, char *activity);

char *bot_context(void *host);
void bot_plane_ready(void *host, int ready);
int bot_launch_plane(void *host, char *id, double x, double y);
void bot_finish_plane(void *host, char *id, int completed);
