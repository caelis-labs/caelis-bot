//go:build darwin && cgo

#import "material_darwin.h"
#import <WebKit/WebKit.h>

// Public macOS 26 API, resolved at runtime so builds with older SDKs retain
// Liquid Glass on newer systems without linking an unavailable class symbol.
@protocol BotGlassEffect
- (void)setContentView:(NSView *)view;
- (void)setCornerRadius:(CGFloat)radius;
- (void)setStyle:(NSInteger)style;
@end

@interface BotMaterialView : NSView
@property CGFloat radius;
@property NSView *effect;
@property(weak) WKWebView *webView;
@property id accessibilityObserver;
@property NSString *kind;
- (void)refreshMaterial;
- (void)syncPage;
@end

@implementation BotMaterialView
- (NSView *)hitTest:(NSPoint)point { return nil; }
- (BOOL)isAccessibilityElement { return NO; }
- (void)refreshMaterial {
    [self.effect removeFromSuperview];
    BOOL solid = NSWorkspace.sharedWorkspace.accessibilityDisplayShouldReduceTransparency ||
        NSWorkspace.sharedWorkspace.accessibilityDisplayShouldIncreaseContrast;
    self.wantsLayer = YES;
    self.layer.cornerRadius = self.radius;
    self.layer.backgroundColor = solid ? NSColor.windowBackgroundColor.CGColor : NSColor.clearColor.CGColor;
    NSString *kind = @"solid";
    self.effect = nil;
    if (!solid) {
        Class glassClass = NSClassFromString(@"NSGlassEffectView");
        if (@available(macOS 26.0, *)) {
            if (glassClass) {
                NSView<BotGlassEffect> *glass = [[glassClass alloc] initWithFrame:self.bounds];
                [glass setCornerRadius:self.radius];
                // Regular (public enum value 0) adjusts backdrop luminance for
                // text. Clear prioritizes the desktop and is unsuitable here.
                [glass setStyle:0];
                [glass setContentView:[[NSView alloc] initWithFrame:glass.bounds]];
                self.effect = glass;
                kind = @"glass-regular";
            }
        }
        if (!self.effect) {
            NSVisualEffectView *material = [[NSVisualEffectView alloc] initWithFrame:self.bounds];
            material.material = NSVisualEffectMaterialPopover;
            material.blendingMode = NSVisualEffectBlendingModeBehindWindow;
            material.state = NSVisualEffectStateActive;
            material.wantsLayer = YES;
            material.layer.cornerRadius = self.radius;
            material.layer.masksToBounds = YES;
            self.effect = material;
            kind = @"vibrancy";
        }
        self.effect.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
        [self addSubview:self.effect];
    }
    self.kind = kind;
    [self syncPage];
}
- (void)syncPage {
    // The initial document may already be navigating when a WKUserScript is
    // installed. Apply again on didFinishNavigation, including renderer reloads.
    // Only style metadata enters diagnostics, never page text or conversation.
    NSString *script = [NSString stringWithFormat:@"(()=>{const e=document.querySelector('.input-surface,.message-bubble');const before=e?getComputedStyle(e).backgroundColor:'';document.documentElement.dataset.nativeMaterial='%@';return {surface:document.body?.dataset.surface||'',mode:document.documentElement.dataset.nativeMaterial,before,after:e?getComputedStyle(e).backgroundColor:''};})()",self.kind];
    BOOL trace = NSProcessInfo.processInfo.environment[@"CAELIS_BOT_DESKTOP_TRACE"].length > 0;
    NSString *kind = self.kind;
    NSInteger windowNumber = self.window.windowNumber;
    [self.webView evaluateJavaScript:script completionHandler:^(id result,NSError *error){
        if (trace) NSLog(@"Caelis material %@ window=%ld %@ error=%@",kind,(long)windowNumber,result,error.localizedDescription ?: @"none");
    }];
}
- (void)viewDidChangeEffectiveAppearance {
    [super viewDidChangeEffectiveAppearance];
    if (self.layer && !self.effect) self.layer.backgroundColor = NSColor.windowBackgroundColor.CGColor;
}
- (void)dealloc {
    if (self.accessibilityObserver) [NSWorkspace.sharedWorkspace.notificationCenter removeObserver:self.accessibilityObserver];
}
@end

void bot_install_material(NSWindow *window, CGFloat radius, CGFloat sidebarWidth, CGFloat inset) {
    NSView *root = window.contentView;
    // Remove only the previous decorative backdrop, preserving WebKit and its
    // native key-view / accessibility hierarchy as siblings of the material.
    for (NSView *view in root.subviews.copy) {
        if ([view isKindOfClass:NSVisualEffectView.class] || [view isKindOfClass:BotMaterialView.class]) [view removeFromSuperview];
    }
    WKWebView *webView = nil;
    NSMutableArray *views = [NSMutableArray arrayWithObject:root];
    while (views.count) {
        NSView *view = views.lastObject;
        [views removeLastObject];
        if ([view isKindOfClass:WKWebView.class]) { webView = (WKWebView *)view; break; }
        [views addObjectsFromArray:view.subviews];
    }
    BotMaterialView *material = [[BotMaterialView alloc] initWithFrame:NSInsetRect(root.bounds,inset,inset)];
    material.radius = radius;
    material.webView = webView;
    material.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    if (sidebarWidth > 0) {
        material.frame = NSMakeRect(8,8,sidebarWidth-16,MAX(0,root.bounds.size.height-16));
        material.autoresizingMask = NSViewHeightSizable | NSViewMaxXMargin;
    }
    [root addSubview:material positioned:NSWindowBelow relativeTo:nil];
    // Each host has one material. The startup script survives a webview reload;
    // live accessibility changes update the same attribute separately.
    NSString *script = @"document.documentElement.dataset.nativeMaterial='native'";
    [webView.configuration.userContentController addUserScript:[[WKUserScript alloc] initWithSource:script injectionTime:WKUserScriptInjectionTimeAtDocumentEnd forMainFrameOnly:YES]];
    __weak BotMaterialView *weak = material;
    material.accessibilityObserver = [NSWorkspace.sharedWorkspace.notificationCenter addObserverForName:NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) { [weak refreshMaterial]; }];
    [material refreshMaterial];
}

void bot_sync_material_pages(void) {
    for (NSWindow *window in NSApp.windows) {
        for (NSView *view in window.contentView.subviews) {
            if ([view isKindOfClass:BotMaterialView.class]) [(BotMaterialView *)view syncPage];
        }
    }
}
