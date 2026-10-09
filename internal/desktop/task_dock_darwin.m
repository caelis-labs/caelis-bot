//go:build darwin && cgo

#import "task_dock_darwin.h"
#import "task_dock_layout_darwin.h"
#import <QuartzCore/QuartzCore.h>
#import "task_snapshot_darwin.h"

@interface BotTaskPanel : NSPanel
@end
@implementation BotTaskPanel
- (BOOL)canBecomeKeyWindow { return NO; }
- (BOOL)canBecomeMainWindow { return NO; }
@end

@interface BotTaskButton : NSButton
@property(copy) void (^hover)(BOOL);
@property BOOL hovered;
@property NSString *detail;
@property NSImage *snapshot;
@property NSString *snapshotTime;
@property NSString *statusText;
@property NSString *terminalKind;
@property NSImage *terminalIcon;
@property BOOL prominent;
@property BOOL compactPreview;
@property BOOL captionOnly;
@property NSRect exposedRect;
@property NSPoint downPoint;
@property BOOL dragging;
@property BOOL gestureCancelled;
@property BOOL pressed;
@property(copy) void (^drag)(NSInteger phase,NSPoint point);
@property(copy) void (^swipe)(CGFloat delta,NSEventPhase phase);
@property NSTrackingArea *tracking;
@property CALayer *progress;
@property NSArray<CALayer *> *dots;
@property BOOL loading;
@property BOOL locked;
@property BOOL animateLoading;
- (void)updateProgress;
@end

@interface BotTaskStack : NSView
@property BOOL vertical;
@property BOOL growsUp;
@property NSTrackingArea *tracking;
@property BotTaskButton *selected;
@property NSArray<BotTaskButton *> *cards;
- (void)focusButton:(BotTaskButton *)button;
@end

@implementation BotTaskButton
- (BOOL)isFlipped { return NO; }
- (BOOL)acceptsFirstMouse:(NSEvent *)event { return YES; }
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    if(self.tag>=0)return;
    if (self.tracking) [self removeTrackingArea:self.tracking];
    self.tracking = [[NSTrackingArea alloc] initWithRect:NSZeroRect options:NSTrackingMouseEnteredAndExited|NSTrackingActiveAlways|NSTrackingInVisibleRect owner:self userInfo:nil];
    [self addTrackingArea:self.tracking];
}
- (void)mouseEntered:(NSEvent *)event {
    self.hovered=YES;self.needsDisplay=YES;
    for(NSView *control in self.subviews)if([control isKindOfClass:NSButton.class])control.hidden=NO;
    if(self.tag>=0) {
        [(BotTaskStack *)self.superview focusButton:self];
    }
    if(self.hover)self.hover(YES);
}
- (void)mouseExited:(NSEvent *)event {
    self.hovered=NO;self.needsDisplay=YES;
    for(NSView *control in self.subviews)if([control isKindOfClass:NSButton.class])control.hidden=YES;
    if(self.hover)self.hover(NO);
}
- (void)updateProgress {
    if(!self.progress) {
        self.wantsLayer=YES;self.progress=[CALayer layer];
        [self.layer addSublayer:self.progress];
        NSMutableArray *dots=[NSMutableArray new];
        for(NSUInteger i=0;i<3;i++){CALayer *dot=[CALayer layer];[self.progress addSublayer:dot];[dots addObject:dot];}
        self.dots=dots;
    }
    [CATransaction begin];[CATransaction setDisableActions:YES];
    self.progress.frame=self.bounds;
    self.progress.hidden=self.compactPreview || ((self.tag>=0 || self.captionOnly) && (!self.loading || (!self.prominent && !self.hovered)));
    BOOL entry=self.tag<0 && !self.captionOnly;
    CGFloat centerX=entry?NSMidX(self.bounds):NSMaxX(self.bounds)-65;
    CGFloat centerY=entry?NSMidY(self.bounds):17;
    for(NSUInteger i=0;i<self.dots.count;i++) {
        CALayer *dot=self.dots[i];
        CGFloat size=entry?4.5:3;
        dot.bounds=CGRectMake(0,0,size,size);dot.position=CGPointMake(centerX+((NSInteger)i-1)*8,centerY);
        dot.cornerRadius=size/2;dot.backgroundColor=NSColor.whiteColor.CGColor;
        dot.shadowColor=NSColor.blackColor.CGColor;dot.shadowRadius=1;dot.shadowOpacity=entry?0.55:0;dot.shadowOffset=CGSizeMake(0,-0.5);
        dot.borderColor=[NSColor.blackColor colorWithAlphaComponent:0.25].CGColor;dot.borderWidth=entry?0.5:0;
        dot.opacity=self.loading?0.7:0.45;
        if(self.loading && self.animateLoading && !self.progress.hidden) {
            if(![dot animationForKey:@"pulse"]) {
                CAKeyframeAnimation *movement=[CAKeyframeAnimation animationWithKeyPath:@"transform.translation.y"];
                movement.duration=1.25;movement.values=@[@0,@3,@0,@0];movement.keyTimes=@[@0,@0.25,@0.5,@1];
                CAKeyframeAnimation *opacity=[CAKeyframeAnimation animationWithKeyPath:@"opacity"];
                opacity.duration=1.25;opacity.values=@[@0.35,@1,@0.35,@0.35];opacity.keyTimes=movement.keyTimes;
                CAAnimationGroup *pulse=[CAAnimationGroup animation];pulse.animations=@[movement,opacity];
                pulse.duration=1.25;pulse.repeatCount=HUGE_VALF;pulse.beginTime=CACurrentMediaTime()+i*0.14;
                pulse.timingFunction=[CAMediaTimingFunction functionWithName:kCAMediaTimingFunctionEaseInEaseOut];
                [dot addAnimation:pulse forKey:@"pulse"];
            }
        } else [dot removeAnimationForKey:@"pulse"];
    }
    [CATransaction commit];self.needsDisplay=YES;
}
- (void)mouseDown:(NSEvent *)event {
    if(self.tag<0){[super mouseDown:event];return;}
    self.pressed=self.enabled;
    if(!self.enabled)return;
    self.gestureCancelled=NO;self.downPoint=[self.window convertPointToScreen:event.locationInWindow];self.dragging=NO;
}
- (void)mouseDragged:(NSEvent *)event {
    if(self.tag<0 || !self.enabled)return;
    NSPoint point=[self.window convertPointToScreen:event.locationInWindow];
    if(!self.dragging && hypot(point.x-self.downPoint.x,point.y-self.downPoint.y)<6)return;
    if(!self.dragging){self.dragging=YES;if(self.drag)self.drag(0,self.downPoint);}
    if(self.drag)self.drag(1,point);
}
- (void)mouseUp:(NSEvent *)event {
    if(self.tag<0){[super mouseUp:event];return;}
    BOOL pressed=self.pressed;self.pressed=NO;
    if(!pressed || !self.enabled || self.gestureCancelled)return;
    if(self.dragging){self.dragging=NO;if(self.drag)self.drag(2,[self.window convertPointToScreen:event.locationInWindow]);}
    else [NSApp sendAction:self.action to:self.target from:self];
}
- (void)rightMouseDown:(NSEvent *)event { /* No contextual actions on task cards. */ }
- (void)scrollWheel:(NSEvent *)event {
    if(self.tag<0 || !event.hasPreciseScrollingDeltas || fabs(event.scrollingDeltaX)>fabs(event.scrollingDeltaY)){[super scrollWheel:event];return;}
    // Momentum must never dismiss a second card after the original leaves.
    if(event.momentumPhase!=NSEventPhaseNone)return;
    CGFloat upward=event.scrollingDeltaY*(event.isDirectionInvertedFromDevice?-1:1);
    if(self.swipe)self.swipe(upward,event.phase);
}
- (void)drawRect:(NSRect)dirty {
    if(self.tag<0 && !self.captionOnly)return;
    // Paint exposed edges only: a dense stack must not accumulate dozens of
    // translucent fills into an opaque background.
    if(self.tag>=0)NSRectClip(self.exposedRect);
    if(!self.captionOnly) {
    CGFloat inset=(self.prominent || self.hovered)?3:9;
    NSRect rect=NSMakeRect(inset,self.compactPreview?6:49,self.bounds.size.width-2*inset,self.bounds.size.height-(self.compactPreview?12:55));
    NSBezierPath *shape=[NSBezierPath bezierPathWithRoundedRect:rect xRadius:14 yRadius:14];
    [NSGraphicsContext saveGraphicsState];
    NSShadow *shadow=[NSShadow new];shadow.shadowColor=[NSColor.blackColor colorWithAlphaComponent:0.23];shadow.shadowBlurRadius=self.hovered?10:6;shadow.shadowOffset=NSMakeSize(0,-2);[shadow set];
    [[NSColor colorWithCalibratedWhite:0.10 alpha:self.hovered?0.48:0.32] setFill];[shape fill];
    [NSGraphicsContext restoreGraphicsState];
    [[NSColor.whiteColor colorWithAlphaComponent:0.18] setStroke];shape.lineWidth=0.6;[shape stroke];
    if(self.snapshot) {
        NSRect area=NSInsetRect(rect,1,1);
        CGFloat scale=MIN(area.size.width/self.snapshot.size.width,area.size.height/self.snapshot.size.height);
        NSRect imageRect=NSMakeRect(NSMidX(area)-self.snapshot.size.width*scale/2,NSMidY(area)-self.snapshot.size.height*scale/2,self.snapshot.size.width*scale,self.snapshot.size.height*scale);
        [NSGraphicsContext saveGraphicsState];[shape addClip];[self.snapshot drawInRect:imageRect];[NSGraphicsContext restoreGraphicsState];
    } else {
        NSImage *icon=self.terminalIcon ?: [NSImage imageWithSystemSymbolName:@"terminal" accessibilityDescription:nil];
        [icon drawInRect:NSMakeRect(NSMidX(rect)-24,NSMidY(rect)-24,48,48) fromRect:NSZeroRect operation:NSCompositingOperationSourceOver fraction:0.75];
    }
    }
    // Only the card in front owns a caption: transparent stacked captions must
    // not bleed into each other. Hover reveals the next assignment.
    if(!self.compactPreview && (self.prominent || self.hovered || self.captionOnly)) {
        [NSGraphicsContext saveGraphicsState];
        NSShadow *shadow=[NSShadow new];shadow.shadowColor=[NSColor.blackColor colorWithAlphaComponent:0.85];shadow.shadowBlurRadius=3;[shadow set];
        NSMutableParagraphStyle *paragraph=[NSMutableParagraphStyle new];paragraph.lineBreakMode=NSLineBreakByTruncatingTail;
        NSDictionary *body=@{NSFontAttributeName:[NSFont systemFontOfSize:12 weight:NSFontWeightMedium],NSForegroundColorAttributeName:NSColor.whiteColor,NSParagraphStyleAttributeName:paragraph};
        [self.detail drawInRect:NSMakeRect(10,26,self.bounds.size.width-20,18) withAttributes:body];
        NSDictionary *status=@{NSFontAttributeName:[NSFont systemFontOfSize:10],NSForegroundColorAttributeName:[NSColor.whiteColor colorWithAlphaComponent:0.72]};
        [self.statusText drawInRect:NSMakeRect(10,10,132,14) withAttributes:status];
        [NSGraphicsContext restoreGraphicsState];
    }
}

@end

@implementation BotTaskStack
- (void)focusButton:(BotTaskButton *)button {
    if(!button)return;
    NSUInteger index=[self.cards indexOfObjectIdenticalTo:button];if(index==NSNotFound)return;
    if(self.selected!=button)[self.selected mouseExited:[NSEvent new]];self.selected=button;
    // Fan both sides behind the selected card, nearest neighbor on top. Each
    // card retains an exposed edge in either direction even at high overlap.
    // Using real, fixed hit frames keeps the selected card's controls reachable.
    NSMutableArray *depth=[NSMutableArray new];
    for(NSUInteger i=0;i<index;i++)[depth addObject:self.cards[i]];
    for(NSUInteger i=self.cards.count;i>index+1;i--)[depth addObject:self.cards[i-1]];
    [depth addObject:button];self.subviews=depth;
    for(NSUInteger i=0;i<self.cards.count;i++) {
        BotTaskButton *card=self.cards[i];card.prominent=(i==index);card.exposedRect=card.bounds;
        if(i!=index) {
            BotTaskButton *neighbor=self.cards[i<index?i+1:i-1];
            NSRect covered=[card convertRect:neighbor.bounds fromView:neighbor];
            // Clip at the painted edge, not the hit frame's transparent gutter.
            // Otherwise deep overlap looks like separated strips with gaps.
            CGFloat inset=neighbor==button?3:9;
            covered=NSInsetRect(covered,self.vertical?0:inset,self.vertical?6:0);
            BOOL lowerEdge=(i<index)!= (self.vertical && !self.growsUp);
            if(self.vertical) {
                card.exposedRect=lowerEdge?NSMakeRect(0,0,card.bounds.size.width,MAX(0,NSMinY(covered))):NSMakeRect(0,NSMaxY(covered),card.bounds.size.width,MAX(0,NSMaxY(card.bounds)-NSMaxY(covered)));
            } else {
                card.exposedRect=lowerEdge?NSMakeRect(0,0,MAX(0,NSMinX(covered)),card.bounds.size.height):NSMakeRect(NSMaxX(covered),0,MAX(0,NSMaxX(card.bounds)-NSMaxX(covered)),card.bounds.size.height);
            }
        }
        card.needsDisplay=YES;[card updateProgress];
    }
}
- (void)scrollWheel:(NSEvent *)event { /* No scroll browsing; card swipes own dismissal. */ }
- (void)updateTrackingAreas {
    [super updateTrackingAreas];if(self.tracking)[self removeTrackingArea:self.tracking];
    self.tracking=[[NSTrackingArea alloc] initWithRect:NSZeroRect options:NSTrackingMouseMoved|NSTrackingMouseEnteredAndExited|NSTrackingActiveAlways|NSTrackingInVisibleRect owner:self userInfo:nil];[self addTrackingArea:self.tracking];
}
- (void)mouseMoved:(NSEvent *)event {
    NSPoint point=[self.superview convertPoint:event.locationInWindow fromView:nil];
    NSView *hit=[self hitTest:point];
    while(hit && hit!=self && ![hit isKindOfClass:BotTaskButton.class])hit=hit.superview;
    BotTaskButton *button=[hit isKindOfClass:BotTaskButton.class]?(BotTaskButton *)hit:nil;
    if(button!=self.selected) {
        if(button)[button mouseEntered:event];
        else {[self.selected mouseExited:event];self.selected=nil;}
    }
}
- (void)mouseEntered:(NSEvent *)event { [self mouseMoved:event]; }
- (void)mouseExited:(NSEvent *)event { [self.selected mouseExited:event];self.selected=nil; }
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
@property(nonatomic) NSArray *tasks;
@property NSArray *pendingTasks;
@property BOOL expanded;
@property BOOL stackVertical;
@property BOOL stackUp;
@property BotTaskButton *stackCaption;
@property BOOL visible;
@property NSRect pet;
@property NSRect bounds;
@property NSTimer *openTimer;
@property NSTimer *closeTimer;
@property NSTimer *openingTimer;
@property NSUInteger hoverGeneration;
@property NSArray<BotTaskButton *> *buttons;
@property NSString *opening;
@property(copy, nonatomic) NSDictionary *transitions;
@property NSString *openingMessage;
@property(nonatomic) BOOL shortcutHeld;
@property BOOL shortcutPeek;
@property id outsideMonitor;
@property id localMonitor;
@property NSCache *snapshots;
@property BotTaskSnapshot *snapshotCapture;
@property NSString *snapshotTask;
@property BOOL stopped;
@property BOOL dragging;
@property NSString *dragID;
@property NSPoint dragStart;
@property NSPoint dragPoint;
@property NSRect dragFrame;
@property NSPanel *dragPreview;
@property NSTextField *dragHint;
@property NSString *swipeID;
@property CGFloat swipeDistance;
@property NSString *terminalKind;
@end

@implementation BotTaskDock
static NSPanel *taskPanel(NSString *title) {
    NSPanel *panel=[[BotTaskPanel alloc] initWithContentRect:NSMakeRect(0,0,48,26) styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
    panel.title=title; panel.opaque=NO; panel.backgroundColor=NSColor.clearColor;
    panel.hasShadow=NO;panel.acceptsMouseMovedEvents=YES; panel.hidesOnDeactivate=NO; panel.releasedWhenClosed=NO;
    // The composer rises by one level for its attachment menu. Keep task
    // controls above it without entering AppKit's modal-panel level.
    panel.level=NSFloatingWindowLevel+2;
    panel.collectionBehavior=NSWindowCollectionBehaviorCanJoinAllSpaces|NSWindowCollectionBehaviorStationary|NSWindowCollectionBehaviorIgnoresCycle|NSWindowCollectionBehaviorFullScreenAuxiliary;
    if (@available(macOS 13.0,*)) panel.collectionBehavior|=NSWindowCollectionBehaviorCanJoinAllApplications;
    return panel;
}
- (instancetype)init {
    if((self=[super init])) {
        _snapshots=[NSCache new];_snapshots.countLimit=24;_snapshots.totalCostLimit=16*1024*1024;
        _snapshotCapture=[BotTaskSnapshot new];
        _tasks=@[]; _window=taskPanel(@"Caelis Bot — 任务");
        [NSWorkspace.sharedWorkspace.notificationCenter addObserver:self selector:@selector(updateActivity) name:NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification object:nil];
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
        NSString *status=[entry[@"status"] isKindOfClass:NSString.class] ? entry[@"status"] : @"unknown";
        [valid addObject:@{@"id":identifier,@"prompt":prompt,@"status":status,@"provider":([entry[@"provider"] isKindOfClass:NSString.class]?entry[@"provider"]:@""),@"targetLabel":([entry[@"targetLabel"] isKindOfClass:NSString.class]?entry[@"targetLabel"]:@""),@"locked":@([entry[@"locked"] isKindOfClass:NSNumber.class] && [entry[@"locked"] boolValue]),@"terminal":([entry[@"terminal"] isKindOfClass:NSString.class]?entry[@"terminal"]:@"system")}]; [ids addObject:identifier];
    }
    for(NSDictionary *old in self.tasks)if(![ids containsObject:old[@"id"]])[self.snapshots removeObjectForKey:old[@"id"]];
    if(self.snapshotTask && ![ids containsObject:self.snapshotTask]){[self.snapshotCapture cancel];self.snapshotTask=nil;}
    // A drag owns its original identity until release. Apply the latest list afterwards.
    if(self.dragging){self.pendingTasks=valid;return;}
    // An incoming snapshot must not move another task under the pointer.
    if(self.expanded && valid.count) {
        self.pendingTasks=valid;
        NSMutableArray *stable=[NSMutableArray new];
        for(NSDictionary *old in self.tasks) {
            NSDictionary *latest=nil;
            for(NSDictionary *entry in valid) if([entry[@"id"] isEqual:old[@"id"]]) { latest=entry; break; }
            [stable addObject:latest ?: @{@"id":old[@"id"],@"prompt":old[@"prompt"],@"status":@"unavailable"}];
        }
        BOOL appended=NO;
        for(NSDictionary *entry in valid) {
            BOOL found=NO;for(NSDictionary *old in stable)if([old[@"id"] isEqual:entry[@"id"]]){found=YES;break;}
            if(!found){[stable addObject:entry];appended=YES;}
        }
        _tasks=stable;
        if(appended){[self render];[self layout];}else [self updateActivity];
        return;
    }
    _tasks=[valid copy]; self.pendingTasks=nil;
    if(!valid.count) [self collapse]; else [self render];
    [self layout];
}
- (NSString *)text:(NSString *)key { return self.language[key] ?: key; }
- (void)setLanguage:(NSDictionary<NSString *, NSString *> *)language {
    _language = [language copy];
    _window.title = [self text:@"tasksTitle"];
    if (self.tasks.count) [self render];
}
- (BotTaskButton *)button:(NSString *)title frame:(NSRect)frame index:(NSInteger)index {
    BotTaskButton *button=[[BotTaskButton alloc] initWithFrame:frame];
    button.title=title; button.bordered=NO; button.tag=index; button.target=self;
    // Hover may expand while the entry is tracking a click. Its eventual
    // action must stay "expand", never toggle the new cards closed again.
    button.action=index<0 ? @selector(expand) : @selector(open:);
    __weak BotTaskDock *weak=self;
    button.hover=^(BOOL entered){ [weak hover:index entered:entered]; };
    button.accessibilityLabel=index<0 ? [self text:@"expandTasks"] : [self promptAt:index];
    if(index>=0) {
        button.identifier=self.tasks[index][@"id"];
        button.detail=[self promptAt:index];
        NSDictionary *snapshot=[self.snapshots objectForKey:self.tasks[index][@"id"]];
        button.snapshot=snapshot[@"image"];button.snapshotTime=snapshot[@"time"];
        button.terminalKind=self.tasks[index][@"terminal"];
        NSDictionary *bundles=@{@"terminal":@"com.apple.Terminal",@"iterm2":@"com.googlecode.iterm2",@"ghostty":@"com.mitchellh.ghostty"};
        NSURL *app=([bundles objectForKey:button.terminalKind]?[NSWorkspace.sharedWorkspace URLForApplicationWithBundleIdentifier:bundles[button.terminalKind]]:nil);
        if(app)button.terminalIcon=[NSWorkspace.sharedWorkspace iconForFile:app.path];
        NSString *identifier=self.tasks[index][@"id"];
        button.drag=^(NSInteger phase,NSPoint point){[weak dragTask:identifier phase:phase point:point];};
        button.swipe=^(CGFloat delta,NSEventPhase phase){[weak swipeTask:identifier delta:delta phase:phase];};
        button.accessibilityHelp=[self text:@"openTaskInTerminal"];button.toolTip=button.detail;
    }
    return button;
}
- (NSString *)promptAt:(NSInteger)index {
    NSString *text=self.tasks[index][@"prompt"];
    return text.length ? text : [self text:@"taskPromptMissing"];
}
- (void)render {
    NSString *focusedID=nil;BOOL wasHovered=NO;
    for(BotTaskButton *button in self.buttons)if(button.prominent){focusedID=button.identifier;wasHovered=button.hovered;break;}
    BotTaskDockPlacement placement=taskDockPlacement(self.pet,self.bounds,self.tasks.count,self.expanded);
    self.stackVertical=placement.vertical;self.stackUp=placement.growsUp;
    CGFloat width=placement.frame.size.width,height=placement.frame.size.height;
    NSView *surface=[[NSView alloc] initWithFrame:NSMakeRect(0,0,width,height)];
    self.window.hasShadow=NO;self.stackCaption=nil;
    NSMutableArray *buttons=[NSMutableArray new];
    if(self.expanded) {
        BotTaskStack *row=[[BotTaskStack alloc] initWithFrame:NSMakeRect(0,self.stackVertical?48:0,width,self.stackVertical?height-48:height)];
        row.vertical=self.stackVertical;row.growsUp=self.stackUp;
        for(NSUInteger i=0;i<self.tasks.count;i++) {
            NSRect frame=taskDockCardFrame(row.bounds.size,self.tasks.count,i,self.stackVertical,self.stackUp);
            BotTaskButton *button=[self button:@"" frame:frame index:i];
            button.compactPreview=self.stackVertical;
            NSString *identifier=self.tasks[i][@"id"];
            BOOL locked=[self.tasks[i][@"locked"] boolValue];
            [row addSubview:button positioned:NSWindowBelow relativeTo:nil];[buttons addObject:button];
            NSButton *lock=[self control:locked?@"lock.fill":@"lock.open" label:[self text:locked?@"unlockTask":@"lockTask"] action:@selector(changeLock:) identifier:identifier frame:NSMakeRect(frame.size.width-73,frame.size.height-37,26,26)];
            NSButton *remove=[self control:@"xmark" label:[self text:@"removeTaskPin"] action:@selector(removePin:) identifier:identifier frame:NSMakeRect(frame.size.width-42,frame.size.height-37,26,26)];
            lock.hidden=YES;remove.hidden=YES;[button addSubview:lock];[button addSubview:remove];
        }
        BotTaskButton *focused=buttons.firstObject;
        for(BotTaskButton *button in buttons)if([button.identifier isEqual:focusedID]){focused=button;break;}
        row.cards=buttons;[row focusButton:focused];row.selected=nil;
        // Opening/status projection must not put another card over the target
        // of the user's next click. Identity, not array position, owns focus.
        if(wasHovered && [focused.identifier isEqual:focusedID]) {
            focused.hovered=YES;row.selected=focused;
            for(NSView *control in focused.subviews)control.hidden=NO;
        }
        [surface addSubview:row];
        if(self.stackVertical) {
            BotTaskButton *caption=[[BotTaskButton alloc] initWithFrame:NSMakeRect(8,0,width-16,48)];caption.tag=-2;caption.captionOnly=YES;caption.prominent=YES;caption.enabled=NO;caption.bordered=NO;
            self.stackCaption=caption;[surface addSubview:caption];
        }
    } else {
        BotTaskButton *button=[self button:@"" frame:surface.bounds index:-1];[surface addSubview:button];[buttons addObject:button];
    }
    // NSWindow resizes a newly assigned contentView to its existing bounds.
    // Set the intended content size first, never read it back from that view.
    [self.window setContentSize:NSMakeSize(width,height)];
    self.window.contentView=surface;
    self.buttons=buttons;
    [self updateActivity];
}
- (void)updateActivity {
    BOOL anyActive=NO;
    for(NSDictionary *task in (self.pendingTasks ?: self.tasks)) if([self activeStatus:task[@"status"]])anyActive=YES;
    BOOL animate=!NSWorkspace.sharedWorkspace.accessibilityDisplayShouldReduceMotion;
    for(BotTaskButton *button in self.buttons) {
        NSString *status=button.tag>=0 && (NSUInteger)button.tag<self.tasks.count ? self.tasks[button.tag][@"status"] : @"";
        button.enabled=![status isEqual:@"unavailable"];
        if(!button.enabled)button.pressed=NO;
        NSString *phase=button.tag>=0 ? self.transitions[self.tasks[button.tag][@"id"]] : nil;
        BOOL opening=button.tag<0 ? (self.transitions.count>0 || self.opening.length>0) : (phase.length>0 || [self.tasks[button.tag][@"id"] isEqual:self.opening]);
        NSString *statusKey=[self activeStatus:status] ? @"taskRunning" : ([status isEqual:@"completed"] ? @"taskFinished" : ([status isEqual:@"waiting_approval"] || [status isEqual:@"awaiting_approval"] ? @"taskApproval" : ([status isEqual:@"unknown"] ? @"taskUnknown" : ([status isEqual:@"failed"] ? @"taskFailed" : ([status isEqual:@"interrupted"] ? @"taskInterrupted" : @"taskIdle")))));
        button.statusText=opening?[self text:[phase isEqual:@"closing"]?@"taskClosing":([phase isEqual:@"opening"]?@"taskOpening":@"taskSwitching")]:[self text:statusKey];
        if(button.tag>=0) {
            NSString *machine=self.tasks[button.tag][@"targetLabel"];
            if(machine.length)button.statusText=[NSString stringWithFormat:@"%@ · %@",button.statusText,machine];
            button.accessibilityValue=button.statusText;
            BOOL locked=[self.tasks[button.tag][@"locked"] boolValue];button.locked=locked;

            for(NSView *view in button.subviews)if([view isKindOfClass:NSButton.class]) {
                NSButton *control=(NSButton *)view;
                control.enabled=![status isEqual:@"unavailable"];
                if(control.action==@selector(changeLock:)) {
                    NSString *label=[self text:locked?@"unlockTask":@"lockTask"];
                    control.image=[NSImage imageWithSystemSymbolName:locked?@"lock.fill":@"lock.open" accessibilityDescription:label];
                    control.toolTip=label;control.accessibilityLabel=label;control.contentTintColor=NSColor.whiteColor;
                    control.wantsLayer=YES;control.layer.shadowColor=NSColor.blackColor.CGColor;control.layer.shadowOpacity=0.4;control.layer.shadowRadius=2;
                }
            }
        }
        button.loading=(self.visible || self.shortcutPeek) && (opening || (button.tag<0 ? anyActive : [self activeStatus:status]));
        button.animateLoading=animate; [button updateProgress];
    }
    [self updateStackCaption];
}
- (void)updateStackCaption {
    if(!self.stackCaption)return;
    for(BotTaskButton *button in self.buttons)if(button.prominent) {
        self.stackCaption.detail=button.detail;self.stackCaption.statusText=button.statusText;
        self.stackCaption.loading=button.loading;self.stackCaption.animateLoading=button.animateLoading;
        [self.stackCaption updateProgress];break;
    }
}
- (NSButton *)control:(NSString *)symbol label:(NSString *)label action:(SEL)action identifier:(NSString *)identifier frame:(NSRect)frame {
    NSButton *button=[NSButton buttonWithImage:[NSImage imageWithSystemSymbolName:symbol accessibilityDescription:label] target:self action:action];
    button.bordered=NO;button.frame=frame;button.identifier=identifier;button.contentTintColor=NSColor.whiteColor;button.toolTip=label;button.accessibilityLabel=label;
    return button;
}
- (NSString *)identifierFor:(id)sender {
    return [sender isKindOfClass:NSMenuItem.class]?[sender representedObject]:[sender identifier];
}
- (void)removePin:(id)sender {
    NSString *identifier=[self identifierFor:sender];
    [self collapse];if(self.unpinTask)self.unpinTask(identifier);
}
- (void)changeLock:(id)sender {
    NSString *identifier=[self identifierFor:sender];
    for(NSDictionary *task in self.tasks)if([task[@"id"] isEqual:identifier]) {
        if(self.lockTask)self.lockTask(identifier,![task[@"locked"] boolValue]);break;
    }
}
- (void)captureTask:(NSString *)identifier source:(NSDictionary *)source {
    if(self.transitions[identifier])return; // A late preview must not start during a newer window action.
    BOOL present=NO;for(NSDictionary *task in self.tasks)if([task[@"id"] isEqual:identifier] && ![task[@"status"] isEqual:@"unavailable"]){present=YES;break;}
    if(!present)return;
    // This is a cached still, not a live feed. Repeated toggles reuse a recent
    // image instead of asking the capture service again during each animation.
    NSDate *captured=[self.snapshots objectForKey:identifier][@"created"];
    if(captured && -captured.timeIntervalSinceNow<30)return;
    self.snapshotTask=identifier;
    __weak BotTaskDock *weak=self;
    [self.snapshotCapture capture:source completion:^(NSImage *image){
        if(weak.stopped || ![weak.snapshotTask isEqual:identifier])return;
        weak.snapshotTask=nil;if(!image)return;
        NSDateFormatter *formatter=[NSDateFormatter new];formatter.dateFormat=@"HH:mm";
        NSString *time=[NSString stringWithFormat:@"%@ · %@",[weak text:@"taskSnapshot"],[formatter stringFromDate:NSDate.date]];
        [weak.snapshots setObject:@{@"image":image,@"time":time,@"created":NSDate.date} forKey:identifier cost:640*400*4];
        // Updating an image must not rebuild/reorder a card under the pointer.
        BotTaskButton *button=[weak buttonFor:identifier];button.snapshot=image;button.snapshotTime=time;button.needsDisplay=YES;
    }];
}
- (BOOL)lockedTask:(NSString *)identifier {
    for(NSDictionary *task in self.tasks)if([task[@"id"] isEqual:identifier])return [task[@"locked"] boolValue];return YES;
}
- (BotTaskButton *)buttonFor:(NSString *)identifier {
    for(BotTaskButton *button in self.buttons)if(button.tag>=0 && (NSUInteger)button.tag<self.tasks.count && [self.tasks[button.tag][@"id"] isEqual:identifier])return button;return nil;
}
- (void)dismissGesture:(NSString *)identifier {
    if([self lockedTask:identifier]){[self showFailure:[self text:@"taskUnlockBeforeDismiss"]];return;}
    [self collapse];if(self.unpinTask)self.unpinTask(identifier);
}
- (void)swipeTask:(NSString *)identifier delta:(CGFloat)delta phase:(NSEventPhase)phase {
    if(self.dragging || ![self buttonFor:identifier])return;
    if(phase==NSEventPhaseBegan || ![self.swipeID isEqual:identifier]){self.swipeID=identifier;self.swipeDistance=0;}
    self.swipeDistance=MAX(0,self.swipeDistance+delta);
    BotTaskButton *button=[self buttonFor:identifier];
    [CATransaction begin];[CATransaction setDisableActions:YES];
    button.layer.transform=CATransform3DMakeTranslation(0,MIN([self lockedTask:identifier]?10:45,self.swipeDistance),0);
    [CATransaction commit];
    if(phase==NSEventPhaseEnded || phase==NSEventPhaseCancelled) {
        CGFloat distance=self.swipeDistance;self.swipeID=nil;self.swipeDistance=0;button.layer.transform=CATransform3DIdentity;
        if(phase!=NSEventPhaseCancelled && distance>=65)[self dismissGesture:identifier];
    }
}
- (NSInteger)dragActionAt:(NSPoint)point {
    CGFloat dx=point.x-self.dragStart.x,dy=point.y-self.dragStart.y;
    if(dy>=72 && fabs(dx)<68 && (!self.stackVertical || point.y>NSMaxY(self.window.frame)+28))return 1;
    if(NSPointInRect(point,NSInsetRect(self.window.frame,-28,-28)))return 2;
    return 3;
}
- (void)cancelDrag {
    if(!self.dragging)return;
    if(self.dragID){[self buttonFor:self.dragID].alphaValue=1;[self buttonFor:self.dragID].dragging=NO;[self buttonFor:self.dragID].gestureCancelled=YES;}
    self.dragging=NO;self.dragID=nil;[self.dragPreview close];self.dragPreview=nil;self.dragHint=nil;
    if(self.pendingTasks){NSArray *tasks=self.pendingTasks;self.pendingTasks=nil;_tasks=tasks;[self render];[self layout];}
}
- (void)dragTask:(NSString *)identifier phase:(NSInteger)phase point:(NSPoint)point {
    BotTaskButton *button=[self buttonFor:identifier];if(!button || !button.enabled)return;
    if(phase==0) {
        [self cancelDrag];[(BotTaskStack *)button.superview focusButton:button];self.dragID=identifier;self.dragStart=point;self.dragPoint=point;self.dragging=YES;
        [self.closeTimer invalidate];self.closeTimer=nil;
        self.dragFrame=[button.window convertRectToScreen:[button convertRect:button.bounds toView:nil]];
        NSBitmapImageRep *rep=[button bitmapImageRepForCachingDisplayInRect:button.bounds];[button cacheDisplayInRect:button.bounds toBitmapImageRep:rep];
        NSImage *image=[[NSImage alloc] initWithSize:button.bounds.size];[image addRepresentation:rep];
        self.dragPreview=taskPanel(@"Task drag");self.dragPreview.ignoresMouseEvents=YES;
        NSImageView *view=[[NSImageView alloc] initWithFrame:NSMakeRect(0,0,self.dragFrame.size.width,self.dragFrame.size.height)];view.image=image;
        [self.dragPreview setFrame:self.dragFrame display:NO];self.dragPreview.contentView=view;
        self.dragHint=[NSTextField labelWithString:@""];self.dragHint.frame=NSMakeRect(10,0,204,20);self.dragHint.alignment=NSTextAlignmentCenter;self.dragHint.textColor=NSColor.whiteColor;
        [view addSubview:self.dragHint];[self.dragPreview orderFrontRegardless];button.alphaValue=0.25;
        return;
    }
    if(!self.dragging || ![self.dragID isEqual:identifier])return;
    self.dragPoint=point;NSInteger action=[self dragActionAt:point];
    NSRect frame=self.dragFrame;frame.origin.x+=point.x-self.dragStart.x;frame.origin.y+=point.y-self.dragStart.y;
    [self.dragPreview setFrame:frame display:YES];
    self.dragHint.stringValue=[self text:action==1?([self lockedTask:identifier]?@"taskUnlockBeforeDismiss":@"taskDropClose"):(action==2?@"taskDropReorder":@"taskDropOpen")];
    self.dragPreview.alphaValue=action==1?0.70:0.95;
    if(phase!=2)return;
    NSString *before=@"";
    if(action==2) {
        for(BotTaskButton *candidate in self.buttons){
            NSString *other=self.tasks[candidate.tag][@"id"];if([other isEqual:identifier])continue;
            NSRect candidateFrame=[candidate.window convertRectToScreen:[candidate convertRect:candidate.bounds toView:nil]];
            BOOL precedes=self.stackVertical?(self.stackUp?point.y<NSMidY(candidateFrame):point.y>NSMidY(candidateFrame)):point.x<NSMidX(candidateFrame);
            if(precedes){before=other;break;}
        }
    }
    [self cancelDrag];
    if(action==1)[self dismissGesture:identifier];
    else if(action==2){
        [self collapse];NSMutableArray *ordered=[self.tasks mutableCopy];NSDictionary *moving=nil;
        for(NSDictionary *task in ordered)if([task[@"id"] isEqual:identifier]){moving=task;break;}
        if(moving){[ordered removeObject:moving];NSUInteger index=ordered.count;for(NSUInteger i=0;i<ordered.count;i++)if([ordered[i][@"id"] isEqual:before]){index=i;break;}[ordered insertObject:moving atIndex:index];[self setTasks:ordered];}
        if(self.reorderTask)self.reorderTask(identifier,before);[self expand];
    }
    else {[self collapse];if(self.placeTask)self.placeTask(identifier,point);}
}
- (void)cancelOpen { if(self.cancelOpening)self.cancelOpening(); }
- (BOOL)activeStatus:(NSString *)status {
    return [@[@"pending",@"working",@"running",@"inProgress",@"starting",@"sending",@"interrupting"] containsObject:status ?: @""];
}
- (void)hover:(NSInteger)index entered:(BOOL)entered {
    [self updateStackCaption];
    if((!self.visible && !self.shortcutPeek) || self.dragging)return;
    [self.openTimer invalidate]; self.openTimer=nil;
    [self.closeTimer invalidate]; self.closeTimer=nil;
    NSUInteger generation=++self.hoverGeneration;
    __weak BotTaskDock *weak=self;
    if(entered) {
        if(index<0) self.openTimer=[NSTimer scheduledTimerWithTimeInterval:0.16 repeats:NO block:^(NSTimer *timer){[weak expand];}];
    } else {
        self.closeTimer=[NSTimer scheduledTimerWithTimeInterval:0.42 repeats:NO block:^(NSTimer *timer){
            if(generation!=weak.hoverGeneration || weak.shortcutHeld)return;
            if(NSPointInRect(NSEvent.mouseLocation,weak.window.frame))return;
            [weak collapse];
        }];
    }
}
- (void)toggle { if(self.expanded)[self collapse];else [self expand]; }
- (void)setShortcutHeld:(BOOL)held {
    _shortcutHeld=held;
    if(held){self.shortcutPeek=YES;[self expand];}
    else if(!self.dragging && !NSPointInRect(NSEvent.mouseLocation,self.window.frame))[self collapse];
}
- (void)expand {
    if(self.expanded || (!self.visible && !self.shortcutPeek) || !self.tasks.count)return;
    [self.openTimer invalidate]; self.openTimer=nil;
    self.expanded=YES; [self render]; [self layout];
    __weak BotTaskDock *weak=self;
    self.outsideMonitor=[NSEvent addGlobalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown|NSEventMaskRightMouseDown handler:^(NSEvent *event){[weak collapse];}];
    self.localMonitor=[NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown|NSEventMaskRightMouseDown|NSEventMaskKeyDown handler:^NSEvent *(NSEvent *event){
        if(event.type==NSEventTypeKeyDown && event.keyCode==53 && weak.dragging){[weak cancelDrag];return nil;}
        if((event.type==NSEventTypeKeyDown && event.keyCode==53) || (event.type!=NSEventTypeKeyDown && event.window!=weak.window)) [weak collapse];
        return event;
    }];
    if(self.gesture)self.gesture(@"attention");
}
- (void)collapse {
    if(self.dragging)return;
    [self.openTimer invalidate]; self.openTimer=nil; [self.closeTimer invalidate]; self.closeTimer=nil;
    ++self.hoverGeneration;
    self.expanded=NO; self.shortcutPeek=NO;
    if(self.outsideMonitor){[NSEvent removeMonitor:self.outsideMonitor];self.outsideMonitor=nil;}
    if(self.localMonitor){[NSEvent removeMonitor:self.localMonitor];self.localMonitor=nil;}
    if(self.pendingTasks) { _tasks=self.pendingTasks; self.pendingTasks=nil; }
    [self render]; [self layout];
}
- (void)open:(BotTaskButton *)button {
    NSEvent *event=NSApp.currentEvent;
    NSInteger count=(event.type==NSEventTypeLeftMouseUp || event.type==NSEventTypeLeftMouseDown)?event.clickCount:0;
    NSLog(@"Task terminal card click: count=%ld",(long)count);
    if((!self.visible && !self.shortcutPeek) || !button.enabled || button.tag<0 || [self buttonFor:button.identifier]!=button)return;
    NSString *identifier=button.identifier;
    // Keep cards reachable for a second click; this panel never activates the app.
    if(self.gesture)self.gesture(@"nod");
    if(self.openTask)self.openTask(identifier);
}
- (void)placeWithPet:(NSRect)pet bounds:(NSRect)bounds visible:(BOOL)visible {
    if(self.dragging && (!NSEqualRects(self.pet,pet) || !NSEqualRects(self.bounds,bounds)))[self cancelDrag];
    self.pet=pet; self.bounds=bounds; self.visible=visible;
    if(!visible && !self.shortcutPeek){[self cancelDrag];[self collapse];} else { [self updateActivity]; [self layout]; }
}
- (void)layout {
    if((!self.visible && !self.shortcutPeek) || !self.tasks.count) { [self.window orderOut:nil]; return; }
    BotTaskDockPlacement placement=taskDockPlacement(self.pet,self.bounds,self.tasks.count,self.expanded);
    if(self.stackVertical!=placement.vertical || self.stackUp!=placement.growsUp || !NSEqualSizes(self.window.frame.size,placement.frame.size))[self render];
    [self.window setFrame:placement.frame display:YES];
    if(!self.window.visible)[self.window orderFrontRegardless];
}
- (void)showFailure:(NSString *)message {
    if(self.notice)self.notice(message,NO);
}
- (void)setTransitions:(NSDictionary *)transitions {
    _transitions=[transitions copy];
    if(self.snapshotTask && transitions[self.snapshotTask]){[self.snapshotCapture cancel];self.snapshotTask=nil;}
    [self updateActivity];
}
- (void)setOpening:(NSString *)identifier message:(NSString *)message {
    [self.openingTimer invalidate];self.openingTimer=nil;
    self.opening=identifier;self.openingMessage=message;
    // Status changes must keep the actual mouse responder and hover target.
    // Rebuilding the cards here loses a subsequent mouse-down/up mid-transition.
    [self updateActivity];
    if(self.notice)self.notice(@"",YES);
    if(identifier.length && message.length) {
        __weak BotTaskDock *weak=self;
        self.openingTimer=[NSTimer scheduledTimerWithTimeInterval:0.65 repeats:NO block:^(NSTimer *timer){
            if(!weak.stopped && [weak.opening isEqual:identifier] && [weak.openingMessage isEqual:message] && weak.notice)weak.notice(message,YES);
        }];
    }
}
- (void)stop {
    [self.openingTimer invalidate];self.openingTimer=nil;
    [self cancelDrag];self.stopped=YES;[self.snapshotCapture cancel];self.snapshotTask=nil;[self.snapshots removeAllObjects];
    [NSWorkspace.sharedWorkspace.notificationCenter removeObserver:self];
    self.visible=NO; [self collapse]; [self.window close];
    self.openTask=nil; self.unpinTask=nil;self.lockTask=nil;self.reorderTask=nil;self.placeTask=nil; self.cancelOpening=nil; self.gesture=nil; self.notice=nil;
}
@end
