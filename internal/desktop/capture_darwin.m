//go:build darwin && cgo
#import "capture_darwin.h"
#import "capture_image_darwin.h"
#import "permission_capture_darwin.h"
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <UniformTypeIdentifiers/UniformTypeIdentifiers.h>
#include <math.h>
extern void desktopCaptureEvent(uintptr_t handle,int kind,char *text);

// CALayer stores resolved CGColors. Re-resolve semantic colors in the actual
// window appearance when a toolbar is attached or the system theme changes.
@interface BotCaptureSurface : NSView
@property NSColor *fillColor;
@property NSColor *strokeColor;
- (void)refreshColors;
@end
@implementation BotCaptureSurface
- (void)refreshColors {
    [self.effectiveAppearance performAsCurrentDrawingAppearance:^{
        self.layer.backgroundColor=self.fillColor.CGColor;
        self.layer.borderColor=self.strokeColor.CGColor;
    }];
}
- (void)viewDidMoveToWindow { [super viewDidMoveToWindow];[self refreshColors]; }
- (void)viewDidChangeEffectiveAppearance { [super viewDidChangeEffectiveAppearance];[self refreshColors]; }
@end

@interface BotCaptureFieldEditor : NSTextView
@property BOOL interpretingMarkedText;
@end
@implementation BotCaptureFieldEditor
- (void)keyDown:(NSEvent *)event {
    self.interpretingMarkedText=self.hasMarkedText;
    [super keyDown:event];
    self.interpretingMarkedText=NO;
}
@end
static BOOL composing(NSTextView *editor) {
    return editor.hasMarkedText || ([editor isKindOfClass:BotCaptureFieldEditor.class] && ((BotCaptureFieldEditor *)editor).interpretingMarkedText);
}
@interface BotStrokePreview : NSView
@property CGFloat stroke;
@property NSColor *ink;
@end
@implementation BotStrokePreview
- (void)drawRect:(NSRect)dirtyRect {
    [self.ink setStroke];NSBezierPath *path=[NSBezierPath new];
    path.lineWidth=self.stroke;path.lineCapStyle=NSLineCapStyleRound;
    [path moveToPoint:NSMakePoint(12,NSMidY(self.bounds))];
    [path lineToPoint:NSMakePoint(NSMaxX(self.bounds)-12,NSMidY(self.bounds))];[path stroke];
}
@end


@class BotCaptureCanvas;
@interface BotCapturePanel : NSPanel <NSWindowDelegate>
@property(weak) BotCaptureCanvas *captureCanvas;
@property BotCaptureFieldEditor *captureEditor;
@end


@interface BotCapture ()
@property NSMutableArray<BotCapturePanel *> *pins;
@property BotCapturePanel *overlay;
@property BotCaptureCanvas *canvas;
@property NSRunningApplication *previousApp;
@property NSUInteger generation;
@property BOOL preparing;
@property BOOL stopped;
@property(nonatomic) NSDictionary *availability;
@property NSTimer *refreshTimer;
@property NSMutableArray *observers;
@property id keyMonitor;
- (NSString *)text:(NSString *)key fallback:(NSString *)fallback;
- (void)finish:(BOOL)restoreFocus;
- (void)pinDocument:(BotCaptureDocument *)document at:(NSPoint)point;
- (void)ask:(BotCaptureCanvas *)canvas;
- (void)refresh;
@end

@interface BotCaptureCanvas : NSView <NSTextFieldDelegate>
@property(weak) BotCapture *owner;
@property BotCaptureDocument *document;
@property BOOL pinned;
@property BOOL restored;
@property BOOL editing;
@property NSString *tool;
@property NSColor *ink;
@property CGFloat stroke;
@property NSPoint start;
@property NSPoint last;
@property NSRect originalSelection;
@property NSInteger resizeEdges;
@property BOOL selecting;
@property BOOL moving;
@property NSDictionary *previewMark;
@property NSView *toolbar;
@property BotCapturePanel *toolbarPanel;
@property NSTextField *statusLabel;
@property NSTextField *textEditor;
@property NSTextField *noteEditor;
@property NSButton *askButton;
@property NSButton *backgroundButton;
@property NSTextField *scopeLabel;
@property NSMutableArray<NSButton *> *toolButtons;
@property NSButton *widthButton;
@property NSPopover *widthPopover;
@property NSTextField *widthValue;
@property BotStrokePreview *widthPreview;
@property BOOL adjustingSelection;
@property BOOL gestureActive;
@property NSRect hoverRect;
@property NSTrackingArea *tracking;
- (void)focusNote;
- (void)closeAction:(id)sender;
- (void)undoAction:(id)sender;
- (void)redoAction:(id)sender;
- (void)buildToolbar;
- (void)updateAvailability;
- (void)copyImage;
- (void)pin;
- (void)save;
- (void)showMessage:(NSString *)message;
@end

@implementation BotCapturePanel
- (BOOL)canBecomeKeyWindow {return YES;}
- (BOOL)canBecomeMainWindow {return NO;}
- (id)windowWillReturnFieldEditor:(NSWindow *)window toObject:(id)object {
    if(![object isKindOfClass:NSTextField.class]||![[object delegate] isKindOfClass:BotCaptureCanvas.class])return nil;
    if(!self.captureEditor){self.captureEditor=[BotCaptureFieldEditor new];self.captureEditor.fieldEditor=YES;}
    return self.captureEditor;
}
- (BOOL)performKeyEquivalent:(NSEvent *)event {
    BOOL command=(event.modifierFlags&NSEventModifierFlagCommand)!=0;
    NSString *key=event.charactersIgnoringModifiers.lowercaseString;
    NSTextView *editor=[self.firstResponder isKindOfClass:NSTextView.class]?(NSTextView *)self.firstResponder:nil;
    if(command && self.captureCanvas) {
        if([key isEqual:@"c"]){
            // Default note focus must not swallow image copy, but text selection owns Copy.
            if(composing(editor))return YES;
            if(editor.selectedRange.length>0){[editor copy:nil];return YES;}
            [self.captureCanvas copyImage];return YES;
        }
        if(editor)return [super performKeyEquivalent:event];
        if([key isEqual:@"s"]){[self.captureCanvas save];return YES;}
        if([key isEqual:@"z"]){if(event.modifierFlags&NSEventModifierFlagShift)[self.captureCanvas redoAction:nil];else [self.captureCanvas undoAction:nil];return YES;}
    }
    return [super performKeyEquivalent:event];
}
@end


static NSRect selectionBetween(NSPoint a,NSPoint b,NSRect bounds) {
    NSRect r=NSMakeRect(MIN(a.x,b.x),MIN(a.y,b.y),fabs(a.x-b.x),fabs(a.y-b.y));
    return NSIntersectionRect(r,bounds);
}
static NSImage *imageFromBitmap(NSBitmapImageRep *rep) {
    if(!rep)return nil;
    NSImage *image=[[NSImage alloc] initWithSize:NSMakeSize(rep.pixelsWide,rep.pixelsHigh)];
    [image addRepresentation:rep];return image;
}
static NSWindowCollectionBehavior captureBehavior(void) {
    NSWindowCollectionBehavior behavior=NSWindowCollectionBehaviorCanJoinAllSpaces|NSWindowCollectionBehaviorFullScreenAuxiliary;
    if(@available(macOS 13.0,*))behavior|=NSWindowCollectionBehaviorCanJoinAllApplications;
    return behavior;
}
static BotCapturePanel *capturePanel(NSRect frame,BOOL overlay) {
    BotCapturePanel *panel=[[BotCapturePanel alloc] initWithContentRect:frame styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
    panel.delegate=panel;panel.releasedWhenClosed=NO;panel.hidesOnDeactivate=NO;panel.becomesKeyOnlyIfNeeded=NO;
    panel.level=overlay?NSScreenSaverWindowLevel:NSFloatingWindowLevel;
    panel.collectionBehavior=captureBehavior();panel.opaque=overlay;panel.backgroundColor=NSColor.clearColor;
    panel.hasShadow=!overlay;panel.acceptsMouseMovedEvents=YES;
    return panel;
}

@implementation BotCaptureCanvas
- (BOOL)control:(NSControl *)control textView:(NSTextView *)editor doCommandBySelector:(SEL)command {
    if(command==@selector(insertNewline:)) {
        // Keep the Return that accepts an IME candidate inside the text system.
        if(composing(editor))return YES;
        if(control==self.textEditor){[self textDone:control];return YES;}
        if(control==self.noteEditor){[self askAction:control];return YES;}
    }
    if(command==@selector(cancelOperation:)) {
        if(composing(editor))return NO;
        if(control==self.textEditor){[self.textEditor removeFromSuperview];self.textEditor=nil;[self focusNote];}
        else [self closeAction:nil];
        return YES;
    }
    return NO;
}
- (void)controlTextDidChange:(NSNotification *)notification {
    if(notification.object==self.noteEditor)self.document.note=self.noteEditor.stringValue;
}
- (void)focusNote {
    if(!self.noteEditor.editable||self.toolbar.hidden||self.textEditor||NSIsEmptyRect(self.document.selection))return;
    self.adjustingSelection=NO;
    [self.noteEditor.window makeKeyWindow];[self.noteEditor.window makeFirstResponder:self.noteEditor];
    NSTextView *editor=(NSTextView *)self.noteEditor.currentEditor;
    [editor setSelectedRange:NSMakeRange(editor.string.length,0)];
}
- (void)adjustWidth:(NSSlider *)sender {self.stroke=sender.doubleValue;[self updateWidthPreview];}
- (void)updateWidthPreview {
    // The toolbar shows the current stroke, while the popover previews its actual width.
    NSImage *image=[[NSImage alloc] initWithSize:NSMakeSize(22,22)];[image lockFocus];
    [NSColor.blackColor setStroke];NSBezierPath *path=[NSBezierPath new];
    path.lineWidth=MAX(1,self.stroke*0.5);path.lineCapStyle=NSLineCapStyleRound;
    [path moveToPoint:NSMakePoint(4,11)];[path lineToPoint:NSMakePoint(18,11)];[path stroke];[image unlockFocus];image.template=YES;
    self.widthButton.image=image;
    NSString *label=[self.owner text:@"capture.width" fallback:@"Stroke width"];
    self.widthButton.toolTip=[NSString stringWithFormat:@"%@ · %.1f",label,self.stroke];
    [self.widthButton setAccessibilityLabel:self.widthButton.toolTip];
    self.widthValue.stringValue=[NSString stringWithFormat:@"%.1f",self.stroke];
    self.widthPreview.stroke=self.stroke;self.widthPreview.ink=self.ink;self.widthPreview.needsDisplay=YES;
}
- (void)updateTools {
    for(NSButton *button in self.toolButtons)button.layer.backgroundColor=[button.identifier isEqual:self.tool]?[NSColor.controlAccentColor colorWithAlphaComponent:0.2].CGColor:NSColor.clearColor.CGColor;
}
- (void)separator:(CGFloat)x {
    NSBox *line=[[NSBox alloc] initWithFrame:NSMakeRect(x,49,1,20)];line.boxType=NSBoxSeparator;[self.toolbar addSubview:line];
}
- (NSButton *)icon:(NSString *)symbol key:(NSString *)key fallback:(NSString *)fallback action:(SEL)action x:(CGFloat)x {
    NSString *label=[self.owner text:key fallback:fallback];
    NSButton *b=[NSButton buttonWithImage:[NSImage imageWithSystemSymbolName:symbol accessibilityDescription:label] target:self action:action];
    b.frame=NSMakeRect(x,44,30,30);b.bordered=NO;b.bezelStyle=NSBezelStyleRegularSquare;b.imageScaling=NSImageScaleProportionallyDown;
    b.toolTip=label;[b setAccessibilityLabel:label];b.contentTintColor=NSColor.labelColor;b.wantsLayer=YES;b.layer.cornerRadius=6;
    [self.toolbar addSubview:b];return b;
}
- (instancetype)initWithFrame:(NSRect)frame {
    if((self=[super initWithFrame:frame])){_tool=@"select";_ink=NSColor.systemRedColor;_stroke=3;}
    return self;
}
- (void)viewDidChangeEffectiveAppearance {
    [super viewDidChangeEffectiveAppearance];
    if(!self.toolbar)return;
    [self.effectiveAppearance performAsCurrentDrawingAppearance:^{[self updateTools];[self updateAvailability];}];
}
- (BOOL)isFlipped {return YES;}
- (BOOL)acceptsFirstResponder {return YES;}
- (BOOL)acceptsFirstMouse:(NSEvent *)event {return YES;}
- (NSPoint)documentPoint:(NSEvent *)event {
    NSPoint p=[self convertPoint:event.locationInWindow fromView:nil];
    NSRect region=self.pinned?self.document.selection:NSMakeRect(0,0,self.document.canvasSize.width,self.document.canvasSize.height);
    p.x=region.origin.x+p.x*region.size.width/self.bounds.size.width;
    p.y=region.origin.y+p.y*region.size.height/self.bounds.size.height;
    p.x=MAX(0,MIN(self.document.canvasSize.width,p.x));p.y=MAX(0,MIN(self.document.canvasSize.height,p.y));
    return p;
}
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    if(self.tracking)[self removeTrackingArea:self.tracking];
    self.tracking=[[NSTrackingArea alloc] initWithRect:self.bounds options:NSTrackingMouseMoved|NSTrackingMouseEnteredAndExited|NSTrackingActiveAlways owner:self userInfo:nil];
    [self addTrackingArea:self.tracking];
}
- (void)drawRect:(NSRect)dirty {
    BotCaptureDocument *doc=self.document;
    [NSGraphicsContext saveGraphicsState];
    NSAffineTransform *transform=[NSAffineTransform transform];
    NSRect region=self.pinned?doc.selection:NSMakeRect(0,0,doc.canvasSize.width,doc.canvasSize.height);
    [transform scaleXBy:self.bounds.size.width/region.size.width yBy:self.bounds.size.height/region.size.height];
    [transform translateXBy:-region.origin.x yBy:-region.origin.y];[transform concat];
    [doc.image drawInRect:NSMakeRect(0,0,doc.canvasSize.width,doc.canvasSize.height) fromRect:NSZeroRect operation:NSCompositingOperationCopy fraction:1 respectFlipped:YES hints:nil];
    if(self.previewMark)[doc.marks addObject:self.previewMark];
    [doc drawMarks];
    if(self.previewMark)[doc.marks removeLastObject];
    [NSGraphicsContext restoreGraphicsState];
    if(!self.pinned) {
        NSRect r=doc.selection;
        if(NSIsEmptyRect(r))r=self.hoverRect;
        NSBezierPath *shade=[NSBezierPath bezierPathWithRect:self.bounds];
        if(!NSIsEmptyRect(r))[shade appendBezierPathWithRect:r];
        shade.windingRule=NSWindingRuleEvenOdd;
        [[[NSColor blackColor] colorWithAlphaComponent:0.36] setFill];[shade fill];
        if(!NSIsEmptyRect(r)) {
            [NSColor.controlAccentColor setStroke];NSBezierPath *border=[NSBezierPath bezierPathWithRect:r];border.lineWidth=1.5;[border stroke];
            for(int x=0;x<3;x++)for(int y=0;y<3;y++)if(x!=1||y!=1) {
                NSRect dot=NSMakeRect(r.origin.x+x*r.size.width/2-3,r.origin.y+y*r.size.height/2-3,6,6);
                [NSColor.whiteColor setFill];NSRectFill(dot);
            }
            NSString *size=[NSString stringWithFormat:@"%ld × %ld",(long)llround(r.size.width*doc.pixelScale),(long)llround(r.size.height*doc.pixelScale)];
            [size drawAtPoint:NSMakePoint(r.origin.x,MAX(6,r.origin.y-22)) withAttributes:@{NSFontAttributeName:[NSFont monospacedDigitSystemFontOfSize:12 weight:NSFontWeightMedium],NSForegroundColorAttributeName:NSColor.whiteColor,NSBackgroundColorAttributeName:NSColor.blackColor}];
        }
        if(self.selecting || NSIsEmptyRect(doc.selection)) {
            NSPoint p=self.last;CGFloat side=108;
            NSRect magnifier=NSMakeRect(MIN(self.bounds.size.width-side-8,p.x+24),MIN(self.bounds.size.height-side-8,p.y+24),side,side);
            magnifier.origin.x=MAX(8,magnifier.origin.x);magnifier.origin.y=MAX(8,magnifier.origin.y);
            NSRect source=NSMakeRect(MAX(0,MIN(doc.canvasSize.width-18,p.x-9)),MAX(0,MIN(doc.canvasSize.height-18,doc.canvasSize.height-p.y-9)),18,18);
            [NSGraphicsContext saveGraphicsState];
            [NSBezierPath clipRect:magnifier];NSGraphicsContext.currentContext.imageInterpolation=NSImageInterpolationNone;
            [doc.image drawInRect:magnifier fromRect:source operation:NSCompositingOperationCopy fraction:1 respectFlipped:YES hints:nil];
            [NSColor.whiteColor setStroke];NSFrameRectWithWidth(magnifier,2);
            NSBezierPath *cross=[NSBezierPath new];[cross moveToPoint:NSMakePoint(NSMidX(magnifier)-9,NSMidY(magnifier))];[cross lineToPoint:NSMakePoint(NSMidX(magnifier)+9,NSMidY(magnifier))];
            [cross moveToPoint:NSMakePoint(NSMidX(magnifier),NSMidY(magnifier)-9)];[cross lineToPoint:NSMakePoint(NSMidX(magnifier),NSMidY(magnifier)+9)];[cross stroke];
            [NSGraphicsContext restoreGraphicsState];
        }
    }
}
- (NSButton *)button:(NSString *)title action:(SEL)action x:(CGFloat)x width:(CGFloat)width {
    NSButton *button=[NSButton buttonWithTitle:title target:self action:action];
    button.frame=NSMakeRect(x,6,width,26);button.bezelStyle=NSBezelStyleRounded;
    [self.toolbar addSubview:button];return button;
}
- (void)buildToolbar {
    [self.widthPopover close];self.widthPopover=nil;
    [self.toolbar removeFromSuperview];CGFloat width=520;
    BotCaptureSurface *toolbar=[[BotCaptureSurface alloc] initWithFrame:NSMakeRect(0,0,width,86)];
    self.toolbar=toolbar;toolbar.wantsLayer=YES;toolbar.fillColor=NSColor.windowBackgroundColor;
    toolbar.strokeColor=[NSColor.separatorColor colorWithAlphaComponent:0.65];
    toolbar.layer.cornerRadius=12;toolbar.layer.borderWidth=0.5;
    if(self.pinned) {
        if(!self.toolbarPanel)self.toolbarPanel=capturePanel(NSMakeRect(0,0,width,86),NO);
        ((BotCapturePanel *)self.toolbarPanel).captureCanvas=self;
        self.toolbarPanel.contentView=self.toolbar;[self.window addChildWindow:self.toolbarPanel ordered:NSWindowAbove];[self.toolbarPanel orderFront:nil];
    } else { ((BotCapturePanel *)self.window).captureCanvas=self;[self addSubview:self.toolbar]; }
    self.toolButtons=[NSMutableArray new];
    NSArray *symbols=@[@"cursorarrow",@"arrow.up.right",@"rectangle",@"textformat",@"square.grid.3x3",@"rectangle.fill"];
    NSArray *keys=@[@"select",@"arrow",@"rectangle",@"text",@"mosaic",@"redact"];
    for(NSUInteger i=0;i<keys.count;i++) {
        NSButton *b=[self icon:symbols[i] key:[@"capture.tool." stringByAppendingString:keys[i]] fallback:keys[i] action:@selector(chooseTool:) x:10+i*32];
        b.identifier=keys[i];[self.toolButtons addObject:b];
    }
    [self separator:209];
    NSButton *color=[self icon:@"paintpalette.fill" key:@"capture.color" fallback:@"Color" action:@selector(changeColor:) x:216];color.contentTintColor=self.ink;
    self.widthButton=[self icon:@"lineweight" key:@"capture.width" fallback:@"Stroke width" action:@selector(changeWidth:) x:248];
    [self icon:@"arrow.uturn.backward" key:@"capture.undo" fallback:@"Undo (⌘Z)" action:@selector(undoAction:) x:280];
    [self icon:@"arrow.uturn.forward" key:@"capture.redo" fallback:@"Redo (⇧⌘Z)" action:@selector(redoAction:) x:312];
    [self separator:351];
    [self icon:@"doc.on.doc" key:@"capture.copy" fallback:@"Copy (⌘C)" action:@selector(copyAction:) x:361];
    [self icon:@"square.and.arrow.down" key:@"capture.save" fallback:@"Save (⌘S)" action:@selector(saveAction:) x:395];
    [self icon:@"pin" key:@"capture.pin" fallback:@"Pin (F3)" action:@selector(pinAction:) x:429];
    [self icon:@"xmark" key:@"capture.close" fallback:@"Cancel (Esc)" action:@selector(closeAction:) x:478];
    BotCaptureSurface *composer=[[BotCaptureSurface alloc] initWithFrame:NSMakeRect(12,10,width-24,32)];composer.wantsLayer=YES;
    composer.layer.cornerRadius=9;composer.fillColor=NSColor.textBackgroundColor;
    composer.layer.borderWidth=0.5;composer.strokeColor=NSColor.separatorColor;
    [self.toolbar addSubview:composer];
    self.noteEditor=[[NSTextField alloc] initWithFrame:NSMakeRect(10,6,composer.bounds.size.width-122,20)];
    self.noteEditor.placeholderString=[self.owner text:@"capture.note" fallback:@"Add a note (optional)"];
    self.noteEditor.stringValue=self.document.note?:@"";self.noteEditor.font=[NSFont systemFontOfSize:13];self.noteEditor.delegate=self;
    self.noteEditor.editable=!self.document.identifier.length;
    self.noteEditor.bordered=NO;self.noteEditor.drawsBackground=NO;self.noteEditor.focusRingType=NSFocusRingTypeNone;
    [self.noteEditor setAccessibilityLabel:self.noteEditor.placeholderString];[composer addSubview:self.noteEditor];
    self.askButton=[self icon:@"sparkles" key:@"capture.ask" fallback:@"Send to Bot" action:@selector(askAction:) x:0];
    [self.askButton removeFromSuperview];self.askButton.frame=NSMakeRect(composer.bounds.size.width-108,2,102,28);
    self.askButton.title=[self.owner text:@"capture.askButton" fallback:@"Ask Bot"];self.askButton.imagePosition=NSImageLeft;
    self.askButton.font=[NSFont systemFontOfSize:12 weight:NSFontWeightMedium];
    self.askButton.layer.cornerRadius=7;[composer addSubview:self.askButton];
    self.statusLabel=[NSTextField labelWithString:@""];self.statusLabel.frame=NSMakeRect(12,83,width-24,20);
    self.statusLabel.font=[NSFont systemFontOfSize:11];self.statusLabel.textColor=NSColor.secondaryLabelColor;
    [self.toolbar addSubview:self.statusLabel];
    // Make sending scope visible before any submission. It belongs to this
    // frozen document; changing it never changes the next capture's preference.
    for(NSView *view in self.toolbar.subviews){NSRect frame=view.frame;frame.origin.y+=24;view.frame=frame;}
    self.backgroundButton=[NSButton checkboxWithTitle:[self.owner text:@"capture.includeBackground" fallback:@"Also send full screen"] target:self action:@selector(changeBackground:)];
    self.backgroundButton.frame=NSMakeRect(12,6,300,22);self.backgroundButton.font=[NSFont systemFontOfSize:11];
    self.backgroundButton.state=self.document.includeBackground?NSControlStateValueOn:NSControlStateValueOff;
    [self.toolbar addSubview:self.backgroundButton];
    self.scopeLabel=[NSTextField labelWithString:@""];self.scopeLabel.frame=NSMakeRect(300,9,width-312,18);
    self.scopeLabel.alignment=NSTextAlignmentRight;self.scopeLabel.font=[NSFont systemFontOfSize:11];self.scopeLabel.textColor=NSColor.secondaryLabelColor;
    [self.toolbar addSubview:self.scopeLabel];
    [self updateAvailability];[self positionToolbar];[self updateTools];[self updateWidthPreview];
}
- (void)changeBackground:(NSButton *)sender {
    if(self.document.identifier.length||[self.document.source isEqual:@"clipboard"]){[self updateAvailability];return;}
    self.document.includeBackground=sender.state==NSControlStateValueOn;[self updateAvailability];
}
- (void)positionToolbar {
    if(!self.toolbar)return;
    CGFloat width=self.toolbar.frame.size.width;
    if(self.pinned) {
        NSRect frame=self.window.frame,visible=self.window.screen.visibleFrame;
        CGFloat x=MAX(NSMinX(visible)+8,MIN(NSMaxX(visible)-width-8,frame.origin.x));
        CGFloat height=self.toolbar.frame.size.height;CGFloat y=frame.origin.y-height-8;if(y<NSMinY(visible))y=MIN(NSMaxY(visible)-height,NSMaxY(frame)+8);
        [self.toolbarPanel setFrameOrigin:NSMakePoint(x,y)];return;
    }
    NSRect r=self.document.selection;
    CGFloat x=MAX(8,MIN(self.bounds.size.width-width-8,r.origin.x));
    CGFloat height=self.toolbar.frame.size.height;CGFloat y=NSMaxY(r)+10;if(y+height>self.bounds.size.height-8)y=MAX(8,r.origin.y-height-10);
    [self.toolbar setFrameOrigin:NSMakePoint(x,y)];
}
- (void)updateAvailability {
    BOOL pending=[self.document.outcome isEqual:@"unknown"]||[self.document.outcome isEqual:@"accepted"];
    BOOL supported=[self.owner.availability[@"state"] isEqual:@"supported"];
    self.askButton.enabled=supported&&!pending;
    BOOL clipboard=[self.document.source isEqual:@"clipboard"];
    self.backgroundButton.hidden=clipboard||self.restored;
    self.backgroundButton.enabled=!self.document.identifier.length;
    self.backgroundButton.state=self.document.includeBackground?NSControlStateValueOn:NSControlStateValueOff;
    self.scopeLabel.stringValue=self.restored?(self.document.includeBackground?[self.owner text:@"capture.fullScreenIncluded" fallback:@"Full screen included"]:[self.owner text:@"capture.selectionOnly" fallback:@"Selection only"]):clipboard?[self.owner text:@"capture.clipboardOnly" fallback:@"Clipboard image only"]:self.document.includeBackground?@"":[self.owner text:@"capture.selectionOnly" fallback:@"Selection only"];
    self.askButton.contentTintColor=self.askButton.enabled?NSColor.controlAccentColor:NSColor.disabledControlTextColor;
    self.askButton.layer.backgroundColor=[NSColor.controlAccentColor colorWithAlphaComponent:self.askButton.enabled?0.16:0.04].CGColor;
    NSString *message=@"";
    if(pending)message=[self.owner text:@"capture.unknown" fallback:@"Delivery unconfirmed. Kept locally; will not resend."];
    else if([self.document.outcome isEqual:@"rejected"])message=[self.owner text:@"capture.rejected" fallback:@"Not sent. You can retry."];
    self.askButton.toolTip=self.askButton.enabled?[self.owner text:@"capture.ask" fallback:@"Send screenshot to Bot (Enter)"]:(message.length?message:self.owner.availability[@"message"]);
    [self showMessage:message];
}
- (void)showMessage:(NSString *)message {
    // Scope remains visible; only delivery exceptions add another row.
    self.statusLabel.stringValue=message?:@"";self.statusLabel.hidden=!message.length;
    self.noteEditor.toolTip=message.length?message:nil;
    NSRect frame=self.toolbar.frame;frame.size.height=message.length?134:110;self.toolbar.frame=frame;
    if(self.pinned){NSRect panelFrame=self.toolbarPanel.frame;panelFrame.size.height=frame.size.height;[self.toolbarPanel setFrame:panelFrame display:YES];}
    [self positionToolbar];
    self.noteEditor.textColor=NSColor.labelColor;
}
- (void)chooseTool:(NSButton *)sender {
    if(self.document.identifier.length)return;
    [self commitText];self.tool=sender.identifier;
    if(self.pinned&&[self.tool isEqual:@"select"]){self.editing=NO;[self.toolbarPanel orderOut:nil];return;}
    if(self.pinned){self.editing=YES;[self.window makeKeyWindow];}
    self.adjustingSelection=[self.tool isEqual:@"select"];
    [self updateTools];
    if(self.adjustingSelection)[self.window makeFirstResponder:self];else [self focusNote];
}
- (void)changeColor:(NSButton *)sender {
    NSArray *colors=@[NSColor.systemRedColor,NSColor.systemYellowColor,NSColor.systemBlueColor,NSColor.whiteColor,NSColor.blackColor];
    NSUInteger index=[colors indexOfObject:self.ink];self.ink=colors[(index+1)%colors.count];sender.contentTintColor=self.ink;
[self updateWidthPreview];
}
- (void)changeWidth:(NSButton *)sender {
    if(self.widthPopover.shown){[self.widthPopover close];return;}
    NSViewController *controller=[NSViewController new];controller.view=[[NSView alloc] initWithFrame:NSMakeRect(0,0,228,108)];
    NSTextField *label=[NSTextField labelWithString:[self.owner text:@"capture.width" fallback:@"Stroke width"]];
    label.frame=NSMakeRect(16,76,150,18);label.font=[NSFont systemFontOfSize:12 weight:NSFontWeightMedium];[controller.view addSubview:label];
    self.widthValue=[NSTextField labelWithString:@""];self.widthValue.frame=NSMakeRect(164,76,48,18);
    self.widthValue.alignment=NSTextAlignmentRight;self.widthValue.font=[NSFont monospacedDigitSystemFontOfSize:12 weight:NSFontWeightRegular];[controller.view addSubview:self.widthValue];
    self.widthPreview=[[BotStrokePreview alloc] initWithFrame:NSMakeRect(16,38,196,28)];[controller.view addSubview:self.widthPreview];
    NSSlider *slider=[NSSlider sliderWithValue:self.stroke minValue:1 maxValue:16 target:self action:@selector(adjustWidth:)];
    slider.frame=NSMakeRect(16,10,196,22);slider.continuous=YES;slider.numberOfTickMarks=0;
    [slider setAccessibilityLabel:[self.owner text:@"capture.width" fallback:@"Stroke width"]];[controller.view addSubview:slider];
    self.widthPopover=[NSPopover new];self.widthPopover.contentViewController=controller;
    self.widthPopover.behavior=NSPopoverBehaviorTransient;self.widthPopover.animates=NO;
    [self updateWidthPreview];[self.widthPopover showRelativeToRect:sender.bounds ofView:sender preferredEdge:NSRectEdgeMaxY];
}
- (void)undoAction:(id)sender {if(self.document.identifier.length)return;[self commitText];[self.document undo];self.needsDisplay=YES;}
- (void)redoAction:(id)sender {if(self.document.identifier.length)return;[self.document redo];self.needsDisplay=YES;}

- (void)copyAction:(id)sender {[self copyImage];}
- (void)saveAction:(id)sender {[self save];}
- (void)pinAction:(id)sender {[self pin];}
- (void)askAction:(id)sender {
    if(self.gestureActive||!self.askButton.enabled)return;
    [self.widthPopover close];[self commitText];
    if(self.noteEditor)self.document.note=self.noteEditor.stringValue;[self.owner ask:self];
}
- (void)closeAction:(id)sender {
    [self.widthPopover close];
    if(!self.pinned){[self.owner finish:YES];return;}
    [self.toolbarPanel orderOut:nil];[self.window orderOut:nil];
}
- (void)commitText {
    if(!self.textEditor)return;
    NSString *text=self.textEditor.stringValue;
    if(text.length)[self.document addMark:@{@"kind":@"text",@"a":[NSValue valueWithPoint:self.start],@"b":[NSValue valueWithPoint:self.start],@"text":text,@"color":self.ink,@"width":@(self.stroke)}];
    [self.textEditor removeFromSuperview];self.textEditor=nil;
    [self.window makeFirstResponder:self];self.needsDisplay=YES;
}
- (void)textDone:(id)sender {
    if(composing((NSTextView *)self.textEditor.currentEditor))return;
    [self commitText];[self focusNote];
}
- (void)mouseMoved:(NSEvent *)event {
    self.last=[self documentPoint:event];
    if(!self.pinned && NSIsEmptyRect(self.document.selection)) {
        self.hoverRect=NSZeroRect;
        for(NSValue *value in self.document.windows)if(NSPointInRect(self.last,value.rectValue)){self.hoverRect=value.rectValue;break;}
        self.needsDisplay=YES;
    }
}
- (void)mouseDown:(NSEvent *)event {
    self.gestureActive=YES;[self.window makeFirstResponder:self];[self commitText];
    if(self.pinned&&!self.editing) {
        if(event.clickCount==2){self.gestureActive=NO;[self closeAction:nil];return;}
        [self.window performWindowDragWithEvent:event];self.gestureActive=NO;return;
    }
    if(self.document.identifier.length){self.gestureActive=NO;return;}
    self.start=self.last=[self documentPoint:event];
    self.originalSelection=self.document.selection;self.resizeEdges=0;self.moving=NO;self.selecting=NO;
    if([self.tool isEqual:@"select"]&&!self.pinned) {
        NSRect r=self.document.selection;CGFloat tolerance=7;
        if(!NSIsEmptyRect(r) && NSPointInRect(self.start,NSInsetRect(r,-tolerance,-tolerance))) {
            if(fabs(self.start.x-NSMinX(r))<tolerance)self.resizeEdges|=1;
            if(fabs(self.start.x-NSMaxX(r))<tolerance)self.resizeEdges|=2;
            if(fabs(self.start.y-NSMinY(r))<tolerance)self.resizeEdges|=4;
            if(fabs(self.start.y-NSMaxY(r))<tolerance)self.resizeEdges|=8;
            self.moving=self.resizeEdges==0;
        } else {self.selecting=YES;self.document.selection=NSZeroRect;[self.document.marks removeAllObjects];[self.document.redoMarks removeAllObjects];}
        self.toolbar.hidden=YES;
    } else if(NSPointInRect(self.start,self.document.selection)) {
        if([self.tool isEqual:@"text"]) {
            NSRect region=self.pinned?self.document.selection:NSMakeRect(0,0,self.document.canvasSize.width,self.document.canvasSize.height);
            CGFloat sx=self.bounds.size.width/region.size.width,sy=self.bounds.size.height/region.size.height;
            CGFloat x=(self.start.x-region.origin.x)*sx,y=(self.start.y-region.origin.y)*sy;
            self.textEditor=[[NSTextField alloc] initWithFrame:NSMakeRect(x,y,MAX(80,MIN(320,self.bounds.size.width-x)),32)];
            self.textEditor.font=[NSFont systemFontOfSize:MAX(14,self.stroke*5)];self.textEditor.textColor=self.ink;
            self.textEditor.delegate=self;self.textEditor.target=self;self.textEditor.action=@selector(textDone:);
            [self addSubview:self.textEditor];[self.window makeFirstResponder:self.textEditor];
        } else self.previewMark=@{@"kind":self.tool,@"a":[NSValue valueWithPoint:self.start],@"b":[NSValue valueWithPoint:self.start],@"width":@(self.stroke),@"color":self.ink};
    }
    self.needsDisplay=YES;
}
- (void)mouseDragged:(NSEvent *)event {
    if(self.document.identifier.length || self.pinned&&!self.editing)return;
    NSPoint p=[self documentPoint:event];self.last=p;
    NSRect bounds=NSMakeRect(0,0,self.document.canvasSize.width,self.document.canvasSize.height);
    if(self.selecting)self.document.selection=selectionBetween(self.start,p,bounds);
    else if(self.moving) {
        NSRect r=self.originalSelection;r.origin.x+=p.x-self.start.x;r.origin.y+=p.y-self.start.y;
        r.origin.x=MAX(0,MIN(bounds.size.width-r.size.width,r.origin.x));r.origin.y=MAX(0,MIN(bounds.size.height-r.size.height,r.origin.y));
        self.document.selection=r;
    } else if(self.resizeEdges) {
        NSRect r=self.originalSelection;NSPoint a=r.origin,b=NSMakePoint(NSMaxX(r),NSMaxY(r));
        if(self.resizeEdges&1)a.x=p.x;if(self.resizeEdges&2)b.x=p.x;
        if(self.resizeEdges&4)a.y=p.y;if(self.resizeEdges&8)b.y=p.y;
        self.document.selection=selectionBetween(a,b,bounds);
    } else if(self.previewMark) {
        NSRect r=self.document.selection;p.x=MAX(NSMinX(r),MIN(NSMaxX(r),p.x));p.y=MAX(NSMinY(r),MIN(NSMaxY(r),p.y));
        NSMutableDictionary *mark=[self.previewMark mutableCopy];mark[@"b"]=[NSValue valueWithPoint:p];self.previewMark=mark;
    }
    self.needsDisplay=YES;
}
- (void)mouseUp:(NSEvent *)event {
    if(self.selecting&&NSIsEmptyRect(self.document.selection)&&!NSIsEmptyRect(self.hoverRect))self.document.selection=self.hoverRect;
    if(self.previewMark){[self.document addMark:self.previewMark];self.previewMark=nil;}
    self.selecting=NO;self.moving=NO;self.resizeEdges=0;
    if(self.document.selection.size.width>=1 && self.document.selection.size.height>=1) {
        if(!self.toolbar)[self buildToolbar];self.toolbar.hidden=self.pinned&&!self.editing;[self positionToolbar];
    }
    self.needsDisplay=YES;
    self.gestureActive=NO;
    if(!self.pinned&&!self.adjustingSelection)[self focusNote];
}
- (void)rightMouseDown:(NSEvent *)event {
    if(self.textEditor){[self commitText];return;}
    if(!self.pinned){self.tool=@"select";return;}
    NSMenu *menu=[NSMenu new];
    NSArray *names=@[[self.owner text:@"capture.copy" fallback:@"Copy"],[self.owner text:@"capture.save" fallback:@"Save"],[self.owner text:@"capture.edit" fallback:@"Annotate"],@"Ask Bot",[self.owner text:@"capture.hidePin" fallback:@"Hide"],[self.owner text:@"capture.closePin" fallback:@"Close"]];
    NSArray *actions=@[NSStringFromSelector(@selector(copyAction:)),NSStringFromSelector(@selector(saveAction:)),NSStringFromSelector(@selector(editPin:)),NSStringFromSelector(@selector(askAction:)),NSStringFromSelector(@selector(closeAction:)),NSStringFromSelector(@selector(destroyPin:))];
    menu.autoenablesItems=NO;
    for(NSUInteger i=0;i<names.count;i++){NSMenuItem *item=[menu addItemWithTitle:names[i] action:NSSelectorFromString(actions[i]) keyEquivalent:@""];item.target=self;if(i==3)item.enabled=[self.owner.availability[@"state"] isEqual:@"supported"]&&![self.document.outcome isEqual:@"unknown"];if(i==2)item.enabled=!self.document.identifier.length;}
    [NSMenu popUpContextMenu:menu withEvent:event forView:self];
}
- (void)editPin:(id)sender {
    if(self.document.identifier.length)return;
    self.editing=!self.editing;
    if(self.editing){[self buildToolbar];self.tool=@"arrow";[self.window makeKeyWindow];}
    else {self.toolbar.hidden=YES;[self.toolbarPanel orderOut:nil];[self commitText];}
}
- (void)destroyPin:(id)sender {
    NSString *identifier=self.document.identifier;
    if(identifier.length && self.owner.handle && ![self.document.outcome isEqual:@"unknown"])
        desktopCaptureEvent(self.owner.handle,23,(char *)identifier.UTF8String);
    [self.toolbarPanel close];[self.window close];[self.owner.pins removeObject:(BotCapturePanel *)self.window];
}
- (void)scrollWheel:(NSEvent *)event {
    if(!self.pinned||self.editing)return;
    CGFloat factor=event.scrollingDeltaY>0?1.08:0.92;
    NSRect frame=self.window.frame;NSSize available=self.window.screen.visibleFrame.size;
    CGFloat maxWidth=MIN(available.width,available.height*self.document.selection.size.width/self.document.selection.size.height);
    CGFloat width=MIN(maxWidth,MAX(80,frame.size.width*factor));
    CGFloat height=width*self.document.selection.size.height/self.document.selection.size.width;
    frame.origin.y+=frame.size.height-height;frame.size=NSMakeSize(width,height);
    NSRect visible=self.window.screen.visibleFrame;
    frame.origin.x=MAX(NSMinX(visible),MIN(NSMaxX(visible)-width,frame.origin.x));
    frame.origin.y=MAX(NSMinY(visible),MIN(NSMaxY(visible)-height,frame.origin.y));
    [self.window setFrame:frame display:YES];[self positionToolbar];
}
- (void)keyDown:(NSEvent *)event {
    if(event.isARepeat&&(event.keyCode==36||event.keyCode==76))return;
    if(event.keyCode==36||event.keyCode==76){[self askAction:nil];return;}
    if(!self.pinned&&[event.charactersIgnoringModifiers isEqual:@" "]){[self focusNote];return;}
    BOOL cmd=(event.modifierFlags&NSEventModifierFlagCommand)!=0;
    NSString *key=event.charactersIgnoringModifiers.lowercaseString;
    if(cmd&&[key isEqual:@"c"]){[self copyImage];return;}
    if(cmd&&[key isEqual:@"s"]){[self save];return;}
    if(cmd&&[key isEqual:@"z"]){if(event.modifierFlags&NSEventModifierFlagShift)[self redoAction:nil];else [self undoAction:nil];return;}
    if(event.keyCode==53){if(self.textEditor){[self.textEditor removeFromSuperview];self.textEditor=nil;}else if(self.pinned&&self.editing)[self editPin:nil];else [self closeAction:nil];return;}
    if(self.pinned&&[key isEqual:@" "]){[self editPin:nil];return;}
    if((event.keyCode==51||event.keyCode==117)&&!self.document.identifier.length){[self undoAction:nil];return;}
    if(event.keyCode>=123&&event.keyCode<=126&&!self.pinned&&!self.document.identifier.length) {
        NSRect r=self.document.selection;double step=(event.modifierFlags&NSEventModifierFlagShift)?10:1/self.document.pixelScale;
        if(event.keyCode==123)r.origin.x-=step;if(event.keyCode==124)r.origin.x+=step;
        if(event.keyCode==125)r.origin.y+=step;if(event.keyCode==126)r.origin.y-=step;
        r.origin.x=MAX(0,MIN(self.document.canvasSize.width-r.size.width,r.origin.x));
        r.origin.y=MAX(0,MIN(self.document.canvasSize.height-r.size.height,r.origin.y));self.document.selection=r;
        [self positionToolbar];self.needsDisplay=YES;return;
    }
    [super keyDown:event];
}
// Standard responder selectors cover menu commands as well as keyDown.
- (void)copy:(id)sender {[self copyImage];}
- (void)saveDocument:(id)sender {[self save];}
- (void)undo:(id)sender {[self undoAction:sender];}
- (void)redo:(id)sender {[self redoAction:sender];}
- (BOOL)validateUserInterfaceItem:(id<NSValidatedUserInterfaceItem>)item {
    if(item.action==@selector(copy:)||item.action==@selector(saveDocument:))return !NSIsEmptyRect(self.document.selection);
    return YES;
}
- (void)copyImage {
    [self.widthPopover close];
    [self commitText];NSImage *image=imageFromBitmap([self.document render:NO maximumEdge:0]);
    if(!image)return;
    [NSPasteboard.generalPasteboard clearContents];
    if([NSPasteboard.generalPasteboard writeObjects:@[image]]){if(!self.pinned)[self.owner finish:YES];else [self showMessage:[self.owner text:@"capture.copied" fallback:@"Copied"]];}
}
- (void)save {
    [self commitText];NSSavePanel *panel=[NSSavePanel savePanel];panel.allowedContentTypes=@[UTTypePNG];panel.nameFieldStringValue=@"Screenshot.png";
    [panel beginSheetModalForWindow:self.window completionHandler:^(NSModalResponse response){
        if(response!=NSModalResponseOK)return;
        NSData *data=[[self.document render:NO maximumEdge:0] representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        NSError *error=nil;
        if(![data writeToURL:panel.URL options:NSDataWritingAtomic error:&error])[self showMessage:[self.owner text:@"capture.saveFailed" fallback:@"Could not save image"]];
        else if(!self.pinned)[self.owner finish:YES];
    }];
}
- (void)pin {
    [self commitText];if(self.pinned){[self editPin:nil];return;}
    NSRect r=self.document.selection;NSRect screen=self.window.frame;
    [self.owner pinDocument:self.document at:NSMakePoint(screen.origin.x+r.origin.x,NSMaxY(screen)-NSMaxY(r))];
    [self.owner finish:YES];
}
@end

@implementation BotCapture
- (instancetype)init {
    if((self=[super init])) {
        _includeBackground=YES;_pins=[NSMutableArray new];_availability=@{@"state":@"unknown"};_language=@{};_excludedWindows=@[];_observers=[NSMutableArray new];
        __weak BotCapture *weak=self;
        _refreshTimer=[NSTimer scheduledTimerWithTimeInterval:3 repeats:YES block:^(NSTimer *timer){
            BotCapture *strong=weak;
            if(strong && (strong.overlay || strong.pins.count))[strong refresh];
        }];
        _keyMonitor=[NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskKeyDown handler:^NSEvent *(NSEvent *event){
            BotCapture *owner=weak;if(!owner)return event;
            NSMutableArray *canvases=[NSMutableArray new];if(owner.canvas)[canvases addObject:owner.canvas];
            for(NSWindow *pin in owner.pins)[canvases addObject:pin.contentView];
            for(BotCaptureCanvas *canvas in canvases) {
                if(canvas.widthPopover.shown&&(event.keyCode==36||event.keyCode==76||event.keyCode==53)) {
                    [canvas.widthPopover close];[canvas focusNote];return nil;
                }
            }
            if([event.window isKindOfClass:BotCapturePanel.class]&&event.isARepeat&&(event.keyCode==36||event.keyCode==76))return nil;
            return event;
        }];
        for(NSString *event in @[NSApplicationDidChangeScreenParametersNotification]) {
            id observer=[NSNotificationCenter.defaultCenter addObserverForName:event object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note){
                BotCapture *strong=weak;[strong finish:NO];
                for(NSWindow *pin in strong.pins) {
                    BOOL visible=NO;for(NSScreen *screen in NSScreen.screens)if(NSIntersectsRect(screen.visibleFrame,pin.frame)){visible=YES;break;}
                    if(!visible && NSScreen.mainScreen)[pin setFrameOrigin:NSScreen.mainScreen.visibleFrame.origin];
                }
            }];[_observers addObject:observer];
        }
        id observer=[NSWorkspace.sharedWorkspace.notificationCenter addObserverForName:NSWorkspaceActiveSpaceDidChangeNotification object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note){[weak finish:NO];}];
        [_observers addObject:observer];
    }return self;
}
- (NSString *)text:(NSString *)key fallback:(NSString *)fallback {return self.language[key]?:fallback;}
- (void)refresh {if(!self.stopped&&self.handle)desktopCaptureEvent(self.handle,20,"");}
- (void)setAvailability:(NSDictionary *)state {
    _availability=state?:@{@"state":@"unknown"};
    [self.canvas updateAvailability];for(NSWindow *pin in self.pins)[(BotCaptureCanvas *)pin.contentView updateAvailability];
}
- (void)capture {
    if(self.stopped)return;
    if(self.overlay||self.preparing) {[self finish:YES];return;}
    [self refresh];
    self.previousApp=NSWorkspace.sharedWorkspace.frontmostApplication;
    if(!CGPreflightScreenCaptureAccess()||bot_screen_capture_denied()) {
        NSAlert *alert=[NSAlert new];alert.messageText=[self text:@"capture.permission" fallback:@"Allow screen capture in Settings → Permissions, then try again."];
        [alert addButtonWithTitle:[self text:@"capture.ok" fallback:@"OK"]];[alert runModal];return;
    }
    if(@available(macOS 14.0,*)) {
        self.preparing=YES;NSUInteger generation=++self.generation;BOOL includeBackground=self.includeBackground;
        NSPoint pointer=NSEvent.mouseLocation;NSScreen *screen=NSScreen.mainScreen;
        for(NSScreen *candidate in NSScreen.screens)if(NSPointInRect(pointer,candidate.frame)){screen=candidate;break;}
        NSRect frame=screen.frame;CGDirectDisplayID displayID=[screen.deviceDescription[@"NSScreenNumber"] unsignedIntValue];
        NSString *application=self.previousApp.localizedName?:@"";
        pid_t pid=self.previousApp.processIdentifier;
        CGFloat mainHeight=NSScreen.screens.firstObject.frame.size.height;
        NSArray *windowInfo=CFBridgingRelease(CGWindowListCopyWindowInfo(kCGWindowListOptionOnScreenOnly|kCGWindowListExcludeDesktopElements,kCGNullWindowID));
        NSMutableArray *rects=[NSMutableArray new];NSString *title=@"";
        for(NSDictionary *info in windowInfo) {
            if([info[(id)kCGWindowLayer] intValue]!=0)continue;
            CGRect cg; if(!CGRectMakeWithDictionaryRepresentation((__bridge CFDictionaryRef)info[(id)kCGWindowBounds],&cg))continue;
            NSRect local=NSMakeRect(cg.origin.x-frame.origin.x,cg.origin.y-(mainHeight-NSMaxY(frame)),cg.size.width,cg.size.height);
            local=NSIntersectionRect(local,NSMakeRect(0,0,frame.size.width,frame.size.height));
            if(local.size.width>20 && local.size.height>20)[rects addObject:[NSValue valueWithRect:local]];
            if(!title.length && [info[(id)kCGWindowOwnerPID] intValue]==pid)title=info[(id)kCGWindowName]?:@"";
        }
        NSMutableSet *excluded=[NSMutableSet new];for(NSWindow *window in self.excludedWindows)[excluded addObject:@(window.windowNumber)];
        __weak BotCapture *weak=self;
        [SCShareableContent getShareableContentExcludingDesktopWindows:NO onScreenWindowsOnly:YES completionHandler:^(SCShareableContent *content,NSError *error) {
            dispatch_async(dispatch_get_main_queue(), ^{
                BotCapture *strong=weak;if(!strong || strong.stopped||generation!=strong.generation)return;
                SCDisplay *display=nil;for(SCDisplay *d in content.displays)if(d.displayID==displayID){display=d;break;}
                if(error||!display){strong.preparing=NO;[strong captureError];return;}
                NSMutableArray *exclude=[NSMutableArray new];for(SCWindow *w in content.windows)if([excluded containsObject:@(w.windowID)])[exclude addObject:w];
                SCContentFilter *filter=[[SCContentFilter alloc] initWithDisplay:display excludingWindows:exclude];
                SCStreamConfiguration *config=[SCStreamConfiguration new];
                config.width=MAX(1,llround(frame.size.width*screen.backingScaleFactor));config.height=MAX(1,llround(frame.size.height*screen.backingScaleFactor));config.showsCursor=NO;
                [SCScreenshotManager captureImageWithFilter:filter configuration:config completionHandler:^(CGImageRef image,NSError *failure) {
                    NSImage *still=image?[[NSImage alloc] initWithCGImage:image size:frame.size]:nil;
                    CGFloat scale=image?(CGFloat)CGImageGetWidth(image)/frame.size.width:1;
                    dispatch_async(dispatch_get_main_queue(), ^{
                        BotCapture *owner=weak;if(!owner || owner.stopped||generation!=owner.generation)return;
                        owner.preparing=NO;
                        if(failure||!still){[owner captureError];return;}
                        BotCaptureDocument *doc=[BotCaptureDocument new];doc.image=still;doc.canvasSize=frame.size;doc.pixelScale=scale;doc.capturedAt=[[NSISO8601DateFormatter new] stringFromDate:NSDate.date];
                        doc.includeBackground=includeBackground;doc.windows=rects;doc.applicationName=application;doc.windowTitle=title;
                        owner.overlay=capturePanel(frame,YES);owner.canvas=[[BotCaptureCanvas alloc] initWithFrame:NSMakeRect(0,0,frame.size.width,frame.size.height)];
                        owner.canvas.owner=owner;owner.canvas.document=doc;owner.canvas.last=NSMakePoint(pointer.x-frame.origin.x,NSMaxY(frame)-pointer.y);
                        owner.overlay.contentView=owner.canvas;[owner.overlay makeKeyAndOrderFront:nil];[owner.overlay makeFirstResponder:owner.canvas];
                    });
                }];
            });
        }];
    } else {
        NSAlert *alert=[NSAlert new];alert.messageText=[self text:@"capture.unsupported" fallback:@"Screen capture requires macOS 14 or later."];[alert runModal];
    }
}
- (void)captureError {
    NSAlert *alert=[NSAlert new];alert.messageText=[self text:@"capture.failed" fallback:@"Could not capture this display. Check screen access and try again."];[alert runModal];
}
- (void)finish:(BOOL)restoreFocus {
    ++self.generation;self.preparing=NO;
    [self.canvas.widthPopover close];
    BOOL wasKey=self.overlay.keyWindow;
    [self.overlay orderOut:nil];[self.overlay close];self.overlay=nil;self.canvas=nil;
    if(restoreFocus&&wasKey&&self.previousApp&&!self.previousApp.terminated)[self.previousApp activateWithOptions:0];
    self.previousApp=nil;
}
- (void)pinDocument:(BotCaptureDocument *)document at:(NSPoint)point {
    // Pin retains original context in its document; crop presentation uses a
    // separate view transform without replacing the full capture.
    NSBitmapImageRep *rep=[document render:NO maximumEdge:0];if(!rep)return;
    NSScreen *screen=NSScreen.mainScreen;
    for(NSScreen *candidate in NSScreen.screens)if(NSPointInRect(point,candidate.frame)){screen=candidate;break;}
    NSRect available=screen.visibleFrame;
    CGFloat scale=MIN(1,MIN(available.size.width*0.8/document.selection.size.width,available.size.height*0.8/document.selection.size.height));
    CGFloat width=MAX(1,document.selection.size.width*scale),height=MAX(1,document.selection.size.height*scale);
    point.x=MAX(NSMinX(available),MIN(NSMaxX(available)-width,point.x));point.y=MAX(NSMinY(available),MIN(NSMaxY(available)-height,point.y));
    BotCapturePanel *pin=capturePanel(NSMakeRect(point.x,point.y,width,height),NO);
    BotCaptureCanvas *canvas=[[BotCaptureCanvas alloc] initWithFrame:NSMakeRect(0,0,pin.frame.size.width,pin.frame.size.height)];
    canvas.owner=self;canvas.document=document;canvas.pinned=YES;
    pin.contentView=canvas;pin.captureCanvas=canvas;canvas.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;
    [self.pins addObject:pin];[pin orderFrontRegardless];
}
- (void)paste {
    if(self.overlay){[self.canvas pin];return;}
    if(self.stopped)return;
    NSImage *image=[[NSImage alloc] initWithPasteboard:NSPasteboard.generalPasteboard];if(!image)return;
    NSRect rect=NSMakeRect(0,0,image.size.width,image.size.height);
    if(rect.size.width<1||rect.size.height<1||rect.size.width>16384||rect.size.height>16384)return;
    BotCaptureDocument *doc=[BotCaptureDocument new];doc.image=image;doc.canvasSize=image.size;doc.selection=rect;
    for(NSImageRep *rep in image.representations)if(rep.pixelsWide>0)doc.pixelScale=MAX(doc.pixelScale,(CGFloat)rep.pixelsWide/image.size.width);
    doc.includeBackground=NO;doc.source=@"clipboard";
    [self pinDocument:doc at:NSEvent.mouseLocation];[self refresh];
}
- (void)togglePins {
    BOOL any=NO;for(NSWindow *pin in self.pins)if(pin.visible)any=YES;
    for(NSWindow *pin in self.pins){if(any){[((BotCaptureCanvas *)pin.contentView).toolbarPanel orderOut:nil];[pin orderOut:nil];}else [pin orderFrontRegardless];}
}
- (void)ask:(BotCaptureCanvas *)canvas {
    if(![self.availability[@"state"] isEqual:@"supported"]){[canvas updateAvailability];return;}
    BotCaptureDocument *doc=canvas.document;
    if([doc.outcome isEqual:@"unknown"]||[doc.outcome isEqual:@"accepted"]||NSIsEmptyRect(doc.selection))return;
    if(![doc noteWithinLimit]) {
        [canvas showMessage:[self text:@"capture.noteTooLong" fallback:@"Note is too long (maximum 4096 UTF-8 bytes). Shorten it and try again."]];return;
    }
    if(!canvas.restored) {
        NSError *error=nil;NSString *previousID=doc.identifier;
        if(![doc writeToRoot:self.root error:&error]){
            if(!previousID.length&&doc.identifier.length){
                NSString *partial=[self.root stringByAppendingPathComponent:doc.identifier];
                [NSFileManager.defaultManager removeItemAtPath:partial error:nil];doc.identifier=nil;
            }
            [canvas showMessage:[self text:@"capture.saveFailed" fallback:@"Could not save image"]];return;
        }
    }
    doc.outcome=@"unknown";
    if(!canvas.pinned){[self pinDocument:doc at:NSMakePoint(NSMidX(self.overlay.frame),NSMidY(self.overlay.frame))];[self finish:YES];}
    for(NSWindow *pin in self.pins) {
        BotCaptureCanvas *view=(BotCaptureCanvas *)pin.contentView;
        if(view.document==doc){[view buildToolbar];[view updateAvailability];}
    }
    if(self.handle)desktopCaptureEvent(self.handle,21,(char *)doc.identifier.UTF8String);
}
- (void)receipt:(NSDictionary *)result {
    for(NSWindow *pin in self.pins.copy) {
        BotCaptureCanvas *canvas=(BotCaptureCanvas *)pin.contentView;
        if(![canvas.document.identifier isEqual:result[@"id"]])continue;
        canvas.document.outcome=result[@"outcome"];
        if([result[@"outcome"] isEqual:@"draft"]) {
            canvas.document.identifier=nil;
            // Restored documents only retain the rendered selection in memory.
            if(canvas.restored){canvas.document.includeBackground=NO;canvas.restored=NO;}
            [canvas buildToolbar];
        }
        if([result[@"outcome"] isEqual:@"accepted"]){[canvas.toolbarPanel close];[pin close];[self.pins removeObject:(BotCapturePanel *)pin];}
        else {[canvas updateAvailability];[canvas showMessage:result[@"message"]];}
    }
}
- (void)restore:(NSArray *)records {
    for(NSDictionary *record in records) {
        NSDictionary *meta=record[@"snapshot"];NSString *identifier=meta[@"id"];
        BOOL exists=NO;for(NSWindow *pin in self.pins)if([((BotCaptureCanvas *)pin.contentView).document.identifier isEqual:identifier])exists=YES;
        if(exists)continue;
        NSString *path=[[self.root stringByAppendingPathComponent:identifier] stringByAppendingPathComponent:@"selection.png"];
        NSImage *image=[[NSImage alloc] initWithContentsOfFile:path];if(!image)continue;
        BotCaptureDocument *doc=[BotCaptureDocument new];doc.image=image;doc.canvasSize=image.size;doc.selection=NSMakeRect(0,0,image.size.width,image.size.height);
        doc.source=@"clipboard";doc.includeBackground=[meta[@"background"] boolValue];doc.identifier=identifier;doc.outcome=record[@"outcome"];doc.note=meta[@"note"]?:@"";
        [self pinDocument:doc at:NSScreen.mainScreen.visibleFrame.origin];
        BotCaptureCanvas *canvas=(BotCaptureCanvas *)self.pins.lastObject.contentView;canvas.restored=YES;[canvas buildToolbar];
    }
}
- (void)stop {
    self.stopped=YES;self.handle=0;if(self.keyMonitor){[NSEvent removeMonitor:self.keyMonitor];self.keyMonitor=nil;}[self.refreshTimer invalidate];[self finish:NO];
    for(NSWindow *pin in self.pins){[((BotCaptureCanvas *)pin.contentView).toolbarPanel close];[pin close];}[self.pins removeAllObjects];
    for(id observer in self.observers){[NSNotificationCenter.defaultCenter removeObserver:observer];[NSWorkspace.sharedWorkspace.notificationCenter removeObserver:observer];}
    [self.observers removeAllObjects];
}
@end

static id captureJSON(const char *json) {return [NSJSONSerialization JSONObjectWithData:[[NSString stringWithUTF8String:json] dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil];}
void *bot_capture_create(uintptr_t handle,const char *root) {BotCapture *c=[BotCapture new];c.handle=handle;c.root=[NSString stringWithUTF8String:root];return (__bridge_retained void *)c;}
void bot_capture_command(void *pointer,int command) {BotCapture *c=(__bridge BotCapture *)pointer;if(command==0)[c capture];else if(command==1)[c paste];else [c togglePins];}
void bot_capture_availability(void *pointer,const char *json) {[( __bridge BotCapture *)pointer setAvailability:captureJSON(json)];}
void bot_capture_receipt(void *pointer,const char *json) {[( __bridge BotCapture *)pointer receipt:captureJSON(json)];}
void bot_capture_restore(void *pointer,const char *json) {[( __bridge BotCapture *)pointer restore:captureJSON(json)];}
void bot_capture_stop(void *pointer) {BotCapture *c=(__bridge_transfer BotCapture *)pointer;[c stop];}

void bot_capture_preferences(void *pointer,int value) {[( __bridge BotCapture *)pointer setIncludeBackground:value!=0];}
int bot_capture_copy_image(const void *bytes,int length) {
    if(!bytes||length<=0||length>8*1024*1024)return 0;
    NSImage *image=[[NSImage alloc] initWithData:[NSData dataWithBytes:bytes length:length]];if(!image)return 0;
    [NSPasteboard.generalPasteboard clearContents];return [NSPasteboard.generalPasteboard writeObjects:@[image]];
}
