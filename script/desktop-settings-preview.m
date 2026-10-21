#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import "../internal/desktop/native_darwin.h"
@interface DesktopSettingsPreview:NSObject<NSApplicationDelegate,WKScriptMessageHandler,NSWindowDelegate>
@property NSWindow *chat;
@property NSWindow *settings;
@property WKWebView *chatWeb;
@property WKWebView *settingsWeb;
@property NSString *url;
@property NSString *evidence;
@property NSMenu *captureMenu;
@property BOOL captureEnabled;
@property NSMutableArray *timings;
@property BOOL autorun;
@end
@implementation DesktopSettingsPreview
- (NSWindow *)window:(BOOL)settings {
 CGFloat chatWidth=640;const char *width=getenv("BOT_SETTINGS_PREVIEW_WIDTH");if(width)chatWidth=MAX(300,MIN(1400,atof(width)));
 NSWindow *window=[[NSWindow alloc] initWithContentRect:NSMakeRect(170,120,settings?1040:chatWidth,settings?740:700) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskResizable|NSWindowStyleMaskMiniaturizable backing:NSBackingStoreBuffered defer:NO];
 window.title=settings?@"Caelis Bot Dev — 设置（合成数据）":@"Caelis Bot Dev — 聊天（合成数据）";window.releasedWhenClosed=NO;window.delegate=self;
 WKWebViewConfiguration *config=[WKWebViewConfiguration new];[config.userContentController addScriptMessageHandler:self name:@"navigation"];
 WKWebView *web=[[WKWebView alloc] initWithFrame:window.contentView.bounds configuration:config];web.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;window.contentView=web;
 if(settings)self.settingsWeb=web;else self.chatWeb=web;

 [web loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:[self.url stringByAppendingFormat:@"?surface=%@&chrome=native",settings?@"settings":@"chat"]]]];
 return window;
}
- (void)applicationDidFinishLaunching:(NSNotification *)notification {
 self.timings=[NSMutableArray new];self.autorun=getenv("BOT_SETTINGS_AUTORUN")!=NULL;
 self.chat=[self window:NO];self.settings=[self window:YES];[self.chat makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
 NSMenu *main=[NSMenu new];NSMenuItem *root=[NSMenuItem new];[main addItem:root];NSMenu *menu=[NSMenu new];root.submenu=menu;
 NSArray *titles=@[@"Connections",@"Plugins",@"Slow refresh",@"Fail Codex refresh",@"Native navigation regression",@"Save evidence",@"Quit fixture",@"Chat read failure",@"Recover chat read",@"Unknown send fixture",@"Renderer failure"];
 for(NSUInteger i=0;i<titles.count;i++){NSMenuItem *item=[menu addItemWithTitle:titles[i] action:@selector(command:) keyEquivalent:@""];item.tag=i;item.target=self;}
 NSApp.mainMenu=main;
 NSMenuItem *captureRoot=[main addItemWithTitle:@"Capture menu fixture" action:nil keyEquivalent:@""];
 self.captureMenu=[NSMenu new];self.captureMenu.autoenablesItems=NO;captureRoot.submenu=self.captureMenu;
 self.captureEnabled=YES;
 NSMenuItem *toggle=[self.captureMenu addItemWithTitle:@"Toggle screenshot feature" action:@selector(toggleCapture:) keyEquivalent:@""];toggle.target=self;
 bot_capture_menu_add((__bridge void *)self.captureMenu,(__bridge void *)self,YES,"截图");
 if(self.autorun)dispatch_after(dispatch_time(DISPATCH_TIME_NOW,3*NSEC_PER_SEC),dispatch_get_main_queue(),^{[self regression];});
 // Exercise the same native item and separator used by the menu-bar host.
 NSCAssert(self.captureMenu.itemArray.count==3,@"exactly one screenshot command");
 bot_capture_menu_enabled((__bridge void *)self.captureMenu,NO);
 NSCAssert([self.captureMenu itemWithTag:2201].hidden && [self.captureMenu itemWithTag:2202].hidden,@"disabled capture hidden");
 bot_capture_menu_enabled((__bridge void *)self.captureMenu,YES);
 NSCAssert(![self.captureMenu itemWithTag:2201].hidden,@"enabled capture restored");
}
- (void)startCapture:(id)sender { /* Synthetic fixture never captures the screen. */ }
- (void)toggleCapture:(id)sender {
 self.captureEnabled=!self.captureEnabled;
 bot_capture_menu_enabled((__bridge void *)self.captureMenu,self.captureEnabled);
}
- (void)show:(BOOL)settings {
 NSWindow *next=settings?self.settings:self.chat;
 WKWebView *web=settings?self.settingsWeb:self.chatWeb;
 if(!next.visible)[web evaluateJavaScript:settings?@"window.dispatchEvent(new CustomEvent('settings-open',{detail:{section:'connections'}}))":@"window.dispatchEvent(new CustomEvent('history-open',{detail:{preservePosition:true}}))" completionHandler:nil];
 [next makeKeyAndOrderFront:nil];
}
- (void)userContentController:(WKUserContentController *)controller didReceiveScriptMessage:(WKScriptMessage *)message {
 if([message.body isEqual:@"settings"])[self show:YES];
 if([message.body isEqual:@"chat"])[self show:NO];
 if([message.body isEqual:@"hide"])[self windowShouldClose:message.webView==self.settingsWeb?self.settings:self.chat];
}
- (void)command:(NSMenuItem *)item {
 if(item.tag==6){[NSApp terminate:nil];return;}
 if(item.tag==5){[self capture];return;}
 if(item.tag==4){[self regression];return;}
 if(item.tag>=7){[self show:NO];NSArray *scripts=@[@"fixtureChatFailure(true)",@"fixtureChatFailure(false)",@"fixtureUnknownSend=true",@"window.dispatchEvent(new Event('fixture-render-failure'))"];
  [self.chatWeb evaluateJavaScript:scripts[item.tag-7] completionHandler:nil];return;}
 [self show:YES];
 NSArray *scripts=@[@"fixtureNavigate('connections')",@"fixtureNavigate('plugins')",@"fixtureRefreshDelay=5000;fixtureNavigate('connections');Array.from(document.querySelectorAll('button')).find(b=>b.textContent==='重新检测')?.click()",@"fixtureRuntimeFailure='codex';fixtureNavigate('connections');Array.from(document.querySelectorAll('button')).find(b=>b.textContent==='重新检测')?.click()"];
 [self.settingsWeb evaluateJavaScript:scripts[item.tag] completionHandler:nil];
}
- (void)sampleOpen:(NSInteger)index completion:(void (^)(void))completion {
 NSTimeInterval start=NSDate.date.timeIntervalSince1970*1000;
 [self.chatWeb evaluateJavaScript:@"document.querySelector('.chat-settings').click()" completionHandler:^(id value,NSError *error){
  NSTimeInterval click=NSDate.date.timeIntervalSince1970*1000-start;
  [self.settingsWeb evaluateJavaScript:@"JSON.stringify({body:!!document.querySelector('.settings-main'),knownConfiguration:!!document.querySelector('.runtime-model-summary[aria-busy=false]'),settingsOnlyReturn:!!document.querySelector('.settings-navigation')})" completionHandler:^(id result,NSError *err){
   [self.timings addObject:@{@"open":@(index),@"clickRoundTripMS":@(click),@"bodyReadbackMS":@(NSDate.date.timeIntervalSince1970*1000-start),@"windowVisible":@(self.settings.visible),@"chatStillVisible":@(self.chat.visible),@"body":result?:@""}];
   [self.settings performClose:nil];
   if(completion)completion();
  }];
 }];
}
- (void)regression {
 [self show:NO];
 [self.chatWeb evaluateJavaScript:@"(()=>{const scroll=document.querySelector('.chat-scroll');scroll.scrollTop=140;window.fixtureBefore={draft:document.querySelector('textarea')?.value,files:document.body.innerText.includes('fixture-notes.txt'),scroll:scroll.scrollTop};window.fixtureComposer=document.querySelector('textarea');return true})()" completionHandler:^(id before,NSError *error){
  [self sampleOpen:1 completion:^{[self sampleOpen:2 completion:^{
   [self.chatWeb evaluateJavaScript:@"JSON.stringify({before:fixtureBefore,after:{draft:document.querySelector('textarea')?.value,files:document.body.innerText.includes('fixture-notes.txt'),scroll:document.querySelector('.chat-scroll').scrollTop},sameComposer:fixtureComposer===document.querySelector('textarea')})" completionHandler:^(id result,NSError *err){
    if(result)[result writeToFile:[self.evidence stringByAppendingPathComponent:@"navigation.json"] atomically:YES encoding:NSUTF8StringEncoding error:nil];
    NSData *timing=[NSJSONSerialization dataWithJSONObject:self.timings options:NSJSONWritingPrettyPrinted error:nil];[timing writeToFile:[self.evidence stringByAppendingPathComponent:@"timings.json"] atomically:YES];
    [self capture];
    [self show:YES];[self.settingsWeb evaluateJavaScript:@"fixtureNavigate('permissions')" completionHandler:nil];
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW,NSEC_PER_SEC),dispatch_get_main_queue(),^{[self capture];
     if(self.autorun)dispatch_after(dispatch_time(DISPATCH_TIME_NOW,NSEC_PER_SEC),dispatch_get_main_queue(),^{[NSApp terminate:nil];});
    });
   }];
  }];}];
 }];
}
- (void)capture {
 BOOL settings=self.settings.visible;WKWebView *web=settings?self.settingsWeb:self.chatWeb;NSString *name=settings?@"settings":@"chat";
 [web takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image,NSError *error){if(!image)return;NSBitmapImageRep *bitmap=[[NSBitmapImageRep alloc] initWithData:image.TIFFRepresentation];[[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:[self.evidence stringByAppendingPathComponent:[name stringByAppendingString:@".png"]] atomically:YES];}];
 [web evaluateJavaScript:@"JSON.stringify({text:document.body.innerText,calls:fixtureCalls,overflow:document.documentElement.scrollWidth>innerWidth,composerFeedback:document.querySelectorAll('footer [data-transient-feedback],footer .surface-recovery').length,messageFeedback:document.querySelectorAll('.chat-content [data-transient-feedback]').length,messages:document.querySelectorAll('[data-message-id]').length,draft:document.querySelector('textarea')?.value,files:document.querySelectorAll('.draft-file').length})" completionHandler:^(id result,NSError *error){if(result)[result writeToFile:[self.evidence stringByAppendingPathComponent:[name stringByAppendingString:@".json"]] atomically:YES encoding:NSUTF8StringEncoding error:nil];}];
}
- (BOOL)windowShouldClose:(NSWindow *)window {
 WKWebView *web=window==self.settings?self.settingsWeb:self.chatWeb;
 [web evaluateJavaScript:window==self.settings?@"window.dispatchEvent(new Event('settings-close'))":@"window.dispatchEvent(new Event('history-close'))" completionHandler:nil];
 [window orderOut:nil];return NO;
}
@end
int main(int argc,char **argv){@autoreleasepool{if(argc!=3)return 2;NSApplication *app=NSApplication.sharedApplication;[app setActivationPolicy:NSApplicationActivationPolicyRegular];DesktopSettingsPreview *preview=[DesktopSettingsPreview new];preview.url=[NSString stringWithUTF8String:argv[1]];preview.evidence=[NSString stringWithUTF8String:argv[2]];app.delegate=preview;[app run];}return 0;}
