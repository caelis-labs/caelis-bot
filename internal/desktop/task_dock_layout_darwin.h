#import <Cocoa/Cocoa.h>

// AppKit-only geometry. Other desktop hosts choose their own surfaces, gestures
// and coordinate system; this is not part of the task/backend contract.
typedef struct {
    NSRect frame;
    BOOL vertical;
    BOOL growsUp;
} BotTaskDockPlacement;

static CGFloat taskDockClamp(CGFloat value, CGFloat lower, CGFloat upper) {
    return MAX(lower, MIN(value, MAX(lower, upper)));
}
static BotTaskDockPlacement taskDockPlacement(NSRect pet, NSRect visible, NSUInteger count, BOOL expanded) {
    // Before the first native screen observation the view is not displayed.
    if(visible.size.width<=16 || visible.size.height<=16)visible=NSMakeRect(0,0,800,600);
    NSRect safe=NSInsetRect(visible,8,8);
    CGFloat feet=NSMinY(pet)+44*pet.size.height/240;
    CGFloat anchor=taskDockClamp(feet,NSMinY(safe),NSMaxY(safe));
    BotTaskDockPlacement result={NSZeroRect,NO,NO};
    if(!expanded) {
        result.frame=NSMakeRect(taskDockClamp(NSMidX(pet)-31,NSMinX(safe),NSMaxX(safe)-62),taskDockClamp(feet-36,NSMinY(safe),NSMaxY(safe)-28),MIN(62,safe.size.width),MIN(28,safe.size.height));
        return result;
    }
    // Bound the surface by the current work area, not by the number of tasks.
    // Extra tasks consume overlap inside these bounds rather than scroll space.
    CGFloat maxWidth=MIN(safe.size.width,MAX(256,MIN(760,safe.size.width*0.65)));
    CGFloat maxHeight=MIN(safe.size.height,MAX(196,MIN(620,safe.size.height*0.70)));
    CGFloat width=MIN(maxWidth,256+MAX(0,(NSInteger)count-1)*156);
    CGFloat height=MIN(196,safe.size.height);
    CGFloat below=MAX(0,anchor-8-NSMinY(safe));
    BOOL centered=NSMidX(pet)-width/2>=NSMinX(safe) && NSMidX(pet)+width/2<=NSMaxX(safe);
    CGFloat right=NSMaxX(safe)-NSMaxX(pet)-8,left=NSMinX(pet)-8-NSMinX(safe);
    CGFloat up=MAX(0,NSMaxY(safe)-anchor),down=MAX(0,anchor-NSMinY(safe));
    if(count>1 && (!centered || below<height) && MAX(left,right)>=240 && MAX(up,down)>=196) {
        result.vertical=YES;result.growsUp=up>=down;
        height=MIN(MIN(maxHeight,196+(count-1)*116),result.growsUp?up:down);
        CGFloat x=right>=left?NSMaxX(pet)+8:NSMinX(pet)-8-240;
        result.frame=NSMakeRect(taskDockClamp(x,NSMinX(safe),NSMaxX(safe)-240),result.growsUp?anchor:anchor-height,240,height);
        return result;
    }
    CGFloat above=MAX(0,NSMaxY(safe)-NSMaxY(pet)-8);
    result.growsUp=below<height && above>below;
    CGFloat y=result.growsUp?NSMaxY(pet)+8:feet-8-height;
    result.frame=NSMakeRect(taskDockClamp(NSMidX(pet)-width/2,NSMinX(safe),NSMaxX(safe)-width),taskDockClamp(y,NSMinY(safe),NSMaxY(safe)-height),width,height);
    return result;
}

// Every card stays inside the bounded surface, including very dense lists.
// Frames remain stable during hover; only painting and depth order change.
static NSRect taskDockCardFrame(NSSize size, NSUInteger count, NSUInteger index, BOOL vertical, BOOL growsUp) {
    CGFloat width=MIN(224,MAX(1,size.width-16));
    CGFloat height=MIN(vertical?144:192,MAX(1,size.height-4));
    CGFloat span=MAX(0,vertical?size.height-height-4:size.width-width-32);
    CGFloat stride=count>1?MIN(vertical?116:156,span/(count-1)):0;
    CGFloat x=vertical?8:8+index*stride;
    CGFloat y=vertical?(growsUp?index*stride:size.height-height-4-index*stride):0;
    // Clamp after multiplication too: the last reverse-growing slot can acquire
    // a tiny negative origin through floating-point rounding.
    return NSMakeRect(taskDockClamp(x,0,size.width-width),taskDockClamp(y,0,size.height-height),width,height);
}
