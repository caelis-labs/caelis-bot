// Interactive sandbox using the same native task cards as the product. All
// task and terminal data are synthetic; no runtime, shell or network is used.
#import <Cocoa/Cocoa.h>
#include "task_snapshot_darwin.m"
#include "task_dock_darwin.m"

@interface TaskDockPreview : NSObject <NSWindowDelegate>
@property BotTaskDock *dock;
@property NSWindow *controls;
@property NSMutableArray<NSMutableDictionary *> *history;
@property NSMutableDictionary<NSString *,NSWindow *> *windows;
@property NSInteger sequence;
@property BOOL manualOrder;
@property NSInteger placementMode;
@property id shortcut;
@end
@implementation TaskDockPreview
- (NSMutableDictionary *)task:(NSString *)identifier {
    for(NSMutableDictionary *task in self.history)if([task[@"id"] isEqual:identifier])return task;
    return nil;
}
- (void)refresh {
    NSMutableArray *visible=[NSMutableArray new];
    for(NSDictionary *task in self.history)if(![task[@"hidden"] boolValue])[visible addObject:[task copy]];
    [visible sortUsingComparator:^NSComparisonResult(NSDictionary *a,NSDictionary *b){
        BOOL activeA=[a[@"status"] isEqual:@"working"],activeB=[b[@"status"] isEqual:@"working"];
        if(!self.manualOrder && activeA!=activeB)return activeA?NSOrderedAscending:NSOrderedDescending;
        return [b[@"order"] compare:a[@"order"]];
    }];
    [self.dock setTasks:visible];
}
- (void)add {
    self.sequence++;
    NSArray *prompts=@[@"检查审批恢复链路，覆盖拒绝回执与重启场景。",@"核对工作区及产物，整理本次验证报告。",@"实现终端窗口收起与恢复，保持任务继续运行。",@"对比两组性能数据，整理下一步优化建议。"];
    NSMutableDictionary *task=[@{@"id":[NSString stringWithFormat:@"preview-%ld",(long)self.sequence],@"terminal":@"terminal",@"prompt":prompts[(self.sequence-1)%prompts.count],@"status":@"working",@"order":@(self.sequence),@"locked":@NO,@"hidden":@NO} mutableCopy];
    [self.history addObject:task];
    [self refresh];[self.dock expand];
}
- (void)complete {
    for(NSMutableDictionary *task in self.history.reverseObjectEnumerator)if([task[@"status"] isEqual:@"working"] && ![task[@"hidden"] boolValue]){task[@"status"]=@"completed";break;}
    [self refresh];[self.dock expand];
}
- (void)age {
    for(NSMutableDictionary *task in self.history)if([task[@"status"] isEqual:@"completed"] && ![task[@"locked"] boolValue])task[@"hidden"]=@YES;
    [self.dock collapse];[self refresh];[self.dock expand];
}
- (void)resume {
    for(NSMutableDictionary *task in self.history)if([task[@"hidden"] boolValue] || [task[@"status"] isEqual:@"completed"]){task[@"status"]=@"working";task[@"hidden"]=@NO;task[@"order"]=@(++self.sequence);[self refresh];[self.dock expand];return;}
    [self add];
}
- (void)reveal { [self.dock toggle]; }
- (void)reset {
    for(NSWindow *window in self.windows.allValues)[window close];[self.windows removeAllObjects];
    [self.dock collapse];[self.dock.snapshots removeAllObjects];[self.history removeAllObjects];self.sequence=0;self.manualOrder=NO;
    [self add];[self add];[self add];
    self.history[0][@"locked"]=@YES;self.history[1][@"status"]=@"completed";
    [self.dock collapse];[self refresh];[self.dock expand];
}
- (void)toggleTerminal:(NSString *)identifier {
    NSWindow *window=self.windows[identifier];
    if(window && window.visible && !window.miniaturized && window.keyWindow){[window miniaturize:nil];return;}
    if(window && (window.visible || window.miniaturized)){[window deminiaturize:nil];[window makeKeyAndOrderFront:nil];return;}
    NSDictionary *task=[self task:identifier];
    window=[[NSWindow alloc] initWithContentRect:NSMakeRect(150+self.windows.count*36,310+self.windows.count*22,680,380) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskMiniaturizable|NSWindowStyleMaskResizable backing:NSBackingStoreBuffered defer:NO];
    window.title=@"交互草稿 · 模拟终端";window.releasedWhenClosed=NO;
    window.backgroundColor=[NSColor colorWithCalibratedRed:0.065 green:0.075 blue:0.09 alpha:1];
    NSTextField *text=[NSTextField wrappingLabelWithString:[NSString stringWithFormat:@">_ %@  /  worker\n\n%@\n\n• 检查上下文与工作区\n• 正在收集验证证据…\n\n  This is a simulated terminal.\n  No commands or real workers are running.\n\n再次点击对应卡片可以收起；关闭此窗口后可以重新打开。",@"Terminal",task[@"prompt"]]];
    text.font=[NSFont monospacedSystemFontOfSize:15 weight:NSFontWeightRegular];text.textColor=[NSColor colorWithCalibratedRed:0.7 green:0.84 blue:0.79 alpha:1];text.frame=NSMakeRect(28,24,624,326);text.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;
    [window.contentView addSubview:text];self.windows[identifier]=window;[window makeKeyAndOrderFront:nil];
    // Our own synthetic view can be rendered directly, without screen capture.
    [window.contentView display];
    NSBitmapImageRep *rep=[window.contentView bitmapImageRepForCachingDisplayInRect:window.contentView.bounds];
    [window.contentView cacheDisplayInRect:window.contentView.bounds toBitmapImageRep:rep];
    NSImage *image=[[NSImage alloc] initWithSize:window.contentView.bounds.size];[image addRepresentation:rep];
    [self.dock.snapshots setObject:@{@"image":image,@"time":@"示例快照 · 刚刚"} forKey:identifier];
    [self.dock render];
}
- (void)changePlacement:(NSPopUpButton *)sender {
    self.placementMode=sender.indexOfSelectedItem;[self.dock collapse];[self place];[self.dock expand];
}
- (void)place {
    NSRect frame=self.controls.frame,bounds=self.controls.screen.visibleFrame;
    NSRect pet=NSMakeRect(NSMidX(frame)-120,NSMinY(frame)-25,240,240);
    if(self.placementMode>0) {
        BOOL right=self.placementMode==2 || self.placementMode==4;
        BOOL top=self.placementMode==3 || self.placementMode==4;
        pet.origin=NSMakePoint(right?NSMaxX(bounds)-240:NSMinX(bounds),top?NSMaxY(bounds)-240:NSMinY(bounds));
    }
    [self.dock placeWithPet:pet bounds:bounds visible:YES];
}
- (void)windowDidMove:(NSNotification *)notification { [self place]; }
- (void)windowWillClose:(NSNotification *)notification { if(notification.object==self.controls)[NSApp terminate:nil]; }
- (void)start {
    self.history=[NSMutableArray new];self.windows=[NSMutableDictionary new];self.dock=[BotTaskDock new];
    NSString *language=[NSBundle.mainBundle pathForResource:@"native" ofType:@"json"];
    self.dock.language=[NSJSONSerialization JSONObjectWithData:[NSData dataWithContentsOfFile:language] options:0 error:nil];
    __weak TaskDockPreview *weak=self;
    self.dock.openTask=^(NSString *identifier){[weak toggleTerminal:identifier];};
    self.dock.unpinTask=^(NSString *identifier){[weak.windows[identifier] close];[weak.windows removeObjectForKey:identifier];[weak task:identifier][@"hidden"]=@YES;[weak task:identifier][@"locked"]=@NO;[weak refresh];[weak.dock expand];};
    self.dock.lockTask=^(NSString *identifier,BOOL locked){[weak task:identifier][@"locked"]=@(locked);[weak refresh];};
    self.dock.reorderTask=^(NSString *identifier,NSString *before){
        NSMutableArray *ordered=[NSMutableArray new];
        NSArray *current=weak.dock.pendingTasks ?: weak.dock.tasks;
        for(NSDictionary *task in current)if(![task[@"id"] isEqual:identifier])[ordered addObject:task[@"id"]];
        NSUInteger index=[ordered indexOfObject:before];if(index==NSNotFound)index=ordered.count;
        [ordered insertObject:identifier atIndex:index];weak.manualOrder=YES;
        for(NSString *key in ordered.reverseObjectEnumerator)[weak task:key][@"order"]=@(++weak.sequence);
        [weak refresh];
    };
    self.dock.placeTask=^(NSString *identifier,NSPoint point){
        NSWindow *window=weak.windows[identifier];
        if(!window || (!window.visible&&!window.miniaturized)){[weak toggleTerminal:identifier];window=weak.windows[identifier];}
        [window deminiaturize:nil];[window makeKeyAndOrderFront:nil];
        NSScreen *target=NSScreen.mainScreen;for(NSScreen *screen in NSScreen.screens)if(NSPointInRect(point,screen.frame)){target=screen;break;}
        NSRect frame=target.visibleFrame;[window setFrameTopLeftPoint:NSMakePoint(MAX(NSMinX(frame),MIN(point.x-window.frame.size.width/2,NSMaxX(frame)-window.frame.size.width)),MAX(NSMinY(frame)+window.frame.size.height,MIN(point.y,NSMaxY(frame))))];
    };
    self.controls=[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,740,172) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable backing:NSBackingStoreBuffered defer:NO];
    self.controls.title=@"Caelis Bot · 后台任务交互草稿";self.controls.delegate=self;self.controls.releasedWhenClosed=NO;
    NSTextField *intro=[NSTextField wrappingLabelWithString:@"点击打开／收起；悬停显示锁与关闭。上拖／双指上滑关闭终端，后台任务保留。\n顺着堆叠方向拖动排序；拖出顶部关闭，拖出侧面在桌面打开。空间不足加深重叠，悬停逐张展开。"];
    intro.frame=NSMakeRect(22,96,696,54);intro.font=[NSFont systemFontOfSize:13];[self.controls.contentView addSubview:intro];
    NSArray *titles=@[@"展开 / 收起",@"新派任务",@"完成一个",@"过去 30 分钟",@"恢复任务",@"重置"];
    SEL actions[]={@selector(reveal),@selector(add),@selector(complete),@selector(age),@selector(resume),@selector(reset)};
    for(NSUInteger i=0;i<titles.count;i++){
        NSButton *button=[NSButton buttonWithTitle:titles[i] target:self action:actions[i]];button.bezelStyle=NSBezelStyleRounded;button.frame=NSMakeRect(16+i*119,49,112,32);[self.controls.contentView addSubview:button];
    }
    NSTextField *hint=[NSTextField labelWithString:@"草稿内按住 ⌃⇧T 预览 · 关闭此控制窗口退出 · 快照不会上传或传给模型"];
    hint.frame=NSMakeRect(22,17,520,19);hint.font=[NSFont systemFontOfSize:11];hint.textColor=NSColor.secondaryLabelColor;[self.controls.contentView addSubview:hint];
    NSPopUpButton *placement=[[NSPopUpButton alloc] initWithFrame:NSMakeRect(560,12,158,27) pullsDown:NO];
    [placement addItemsWithTitles:@[@"位置：跟随草稿",@"位置：左下角",@"位置：右下角",@"位置：左上角",@"位置：右上角"]];placement.target=self;placement.action=@selector(changePlacement:);[self.controls.contentView addSubview:placement];
    self.placementMode=[NSProcessInfo.processInfo.environment[@"BOT_TASK_PREVIEW_PLACEMENT"] integerValue];[placement selectItemAtIndex:MIN(4,self.placementMode)];
    [self.controls center];[self.controls makeKeyAndOrderFront:nil];[self place];[self reset];
    NSUInteger count=[NSProcessInfo.processInfo.environment[@"BOT_TASK_PREVIEW_TASKS"] integerValue];
    for(NSUInteger i=3;i<count;i++)[self add];
    NSString *capture=NSProcessInfo.processInfo.environment[@"BOT_TASK_PREVIEW_CAPTURE"];
    if(capture.length){
        [self.dock.window.contentView display];
        NSBitmapImageRep *rep=[self.dock.window.contentView bitmapImageRepForCachingDisplayInRect:self.dock.window.contentView.bounds];
        [self.dock.window.contentView cacheDisplayInRect:self.dock.window.contentView.bounds toBitmapImageRep:rep];
        [[rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:capture atomically:YES];
    }
    self.shortcut=[NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskKeyDown|NSEventMaskKeyUp|NSEventMaskFlagsChanged handler:^NSEvent *(NSEvent *event){
        BOOL modifiers=(event.modifierFlags & (NSEventModifierFlagControl|NSEventModifierFlagShift))==(NSEventModifierFlagControl|NSEventModifierFlagShift);
        if(event.keyCode==17 && modifiers && event.type==NSEventTypeKeyDown){[weak.dock setShortcutHeld:YES];return nil;}
        if((event.keyCode==17 && event.type==NSEventTypeKeyUp) || (event.type==NSEventTypeFlagsChanged && !modifiers))[weak.dock setShortcutHeld:NO];
        return event;
    }];
}
@end
int main(void){@autoreleasepool{
    [NSApplication sharedApplication];[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
    NSMenu *menu=[NSMenu new];NSMenuItem *item=[NSMenuItem new];NSMenu *app=[NSMenu new];
    [app addItemWithTitle:@"退出交互草稿" action:@selector(terminate:) keyEquivalent:@"q"];item.submenu=app;[menu addItem:item];NSApp.mainMenu=menu;
    TaskDockPreview *preview=[TaskDockPreview new];[preview start];
    if(!NSProcessInfo.processInfo.environment[@"BOT_TASK_PREVIEW_CAPTURE"].length){[NSApp activateIgnoringOtherApps:YES];[NSApp run];}
    [preview.dock stop];
}}
