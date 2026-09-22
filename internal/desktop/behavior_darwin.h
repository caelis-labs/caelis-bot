// Included by native_darwin.m: one AppKit owner for observations and prop lifetime.
static NSDictionary *bot_rect(NSRect r) {
    return @{@"x":@(r.origin.x),@"y":@(r.origin.y),@"width":@(r.size.width),@"height":@(r.size.height)};
}
static void bot_event(NSWindow *window, NSString *name, id detail) {
    NSData *data=[NSJSONSerialization dataWithJSONObject:detail options:NSJSONWritingFragmentsAllowed error:nil];
    if (!data) return;
    NSString *json=[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
    bot_js(window,[NSString stringWithFormat:@"window.dispatchEvent(new CustomEvent('%@',{detail:%@}))",name,json]);
}
@implementation BotHost (Behavior)
- (NSDictionary *)desktopContext {
    NSScreen *screen=self.pet.screen ?: NSScreen.mainScreen;
    if (!screen) return nil;
    NSRect frame=screen.frame,work=screen.visibleFrame;
    NSString *edge=@"unknown"; id dock=NSNull.null;
    if (NSMinX(work)-NSMinX(frame)>4) {edge=@"left";dock=bot_rect(NSMakeRect(NSMinX(frame),NSMinY(frame),NSMinX(work)-NSMinX(frame),frame.size.height));}
    else if (NSMaxX(frame)-NSMaxX(work)>4) {edge=@"right";dock=bot_rect(NSMakeRect(NSMaxX(work),NSMinY(frame),NSMaxX(frame)-NSMaxX(work),frame.size.height));}
    else if (NSMinY(work)-NSMinY(frame)>4) {edge=@"bottom";dock=bot_rect(NSMakeRect(NSMinX(frame),NSMinY(frame),frame.size.width,NSMinY(work)-NSMinY(frame)));}
    // Window metadata only, at most once per second; no title, pixels or permission prompts.
    double now=NSProcessInfo.processInfo.systemUptime;
    if (!self.frontContext || now-self.frontSample>=1) {
        self.frontSample=now;
        NSRunningApplication *app=NSWorkspace.sharedWorkspace.frontmostApplication;
        id rect=NSNull.null,windowID=NSNull.null;
        NSArray *windows=CFBridgingRelease(CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly|kCGWindowListExcludeDesktopElements,kCGNullWindowID));
        for (NSDictionary *window in windows) {
            if ([window[(__bridge NSString *)kCGWindowOwnerPID] intValue]!=app.processIdentifier || [window[(__bridge NSString *)kCGWindowLayer] intValue]!=0) continue;
            CGRect bounds;
            if (CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)window[(__bridge NSString *)kCGWindowBounds],&bounds) && bounds.size.width>100 && bounds.size.height>100) {
                double top=NSScreen.screens.firstObject.frame.origin.y+NSScreen.screens.firstObject.frame.size.height;
                rect=bot_rect(NSMakeRect(bounds.origin.x,top-CGRectGetMaxY(bounds),bounds.size.width,bounds.size.height));
                windowID=window[(__bridge NSString *)kCGWindowNumber] ?: NSNull.null; break;
            }
        }
        self.frontContext=@{@"application":app.bundleIdentifier ?: @"",@"id":windowID,@"observedAtUptime":@(now),@"frame":rect,@"source":rect==NSNull.null?@"application-only":@"window-metadata"};
    }
    NSPoint pointer=NSEvent.mouseLocation;
    return @{@"desktop":@{@"id":[screen.deviceDescription[@"NSScreenNumber"] stringValue] ?: @"unknown",@"frame":bot_rect(frame),@"workArea":bot_rect(work),@"scale":@(screen.backingScaleFactor)},
      @"dock":@{@"source":@"visible-frame-inset",@"edge":edge,@"confidence":dock==NSNull.null?@"unknown":@"estimated",@"frame":dock},
      @"activeWindow":self.frontContext,@"actor":bot_rect(self.pet.frame),
      @"pointer":@{@"x":@(pointer.x),@"y":@(pointer.y),@"hovering":@(!self.pet.ignoresMouseEvents&&!self.dragging)},
      @"interaction":@{@"pressing":@(self.pressing),@"dragging":@(self.dragging),@"input":@(self.panel.visible || self.history.keyWindow || self.bubble.interactive || NSApp.keyWindow!=nil),@"menu":@(self.menuTracking)}};
}
- (void)publishContext {
    if (!self.visible || !self.handle) return;
    NSDictionary *snapshot=[self desktopContext];
    if (!snapshot || [snapshot isEqual:self.lastContext]) return;
    self.lastContext=snapshot;
    NSMutableDictionary *event=[snapshot mutableCopy];event[@"revision"]=@(++self.contextRevision);
    event[@"sampledAtUptime"]=@(NSProcessInfo.processInfo.systemUptime);
    bot_event(self.pet,@"pet-context",event);
}
- (void)finishPlane:(NSString *)identifier outcome:(NSString *)outcome {
    if (!self.flightID || ![self.flightID isEqualToString:identifier]) return;
    NSString *finished=self.flightID; self.flightID=nil;
    [self.flightTimeout invalidate];self.flightTimeout=nil;
    bot_event(self.prop,@"prop-stop",@{@"id":finished});[self.prop orderOut:nil];
    bot_event(self.pet,@"pet-prop-result",@{@"id":finished,@"outcome":outcome});
    [self trace:[@"plane-" stringByAppendingString:outcome]];
}
- (void)cancelPlane { if(self.flightID)[self finishPlane:self.flightID outcome:@"interrupted"]; }
- (void)previewNear:(NSMenuItem *)item {
    NSString *name=@[@"wave",@"think",@"ask"][item.tag];
    __weak BotHost *weak=self;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW,250*NSEC_PER_MSEC),dispatch_get_main_queue(),^{
        if(weak.visible)bot_event(weak.pet,@"pet-near-preview",name);
    });
}
- (void)previewFace:(NSMenuItem *)item {
    NSString *name=@[@"blink",@"happy",@"curious",@"soft_smile",@"focused",@"expectant"][item.tag];
    __weak BotHost *weak=self;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW,250*NSEC_PER_MSEC),dispatch_get_main_queue(),^{
        if(weak.visible)bot_event(weak.pet,@"pet-face-preview",name);
    });
}
- (void)previewBehavior:(NSMenuItem *)item {
    NSString *name=@[@"breathe",@"observe",@"weight_shift",@"plane_care",@"stretch",@"plane_play"][item.tag];
    // A development-only explicit trigger, using the exact normal renderer path.
    __weak BotHost *weak=self;
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW,250*NSEC_PER_MSEC),dispatch_get_main_queue(),^{
        if(weak.visible)bot_event(weak.pet,@"pet-preview",name);
    });
}
@end
char *bot_context(void *pointer) {
    BotHost *host=(__bridge BotHost *)pointer;
    NSMutableDictionary *context=[[host desktopContext] mutableCopy];
    context[@"revision"]=@(host.contextRevision);
    context[@"sampledAtUptime"]=@(NSProcessInfo.processInfo.systemUptime);
    NSData *data=[NSJSONSerialization dataWithJSONObject:context ?: NSNull.null options:NSJSONWritingFragmentsAllowed error:nil];
    return strdup(data ? [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding].UTF8String : "null");
}
void bot_plane_ready(void *pointer,int ready) {
    BotHost *host=(__bridge BotHost *)pointer;host.propReady=ready;
    if(!ready)[host cancelPlane];
}
int bot_launch_plane(void *pointer,char *identifier,double x,double y) {
    BotHost *host=(__bridge BotHost *)pointer;
    if(!host.visible || !host.propReady || host.flightID || host.dragging || host.pressing || host.menuTracking || host.panel.visible || host.history.keyWindow || host.bubble.interactive) return 0;
    NSRect work=(host.pet.screen ?: NSScreen.mainScreen).visibleFrame,actor=host.pet.frame;
    // The prop has a bounded non-key surface, independent of the actor's drag window.
    NSPoint release=NSMakePoint(actor.origin.x+x*host.scale,actor.origin.y+y*host.scale);
    double width=MIN(520*host.scale,work.size.width-16),height=MIN(360*host.scale,work.size.height-16);
    if(width<180 || height<160) return 0;
    double direction=release.x>NSMidX(work)?-1:1;
    NSRect area=NSMakeRect(release.x-(direction>0?width*.18:width*.82),release.y-height*.25,width,height);
    area.origin.x=MAX(NSMinX(work)+8,MIN(area.origin.x,NSMaxX(work)-width-8));
    area.origin.y=MAX(NSMinY(work)+8,MIN(area.origin.y,NSMaxY(work)-height-8));
    if(!NSPointInRect(release,NSInsetRect(area,20,20)))return 0;
    host.flightID=[NSString stringWithUTF8String:identifier];
    [host.prop setFrame:area display:YES];[host.prop orderFrontRegardless];
    NSDictionary *flightData=@{@"id":host.flightID,@"width":@(width),@"height":@(height),@"x":@(release.x-area.origin.x),@"y":@(release.y-area.origin.y),@"scale":@(host.scale),@"direction":@(direction),@"originX":@(area.origin.x),@"originY":@(area.origin.y)};
    bot_event(host.pet,@"pet-prop-flight",flightData);
    bot_event(host.prop,@"prop-flight",flightData);
    __weak BotHost *weak=host;NSString *flight=host.flightID;
    host.flightTimeout=[NSTimer timerWithTimeInterval:10 repeats:NO block:^(NSTimer *timer){[weak finishPlane:flight outcome:@"timeout"];}];
    [NSRunLoop.mainRunLoop addTimer:host.flightTimeout forMode:NSRunLoopCommonModes];
    [host trace:@"plane-launch"];return 1;
}
void bot_finish_plane(void *pointer,char *identifier,int completed) {
    [(__bridge BotHost *)pointer finishPlane:[NSString stringWithUTF8String:identifier] outcome:completed?@"completed":@"interrupted"];
}
