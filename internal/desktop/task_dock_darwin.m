//go:build darwin && cgo

#import "task_dock_darwin.h"

@interface BotTaskPanel : NSPanel
@end
@implementation BotTaskPanel
- (BOOL)canBecomeKeyWindow { return NO; }
- (BOOL)canBecomeMainWindow { return NO; }
@end

@interface BotTaskButton : NSButton
@property(copy) void (^hover)(BOOL);
@property BOOL hovered;
@property NSTrackingArea *tracking;
@end
@implementation BotTaskButton
- (BOOL)acceptsFirstMouse:(NSEvent *)event { return YES; }
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    if (self.tracking) [self removeTrackingArea:self.tracking];
    self.tracking = [[NSTrackingArea alloc] initWithRect:NSZeroRect options:NSTrackingMouseEnteredAndExited|NSTrackingActiveAlways|NSTrackingInVisibleRect owner:self userInfo:nil];
    [self addTrackingArea:self.tracking];
}
- (void)mouseEntered:(NSEvent *)event { self.hovered=YES; self.needsDisplay=YES; if(self.hover)self.hover(YES); }
- (void)mouseExited:(NSEvent *)event { self.hovered=NO; self.needsDisplay=YES; if(self.hover)self.hover(NO); }
- (void)drawRect:(NSRect)dirty {
    NSRect rect=NSInsetRect(self.bounds,1.5,1.5);
    NSBezierPath *shape=[NSBezierPath bezierPathWithRoundedRect:rect xRadius:rect.size.height/2 yRadius:rect.size.height/2];
    [[NSColor.controlBackgroundColor colorWithAlphaComponent:self.hovered ? 0.95 : 0.65] setFill]; [shape fill];
    [[NSColor.labelColor colorWithAlphaComponent:self.hovered ? 0.25 : 0.09] setStroke]; shape.lineWidth=1; [shape stroke];
    NSDictionary *attributes=@{NSFontAttributeName:[NSFont systemFontOfSize:12 weight:NSFontWeightMedium],NSForegroundColorAttributeName:NSColor.secondaryLabelColor};
    NSSize size=[self.title sizeWithAttributes:attributes];
    [self.title drawAtPoint:NSMakePoint((self.bounds.size.width-size.width)/2,(self.bounds.size.height-size.height)/2) withAttributes:attributes];
}
@end

@interface BotTaskSurface : NSVisualEffectView
@property(copy) void (^leave)(void);
@property NSTrackingArea *tracking;
@end
@implementation BotTaskSurface
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    if(self.tracking)[self removeTrackingArea:self.tracking];
    self.tracking=[[NSTrackingArea alloc] initWithRect:NSZeroRect options:NSTrackingMouseEnteredAndExited|NSTrackingActiveAlways|NSTrackingInVisibleRect owner:self userInfo:nil];
    [self addTrackingArea:self.tracking];
}
- (void)mouseExited:(NSEvent *)event { if(self.leave)self.leave(); }
@end

@interface BotTaskDock ()
@property NSPanel *window;
@property NSPanel *preview;
@property NSTextField *prompt;
@property(nonatomic) NSArray *tasks;
@property NSArray *pendingTasks;
@property BOOL expanded;
@property BOOL visible;
@property NSRect pet;
@property NSRect bounds;
@property NSTimer *openTimer;
@property NSTimer *closeTimer;
@property NSTimer *previewTimer;
@property NSUInteger hoverGeneration;
@end

@implementation BotTaskDock
static NSPanel *taskPanel(NSString *title) {
    NSPanel *panel=[[BotTaskPanel alloc] initWithContentRect:NSMakeRect(0,0,48,26) styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
    panel.title=title; panel.opaque=NO; panel.backgroundColor=NSColor.clearColor;
    panel.hasShadow=YES; panel.hidesOnDeactivate=NO; panel.releasedWhenClosed=NO;
    panel.level=NSFloatingWindowLevel+1;
    panel.collectionBehavior=NSWindowCollectionBehaviorCanJoinAllSpaces|NSWindowCollectionBehaviorStationary|NSWindowCollectionBehaviorIgnoresCycle|NSWindowCollectionBehaviorFullScreenAuxiliary;
    if (@available(macOS 13.0,*)) panel.collectionBehavior|=NSWindowCollectionBehaviorCanJoinAllApplications;
    return panel;
}
static BotTaskSurface *taskMaterial(NSRect rect,CGFloat radius) {
    BotTaskSurface *view=[[BotTaskSurface alloc] initWithFrame:rect];
    view.material=NSVisualEffectMaterialPopover; view.blendingMode=NSVisualEffectBlendingModeBehindWindow; view.state=NSVisualEffectStateActive;
    view.wantsLayer=YES; view.layer.cornerRadius=radius; view.layer.masksToBounds=YES;
    view.layer.borderWidth=0.5; view.layer.borderColor=[NSColor.labelColor colorWithAlphaComponent:0.09].CGColor;
    return view;
}
- (instancetype)init {
    if((self=[super init])) {
        _tasks=@[]; _window=taskPanel(@"Caelis Bot — 任务"); _preview=taskPanel(@"Caelis Bot — 任务提示");
        _preview.ignoresMouseEvents=YES;
        NSVisualEffectView *surface=taskMaterial(NSMakeRect(0,0,320,70),24);
        _prompt=[NSTextField wrappingLabelWithString:@""];
        _prompt.frame=NSMakeRect(18,14,284,42); _prompt.font=[NSFont systemFontOfSize:13];
        _prompt.textColor=NSColor.labelColor; _prompt.maximumNumberOfLines=2;
        _prompt.lineBreakMode=NSLineBreakByTruncatingTail; _prompt.selectable=NO;
        [surface addSubview:_prompt]; _preview.contentView=surface;
    }
    return self;
}
- (NSUInteger)count { return self.tasks.count; }
- (void)setTasks:(NSArray *)tasks {
    NSMutableArray *valid=[NSMutableArray new]; NSMutableSet *ids=[NSMutableSet new];
    for(id entry in tasks) {
        if(![entry isKindOfClass:NSDictionary.class])continue;
        id identifier=entry[@"id"], prompt=entry[@"prompt"];
        if(![identifier isKindOfClass:NSString.class] || ![identifier length] || [ids containsObject:identifier] || ![prompt isKindOfClass:NSString.class])continue;
        [valid addObject:@{@"id":identifier,@"prompt":prompt}]; [ids addObject:identifier];
    }
    // An incoming snapshot must not move another task under the pointer.
    if(self.expanded && valid.count) { self.pendingTasks=valid; return; }
    _tasks=[valid copy]; self.pendingTasks=nil;
    if(!valid.count) [self collapse]; else [self render];
    [self layout];
}
- (BotTaskButton *)button:(NSString *)title frame:(NSRect)frame index:(NSInteger)index {
    BotTaskButton *button=[[BotTaskButton alloc] initWithFrame:frame];
    button.title=title; button.bordered=NO; button.tag=index; button.target=self;
    button.action=index<0 ? @selector(expand) : @selector(open:);
    __weak BotTaskDock *weak=self;
    button.hover=^(BOOL entered){ [weak hover:index entered:entered]; };
    button.accessibilityLabel=index<0 ? @"展开任务" : [self promptAt:index];
    if(index>=0)button.accessibilityHelp=@"在终端中打开此任务";
    return button;
}
- (NSString *)promptAt:(NSInteger)index {
    NSString *text=self.tasks[index][@"prompt"];
    return text.length ? text : @"早期任务未保存原始提示词";
}
- (void)render {
    CGFloat width=self.expanded ? MIN(284, self.tasks.count*46+10) : 48;
    CGFloat height=self.expanded ? 52 : 26;
    BotTaskSurface *surface=taskMaterial(NSMakeRect(0,0,width,height),height/2);
    __weak BotTaskDock *weak=self;
    surface.leave=^{[weak hover:-2 entered:NO];};
    if(self.expanded) {
        NSScrollView *scroll=[[NSScrollView alloc] initWithFrame:NSMakeRect(6,6,width-12,40)];
        scroll.drawsBackground=NO; scroll.hasHorizontalScroller=YES; scroll.autohidesScrollers=YES;
        scroll.scrollerStyle=NSScrollerStyleOverlay; scroll.horizontalScrollElasticity=NSScrollElasticityAllowed;
        NSView *row=[[NSView alloc] initWithFrame:NSMakeRect(0,0,self.tasks.count*46-2,40)];
        for(NSUInteger i=0;i<self.tasks.count;i++) [row addSubview:[self button:[NSString stringWithFormat:@"%lu",(unsigned long)i+1] frame:NSMakeRect(i*46+2,2,36,36) index:i]];
        scroll.documentView=row; [surface addSubview:scroll];
    } else [surface addSubview:[self button:@"···" frame:surface.bounds index:-1]];
    self.window.contentView=surface;
    NSRect frame=self.window.frame; frame.size=surface.frame.size; [self.window setFrame:frame display:YES];
}
- (void)hover:(NSInteger)index entered:(BOOL)entered {
    if(!self.visible)return;
    [self.openTimer invalidate]; self.openTimer=nil;
    [self.closeTimer invalidate]; self.closeTimer=nil;
    [self.previewTimer invalidate]; self.previewTimer=nil;
    NSUInteger generation=++self.hoverGeneration;
    __weak BotTaskDock *weak=self;
    if(entered) {
        if(index<0) self.openTimer=[NSTimer scheduledTimerWithTimeInterval:0.16 repeats:NO block:^(NSTimer *timer){[weak expand];}];
        else if((NSUInteger)index<self.tasks.count) [self showPrompt:[self promptAt:index]];
    } else {
        self.closeTimer=[NSTimer scheduledTimerWithTimeInterval:0.42 repeats:NO block:^(NSTimer *timer){
            if(generation!=weak.hoverGeneration)return;
            if(NSPointInRect(NSEvent.mouseLocation,weak.window.frame)) { [weak.preview orderOut:nil]; return; }
            [weak collapse];
        }];
    }
}
- (void)expand {
    if(self.expanded || !self.visible || !self.tasks.count)return;
    [self.openTimer invalidate]; self.openTimer=nil;
    self.expanded=YES; [self render]; [self layout];
    if(self.gesture)self.gesture(@"attention");
}
- (void)collapse {
    [self.openTimer invalidate]; self.openTimer=nil; [self.closeTimer invalidate]; self.closeTimer=nil;
    [self.previewTimer invalidate]; self.previewTimer=nil; ++self.hoverGeneration;
    self.expanded=NO; [self.preview orderOut:nil];
    if(self.pendingTasks) { _tasks=self.pendingTasks; self.pendingTasks=nil; }
    [self render]; [self layout];
}
- (void)open:(BotTaskButton *)button {
    if(!self.visible || button.tag<0 || (NSUInteger)button.tag>=self.tasks.count)return;
    NSString *identifier=self.tasks[button.tag][@"id"];
    // Close the local affordance before launching; this does not stop a worker.
    [self collapse];
    if(self.gesture)self.gesture(@"nod");
    if(self.openTask)self.openTask(identifier);
}
- (void)placeWithPet:(NSRect)pet bounds:(NSRect)bounds visible:(BOOL)visible {
    self.pet=pet; self.bounds=bounds; self.visible=visible;
    if(!visible)[self collapse]; else [self layout];
}
- (void)layout {
    if(!self.visible || !self.tasks.count) { [self.window orderOut:nil]; [self.preview orderOut:nil]; return; }
    NSRect frame=self.window.frame, bounds=self.bounds;
    frame.origin.x=MAX(NSMinX(bounds)+8,MIN(NSMidX(self.pet)-frame.size.width/2,NSMaxX(bounds)-frame.size.width-8));
    // The model's feet are 44/240 above the base of its transparent canvas.
    frame.origin.y=MAX(NSMinY(bounds)+8,NSMinY(self.pet)+44*self.pet.size.height/240-frame.size.height-8);
    [self.window setFrame:frame display:YES];
    if(!self.window.visible)[self.window orderFrontRegardless];
}
- (void)showPrompt:(NSString *)text {
    self.prompt.stringValue=text;
    NSRect bounds=self.bounds; CGFloat width=MIN(320,bounds.size.width-16);
    NSRect frame=NSMakeRect(MAX(NSMinX(bounds)+8,MIN(NSMidX(self.pet)-width/2,NSMaxX(bounds)-width-8)),NSMaxY(self.pet)+8,width,70);
    if(NSMaxY(frame)>NSMaxY(bounds)-8) {
        frame.origin.x=NSMinX(self.pet)-width-8;
        if(frame.origin.x<NSMinX(bounds)+8)frame.origin.x=NSMaxX(self.pet)+8;
        frame.origin.x=MAX(NSMinX(bounds)+8,MIN(frame.origin.x,NSMaxX(bounds)-width-8));
        frame.origin.y=MAX(NSMinY(bounds)+8,NSMaxY(self.pet)-70);
    }
    self.prompt.frame=NSMakeRect(18,14,width-36,42);
    [self.preview setFrame:frame display:YES]; [self.preview orderFrontRegardless];
}
- (void)showFailure:(NSString *)message {
    if(!self.visible)return;
    [self showPrompt:message];
    [self.previewTimer invalidate]; __weak BotTaskDock *weak=self;
    self.previewTimer=[NSTimer scheduledTimerWithTimeInterval:4 repeats:NO block:^(NSTimer *timer){[weak.preview orderOut:nil];}];
}
- (void)stop {
    self.visible=NO; [self collapse]; [self.window close]; [self.preview close];
    self.openTask=nil; self.gesture=nil;
}
@end
