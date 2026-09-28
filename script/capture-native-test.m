#import <Cocoa/Cocoa.h>
#include <assert.h>
#import "../internal/desktop/capture_darwin.m"
static NSUInteger deliveries;
static NSString *deliveredID;
void desktopCaptureEvent(uintptr_t handle,int kind,char *text) {
    if(kind==21){deliveries++;deliveredID=[NSString stringWithUTF8String:text];}
}
static BotCaptureDocument *fixtureDocument(void) {
    NSImage *image=[[NSImage alloc] initWithSize:NSMakeSize(1280,800)];
    [image lockFocusFlipped:YES];
    [[NSColor colorWithCalibratedWhite:0.94 alpha:1] setFill];NSRectFill(NSMakeRect(0,0,1280,800));
    [NSColor.whiteColor setFill];NSRectFill(NSMakeRect(230,100,930,620));
    [@"Discussion · Synthetic screen input" drawAtPoint:NSMakePoint(270,135) withAttributes:@{NSFontAttributeName:[NSFont systemFontOfSize:25 weight:NSFontWeightSemibold],NSForegroundColorAttributeName:NSColor.blackColor}];
    [@"A place to ask questions and share what you learn." drawAtPoint:NSMakePoint(270,185) withAttributes:@{NSFontAttributeName:[NSFont systemFontOfSize:16],NSForegroundColorAttributeName:NSColor.darkGrayColor}];
    [@"I used to translate every paragraph manually.\nNow I select the part I want to understand\nand ask my assistant for help." drawInRect:NSMakeRect(360,275,550,120) withAttributes:@{NSFontAttributeName:[NSFont systemFontOfSize:23],NSForegroundColorAttributeName:NSColor.blackColor}];
    [@"synthetic@example.test" drawAtPoint:NSMakePoint(610,422) withAttributes:@{NSFontAttributeName:[NSFont systemFontOfSize:15],NSForegroundColorAttributeName:NSColor.blackColor}];
    [image unlockFocus];
    BotCaptureDocument *doc=[BotCaptureDocument new];doc.image=image;doc.canvasSize=image.size;doc.pixelScale=2;
    doc.applicationName=@"Example browser";doc.windowTitle=@"Synthetic discussion";doc.selection=NSMakeRect(340,250,600,230);
    [doc addMark:@{@"kind":@"arrow",@"a":[NSValue valueWithPoint:NSMakePoint(380,400)],@"b":[NSValue valueWithPoint:NSMakePoint(480,360)],@"color":NSColor.systemRedColor,@"width":@3}];
    [doc addMark:@{@"kind":@"redact",@"a":[NSValue valueWithPoint:NSMakePoint(600,410)],@"b":[NSValue valueWithPoint:NSMakePoint(820,445)],@"color":NSColor.blackColor,@"width":@3}];
    [doc addMark:@{@"kind":@"mosaic",@"a":[NSValue valueWithPoint:NSMakePoint(600,410)],@"b":[NSValue valueWithPoint:NSMakePoint(820,445)],@"color":NSColor.blackColor,@"width":@3}];
    return doc;
}
static NSEvent *testKey(NSString *characters,unsigned short code,NSEventModifierFlags flags,NSWindow *window) {
    return [NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint modifierFlags:flags timestamp:0 windowNumber:window.windowNumber context:nil characters:characters charactersIgnoringModifiers:characters isARepeat:NO keyCode:code];
}
static void checkInteraction(NSString *root) {
    NSUInteger before=deliveries;
    BotCapture *owner=[BotCapture new];owner.root=root;owner.handle=1;
    [owner setAvailability:@{@"state":@"supported"}];
    owner.overlay=capturePanel(NSMakeRect(100,100,1280,800),YES);
    owner.canvas=[[BotCaptureCanvas alloc] initWithFrame:NSMakeRect(0,0,1280,800)];
    BotCaptureCanvas *canvas=owner.canvas;canvas.owner=owner;canvas.document=fixtureDocument();
    owner.overlay.contentView=canvas;[canvas buildToolbar];[owner.overlay makeKeyAndOrderFront:nil];
    assert(canvas.backgroundButton.state==NSControlStateValueOn);
    canvas.backgroundButton.state=NSControlStateValueOff;[canvas changeBackground:canvas.backgroundButton];
    assert(!canvas.document.includeBackground&&owner.includeBackground);
    assert(canvas.scopeLabel.stringValue.length&&canvas.askButton.title.length);
    canvas.backgroundButton.state=NSControlStateValueOn;[canvas changeBackground:canvas.backgroundButton];
    assert(canvas.document.includeBackground);
    canvas.document.identifier=@"receipt-locked";canvas.document.outcome=@"unknown";[canvas updateAvailability];
    assert(!canvas.backgroundButton.enabled&&!canvas.askButton.enabled);
    canvas.backgroundButton.state=NSControlStateValueOff;[canvas changeBackground:canvas.backgroundButton];
    assert(canvas.document.includeBackground&&canvas.backgroundButton.state==NSControlStateValueOn);
    canvas.restored=YES;[canvas updateAvailability];
    assert(canvas.backgroundButton.hidden&&[canvas.scopeLabel.stringValue isEqual:@"Full screen included"]);
    canvas.restored=NO;
    canvas.document.identifier=nil;canvas.document.outcome=nil;canvas.document.source=@"clipboard";canvas.document.includeBackground=NO;[canvas updateAvailability];
    assert(canvas.backgroundButton.hidden);
    canvas.backgroundButton.state=NSControlStateValueOn;[canvas changeBackground:canvas.backgroundButton];
    assert(!canvas.document.includeBackground);
    canvas.document.source=@"screen";[canvas updateAvailability];
    [canvas focusNote];
    BotCaptureFieldEditor *editor=(BotCaptureFieldEditor *)canvas.noteEditor.currentEditor;
    assert([editor isKindOfClass:BotCaptureFieldEditor.class]);
    canvas.noteEditor.stringValue=@"Translate this";
    editor.interpretingMarkedText=YES;
    [canvas control:canvas.noteEditor textView:editor doCommandBySelector:@selector(insertNewline:)];
    assert(deliveries==before&&owner.overlay);
    editor.interpretingMarkedText=NO;
    // Selected text owns Cmd+C even though the image is ready.
    [editor setString:@"Translate this"];[editor setSelectedRange:NSMakeRange(0,9)];
    assert([owner.overlay performKeyEquivalent:testKey(@"c",8,NSEventModifierFlagCommand,owner.overlay)]);
    assert([[NSPasteboard.generalPasteboard stringForType:NSPasteboardTypeString] isEqual:@"Translate"]&&owner.overlay);
    // Annotation Return commits one mark, returns to the note and does not send.
    canvas.textEditor=[[NSTextField alloc] initWithFrame:NSMakeRect(350,260,180,30)];
    canvas.textEditor.delegate=canvas;canvas.textEditor.stringValue=@"Annotation";[canvas addSubview:canvas.textEditor];
    [owner.overlay makeFirstResponder:canvas.textEditor];NSUInteger count=canvas.document.marks.count;
    [canvas control:canvas.textEditor textView:(NSTextView *)canvas.textEditor.currentEditor doCommandBySelector:@selector(insertNewline:)];
    assert(!canvas.textEditor&&canvas.document.marks.count==count+1&&canvas.noteEditor.currentEditor&&deliveries==before);
    // Continuous values reach the actual annotation width and visible preview.
    [canvas changeWidth:canvas.widthButton];NSSlider *slider=nil;
    for(NSView *view in canvas.widthPopover.contentViewController.view.subviews)if([view isKindOfClass:NSSlider.class])slider=(NSSlider *)view;
    assert(slider&&slider.continuous&&slider.numberOfTickMarks==0);slider.doubleValue=8.4;[canvas adjustWidth:slider];
    assert(fabs(canvas.stroke-8.4)<0.01&&[canvas.widthValue.stringValue containsString:@"8.4"]);
    [canvas.widthPopover close];[canvas focusNote];
    [owner setAvailability:@{@"state":@"unsupported"}];
    [canvas control:canvas.noteEditor textView:(NSTextView *)canvas.noteEditor.currentEditor doCommandBySelector:@selector(insertNewline:)];
    assert(deliveries==before&&owner.overlay);
    [owner setAvailability:@{@"state":@"supported"}];
    [canvas control:canvas.noteEditor textView:(NSTextView *)canvas.noteEditor.currentEditor doCommandBySelector:@selector(insertNewline:)];
    assert(deliveries==before+1&&!owner.overlay&&owner.pins.count==1);
    [owner stop];
    // Repeat capture cancels both preparing and selected states without sending.
    owner=[BotCapture new];owner.preparing=YES;[owner capture];assert(!owner.preparing&&!owner.overlay);
    owner.overlay=capturePanel(NSMakeRect(100,100,800,600),YES);[owner capture];assert(!owner.overlay&&deliveries==before+1);
    [owner stop];deliveries=before;
}
static void checkRedaction(NSBitmapImageRep *rep,NSInteger x,NSInteger y) {
    NSColor *color=[[rep colorAtX:x y:y] colorUsingColorSpace:NSColorSpace.deviceRGBColorSpace];
    if(color.redComponent>0.12||color.greenComponent>0.12||color.blueComponent>0.12){
        fprintf(stderr,"redaction mismatch at %ld,%ld: %.2f %.2f %.2f\n",(long)x,(long)y,color.redComponent,color.greenComponent,color.blueComponent);abort();
    }
}
int main(int argc,char **argv) {
    @autoreleasepool {
        [NSApplication sharedApplication];[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        BOOL live=argc>1&&strcmp(argv[1],"--live")==0;
        NSString *root=[NSTemporaryDirectory() stringByAppendingPathComponent:NSUUID.UUID.UUIDString];
        BotCapture *owner=[BotCapture new];owner.handle=1;owner.root=root;
        [owner setAvailability:@{@"state":@"supported"}];
        BotCaptureDocument *doc=fixtureDocument();
        NSBitmapImageRep *crop=[doc render:NO maximumEdge:0],*background=[doc render:YES maximumEdge:2400];
        assert(crop.pixelsWide==1200&&crop.pixelsHigh==460);
        checkRedaction(crop,600,340);
        checkRedaction(background,1200,788);
        NSError *error=nil;NSDictionary *metadata=[doc writeToRoot:root error:&error];
        assert(metadata&&[metadata[@"background"] boolValue]);
        assert([metadata[@"selection"][@"x"] integerValue]==637);
        assert([metadata[@"selection"][@"width"] integerValue]==1126);
        doc.identifier=nil;
        NSUInteger count=doc.marks.count;[doc undo];assert(doc.marks.count==count-1);[doc redo];assert(doc.marks.count==count);
        NSRect visible=NSScreen.mainScreen.visibleFrame;
        NSRect frame=NSMakeRect(NSMidX(visible)-640,NSMidY(visible)-400,1280,800);
        owner.overlay=capturePanel(frame,YES);owner.overlay.level=NSFloatingWindowLevel;
        owner.canvas=[[BotCaptureCanvas alloc] initWithFrame:NSMakeRect(0,0,1280,800)];
        owner.canvas.owner=owner;owner.canvas.document=doc;
        owner.overlay.contentView=owner.canvas;[owner.canvas buildToolbar];
        [owner.overlay makeKeyAndOrderFront:nil];[owner.overlay makeFirstResponder:owner.canvas];[NSApp activateIgnoringOtherApps:YES];
        [owner setAvailability:@{@"state":@"unsupported",@"message":@"Ask Bot unavailable: text-only model"}];assert(!owner.canvas.askButton.enabled);
        [owner ask:owner.canvas];assert(deliveries==0);
        [owner setAvailability:@{@"state":@"supported"}];assert(owner.canvas.askButton.enabled);
        if(live){[NSApp run];return 0;}
        checkInteraction(root);
        [owner.overlay makeKeyAndOrderFront:nil];
        [owner.canvas displayIfNeeded];
        NSBitmapImageRep *preview=[owner.canvas bitmapImageRepForCachingDisplayInRect:owner.canvas.bounds];
        [owner.canvas cacheDisplayInRect:owner.canvas.bounds toBitmapImageRep:preview];
        if(argc>1)[[preview representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:[NSString stringWithUTF8String:argv[1]] atomically:YES];
        [owner.canvas pin];assert(owner.pins.count==1 && !owner.overlay);
        BotCaptureCanvas *pin=(BotCaptureCanvas *)owner.pins.firstObject.contentView;
        assert(pin.document==doc && NSWidth(pin.bounds)/NSHeight(pin.bounds)>2.6);
        [pin displayIfNeeded];
        NSBitmapImageRep *pinPreview=[pin bitmapImageRepForCachingDisplayInRect:pin.bounds];
        [pin cacheDisplayInRect:pin.bounds toBitmapImageRep:pinPreview];
        checkRedaction(pinPreview,(NSInteger)(pinPreview.pixelsWide*0.5),(NSInteger)(pinPreview.pixelsHigh*0.74));
        [owner ask:pin];assert(deliveries==1 && deliveredID.length);
        [owner ask:pin];assert(deliveries==1);
        [owner receipt:@{@"id":doc.identifier,@"outcome":@"unknown",@"message":@"Unknown"}];assert(owner.pins.count==1);
        [owner receipt:@{@"id":doc.identifier,@"outcome":@"accepted",@"message":@"Accepted"}];assert(owner.pins.count==0);
        // UTF-8 note limits must reject before allocating an ID or closing the editor.
        for(NSString *note in @[[ @"" stringByPaddingToLength:4097 withString:@"a" startingAtIndex:0],[@"" stringByPaddingToLength:1366 withString:@"汉" startingAtIndex:0]]) {
            BotCaptureDocument *invalid=fixtureDocument();[owner pinDocument:invalid at:frame.origin];
            BotCaptureCanvas *view=(BotCaptureCanvas *)owner.pins.lastObject.contentView;
            [view buildToolbar];view.noteEditor.stringValue=note;
            [view askAction:nil];
            assert(deliveries==1&&!invalid.identifier.length&&view.noteEditor.editable&&[invalid.outcome isEqual:@"draft"]);
            assert([view.statusLabel.stringValue containsString:@"4096"]);
            assert([invalid.note isEqual:note]);
            error=nil;assert(![invalid writeToRoot:root error:&error]&&error&&!invalid.identifier.length);
            [view destroyPin:nil];
        }
        // Boundary notes survive unchanged; optional OS text truncates at UTF-8 boundaries.
        for(NSString *note in @[[ @"" stringByPaddingToLength:4096 withString:@"a" startingAtIndex:0],[@"" stringByPaddingToLength:1365 withString:@"汉" startingAtIndex:0]]) {
            BotCaptureDocument *bounded=fixtureDocument();bounded.note=note;
            bounded.applicationName=[@"" stringByPaddingToLength:1000 withString:@"汉" startingAtIndex:0];
            bounded.windowTitle=[@"" stringByPaddingToLength:1000 withString:@"汉" startingAtIndex:0];
            NSDictionary *saved=[bounded writeToRoot:root error:&error];assert(saved&&[saved[@"note"] isEqual:note]);
            assert([saved[@"application"] lengthOfBytesUsingEncoding:NSUTF8StringEncoding]==510);
            assert([saved[@"windowTitle"] lengthOfBytesUsingEncoding:NSUTF8StringEncoding]==2046);
        }
        // A proven pre-dispatch failure unlocks the same in-memory selection and note.
        BotCaptureDocument *retry=fixtureDocument();retry.note=@"Please translate this";
        [owner pinDocument:retry at:frame.origin];BotCaptureCanvas *retryPin=(BotCaptureCanvas *)owner.pins.lastObject.contentView;
        [owner ask:retryPin];assert(deliveries==2&&!retryPin.noteEditor.editable);
        NSString *failedID=retry.identifier;
        [owner receipt:@{@"id":failedID,@"outcome":@"draft",@"message":@"Prepare again"}];
        assert(!retry.identifier.length&&retryPin.noteEditor.editable);
        assert([retryPin.noteEditor.stringValue isEqual:@"Please translate this"]&&retry.marks.count==3);
        retryPin.noteEditor.stringValue=@"Translate the selected paragraph";[retryPin askAction:nil];
        assert(deliveries==3&&![retry.identifier isEqual:failedID]);
        assert([retry.note isEqual:@"Translate the selected paragraph"]);
        [owner receipt:@{@"id":retry.identifier,@"outcome":@"accepted",@"message":@"Accepted"}];
        // Failed persistence must leave editing available and never deliver.
        [@"blocked" writeToFile:[root stringByAppendingPathComponent:@"blocked"] atomically:YES encoding:NSUTF8StringEncoding error:nil];
        owner.root=[root stringByAppendingPathComponent:@"blocked"];
        BotCaptureDocument *unsaved=fixtureDocument();[owner pinDocument:unsaved at:frame.origin];
        BotCaptureCanvas *unsavedPin=(BotCaptureCanvas *)owner.pins.lastObject.contentView;
        [owner ask:unsavedPin];assert(deliveries==3&&!unsaved.identifier.length&&[unsaved.outcome isEqual:@"draft"]);
        [owner stop];[[NSFileManager defaultManager] removeItemAtPath:root error:nil];
        puts("CAPTURE NATIVE PASS: crop/context redaction, pixels, pin viewport, capability gate and receipt lifecycle");
    }
    return 0;
}
