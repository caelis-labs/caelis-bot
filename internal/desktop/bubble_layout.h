#ifndef BOT_BUBBLE_LAYOUT_H
#define BOT_BUBBLE_LAYOUT_H
#include <math.h>

typedef struct { double x,y,width,height,maxHeight; } BotBubbleLayout;

// AppKit points. Choose the anchor from pet/screen geometry alone, never from
// the requested height: growing on hover must contain the collapsed rectangle.
static inline BotBubbleLayout bot_bubble_layout(double px,double py,double pw,double ph,
    double sx,double sy,double sw,double sh,double requested) {
    double left=sx+8,right=sx+sw-8,bottom=sy+8,top=sy+sh-8;
    double width=fmin(360,fmax(1,right-left));
    double above=top-(py+ph+8);
    int side=above<160;
    double anchor=side ? fmin(top,fmax(bottom+68,py+ph)) : py+ph+8;
    double available=side ? anchor-bottom : above;
    double limit=floor(fmin(480,fmin(sh*.55,fmax(1,available))));
    double height=fmin(fmax(68,requested),limit);
    double x=px+pw/2-width/2;
    if(side) { x=px-width-8; if(x<left)x=px+pw+8; }
    x=fmax(left,fmin(x,right-width));
    BotBubbleLayout result={x,side?anchor-height:anchor,width,height,limit};
    return result;
}
#endif
