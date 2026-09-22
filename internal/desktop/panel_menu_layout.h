#ifndef BOT_PANEL_MENU_LAYOUT_H
#define BOT_PANEL_MENU_LAYOUT_H
#include <math.h>

typedef struct {
    double originY, height, inputTop, menuTop, menuHeight, menuLeft, menuWidth;
} BotPanelMenuLayout;

// AppKit's upward Y axis. The input's screen rectangle remains invariant; only
// its transparent hosting window grows to contain the separate popup.
static inline BotPanelMenuLayout bot_panel_menu_layout(double y, double width, double height,
    double screenMinY, double screenMaxY, double requested) {
    BotPanelMenuLayout result = {y,height,0,0,0,0,width};
    if (requested <= 0) return result;
    double above = fmax(0,screenMaxY-y-height-16);
    double below = fmax(0,y-screenMinY-16);
    int up = below < requested && above > below;
    result.menuHeight = floor(fmin(requested,up ? above : below));
    double extra = result.menuHeight+16; // 8pt gap plus 8pt outer shadow space.
    result.height += extra;
    result.originY -= up ? 0 : extra;
    result.inputTop = up ? extra : 0;
    result.menuTop = up ? 8 : height+8;
    return result;
}
#endif
