// Actual production History/Markdown in a native WKWebView, synthetic data only.
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
@interface ChatPreview:NSObject<NSApplicationDelegate,NSWindowDelegate,WKScriptMessageHandler>
@property NSWindow *window;
@property WKWebView *web;
@property NSString *url;
@property NSString *capturePath;
@property BOOL autoCapture;
@property BOOL dark;
@end
@implementation ChatPreview
- (void)applicationDidFinishLaunching:(NSNotification *)note {
 BOOL question=[[NSURL URLWithString:self.url].query containsString:@"fixture=question-lifecycle"];
 BOOL a2ui=[[NSURL URLWithString:self.url].query containsString:@"fixture=a2ui"];
 BOOL media=[[NSURL URLWithString:self.url].query containsString:@"fixture=chat-media"]||[[NSURL URLWithString:self.url].query containsString:@"fixture=chat-combined"]||[[NSURL URLWithString:self.url].query containsString:@"fixture=chat-local-files"];
 CGFloat width=760,height=672;
 for(NSURLQueryItem *item in [NSURLComponents componentsWithString:self.url].queryItems){if([item.name isEqual:@"width"])width=MAX(300,item.value.doubleValue);if([item.name isEqual:@"height"])height=MAX(400,item.value.doubleValue);}
 self.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(240,100,width,height+(question||a2ui||media?0:152)) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskResizable backing:NSBackingStoreBuffered defer:NO];
 self.window.title=question?@"Caelis Bot Dev — 问题验收":@"Caelis Chat Preview — 聊天流式验收";self.window.delegate=self;self.window.releasedWhenClosed=NO;
 self.window.appearance=[NSAppearance appearanceNamed:self.dark?NSAppearanceNameDarkAqua:NSAppearanceNameAqua];
 WKWebViewConfiguration *configuration=[WKWebViewConfiguration new];
 if(a2ui)[configuration.userContentController addScriptMessageHandler:self name:@"historyLifecycle"];
 if(media)[configuration.userContentController addScriptMessageHandler:self name:@"mediaKeyboard"];
 self.web=[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,width,height) configuration:configuration];self.web.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;
 self.web.appearance=self.window.appearance;
 [self.window.contentView addSubview:self.web];
  NSArray *names=@[@"短段流式",@"大块流式",@"一次完成",@"完成前大块",@"重开聊天",@"保存截图",@"打瞌睡",@"醒来",@"打盹审批",@"桌宠打盹",@"返回聊天",@"气泡打盹",@"思考动作",@"查找动作",@"读取动作",@"流式与工具",@"完成动作",@"头像回归",@"发送回归",@"图片预览"];
  for(NSUInteger i=0;!question&&!a2ui&&!media&&i<names.count;i++){
   NSButton *button=[NSButton buttonWithTitle:names[i] target:self action:@selector(select:)];button.tag=i;
   button.frame=NSMakeRect(12+(i%6)*122,682+(i/6)*34,116,28);button.autoresizingMask=NSViewMinYMargin;[self.window.contentView addSubview:button];
  }
 if(question){
  NSMenu *main=[NSMenu new];NSMenuItem *root=[NSMenuItem new];[main addItem:root];
  NSMenu *menu=[NSMenu new];root.submenu=menu;
  NSMenuItem *save=[[NSMenuItem alloc] initWithTitle:@"Save question screenshot" action:@selector(capture) keyEquivalent:@"s"];save.target=self;[menu addItem:save];
  NSMenuItem *reset=[[NSMenuItem alloc] initWithTitle:@"Reset question fixture" action:@selector(resetQuestion) keyEquivalent:@"r"];reset.target=self;[menu addItem:reset];
  NSMenuItem *regression=[[NSMenuItem alloc] initWithTitle:@"Verify question layout and actions" action:@selector(verifyQuestion) keyEquivalent:@"t"];regression.target=self;[menu addItem:regression];
  for(NSString *key in @[@"m",@"l",@"f",@"d",@"g",@"n",@"w",@"a",@"u"]){NSMenuItem *i=[[NSMenuItem alloc] initWithTitle:[@"Review " stringByAppendingString:key] action:@selector(reviewCase:) keyEquivalent:key];i.target=self;[menu addItem:i];}
  NSApp.mainMenu=main;
 }
 [self.web loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:self.url]]];
 [self.window makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
 if(self.autoCapture){
  if(a2ui||media)[self waitForFixture:[NSDate dateWithTimeIntervalSinceNow:30]];
  else dispatch_after(dispatch_time(DISPATCH_TIME_NOW,3*NSEC_PER_SEC),dispatch_get_main_queue(),^{[self captureAndExit];});
 }
}
- (void)waitForFixture:(NSDate *)deadline {
 [self.web evaluateJavaScript:@"typeof window.fixtureExpected?.ok === 'boolean'" completionHandler:^(id ready,NSError *error){
  if([ready boolValue] || deadline.timeIntervalSinceNow <= 0){[self captureAndExit];return;}
  dispatch_after(dispatch_time(DISPATCH_TIME_NOW,200*NSEC_PER_MSEC),dispatch_get_main_queue(),^{[self waitForFixture:deadline];});
 }];
}
- (void)captureAndExit {
 [self capture];
 dispatch_after(dispatch_time(DISPATCH_TIME_NOW,700*NSEC_PER_MSEC),dispatch_get_main_queue(),^{[NSApp terminate:nil];});
}
- (void)userContentController:(WKUserContentController *)controller didReceiveScriptMessage:(WKScriptMessage *)message {
 if([message.name isEqual:@"mediaKeyboard"]){
  if([message.body isEqual:@"reopen"]){
   [self.window orderOut:nil];
   NSString *script=[NSString stringWithFormat:@"window.fixtureMediaHidden=%@;window.dispatchEvent(new Event('history-close'))",self.window.isVisible?@"false":@"true"];
   [self.web evaluateJavaScript:script completionHandler:nil];
   return;
  }
  if([message.body isEqual:@"reopen-ready"]){
   [self.window makeKeyAndOrderFront:nil];
   NSString *script=[NSString stringWithFormat:@"window.fixtureMediaWindow={hidden:window.fixtureMediaHidden,visible:%@};window.dispatchEvent(new Event('history-open'))",self.window.isVisible?@"true":@"false"];
   [self.web evaluateJavaScript:script completionHandler:nil];
   return;
  }
  BOOL enter=[message.body isEqual:@"enter"];
  NSString *key=enter?@"\r":@"\x1b";
  NSEvent *event=[NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint modifierFlags:0 timestamp:NSProcessInfo.processInfo.systemUptime windowNumber:self.window.windowNumber context:nil characters:key charactersIgnoringModifiers:key isARepeat:NO keyCode:enter?36:53];
  [self.window sendEvent:event];
  return;
 }
 if(![message.name isEqual:@"historyLifecycle"]||![message.body isEqual:@"hide-and-reopen"])return;
 [self.web evaluateJavaScript:@"window.dispatchEvent(new Event('history-close'))" completionHandler:nil];
 [self.window orderOut:nil];
 BOOL hidden=!self.window.isVisible;
 dispatch_after(dispatch_time(DISPATCH_TIME_NOW,300*NSEC_PER_MSEC),dispatch_get_main_queue(),^{
  [self.web evaluateJavaScript:@"!!document.querySelector('[data-message-id=chat-a2ui-fixture]') && !document.querySelector('.chat-a2ui-surface:not([hidden])')" completionHandler:^(id released,NSError *error){
   [self.window makeKeyAndOrderFront:nil];
   NSString *script=[NSString stringWithFormat:@"window.fixtureVisibility={hidden:%@,visible:%@,released:%@};window.dispatchEvent(new Event('history-open'))",
    hidden?@"true":@"false",self.window.isVisible?@"true":@"false",!error&&[released boolValue]?@"true":@"false"];
   [self.web evaluateJavaScript:script completionHandler:nil];
  }];
 });
}
- (void)reviewCase:(NSMenuItem*)item { NSString *key=item.keyEquivalent;
 if([key isEqual:@"n"]||[key isEqual:@"w"]){[self.window setContentSize:NSMakeSize([key isEqual:@"n"]?390:760,680)];return;}
 if([key isEqual:@"a"]){[self.web evaluateJavaScript:@"fixtureReviewApproval()" completionHandler:nil];return;}
 if([key isEqual:@"u"]){[self.web evaluateJavaScript:@"fixtureReplyOutcome='unknown';fixtureQuestionSetup()" completionHandler:nil];return;}
 if([key isEqual:@"d"]){self.dark=!self.dark;self.window.appearance=[NSAppearance appearanceNamed:self.dark?NSAppearanceNameDarkAqua:NSAppearanceNameAqua];self.web.appearance=self.window.appearance;return;} NSString *js=[key isEqual:@"m"]?@"fixtureReviewMulti()":[key isEqual:@"l"]?@"fixtureReviewLong()":[key isEqual:@"g"]?@"fixtureReviewCombined()":@"fixtureReviewFree()";[self.web evaluateJavaScript:js completionHandler:nil]; }
- (void)resetQuestion { [self.web evaluateJavaScript:@"fixtureReplyOutcome='';fixtureQuestionSetup()" completionHandler:nil]; }
- (void)verifyQuestion { [self.web evaluateJavaScript:@"fixtureQuestionRegression()" completionHandler:nil]; }
- (void)select:(NSButton *)button {
 if(button.tag==5){[self capture];return;}
 if(button.tag==18){[self.web evaluateJavaScript:@"fixtureSendRegression()" completionHandler:nil];return;}
 if(button.tag==19){[self.web evaluateJavaScript:@"fixtureMedia()" completionHandler:nil];return;}
 if(button.tag>=12){
  NSArray *scripts=@[@"fixtureAvatar('thinking')",@"fixtureAvatar('search')",@"fixtureAvatar('read')",@"fixtureAvatar('stream')",@"fixtureAvatar('done')",@"fixtureAvatarRegression()"];
  [self.web evaluateJavaScript:scripts[button.tag-12] completionHandler:nil];return;
 }
 if(button.tag>=9){
  NSString *surface=button.tag==9?@"pet":button.tag==10?@"history":@"bubble";
  NSURLComponents *url=[NSURLComponents componentsWithString:self.url];url.query=[NSString stringWithFormat:@"surface=%@&dream=1",surface];
  self.web.frame=button.tag==9?NSMakeRect(245,160,270,360):NSMakeRect(0,0,760,672);
  [self.web loadRequest:[NSURLRequest requestWithURL:url.URL]];return;
 }
 NSArray *scripts=@[@"fixtureChat('stream')",@"fixtureChat('burst')",@"fixtureChat('instant')",@"fixtureChat('final')",@"fixtureReopen()",@"",@"fixtureDream('running')",@"fixtureDream('done')",@"fixtureDream('approval')"];
 [self.web evaluateJavaScript:scripts[button.tag] completionHandler:nil];
}
- (void)capture {
 [self.web takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image,NSError *error){
  if(!image)return;
  NSBitmapImageRep *bitmap=[[NSBitmapImageRep alloc] initWithData:image.TIFFRepresentation];
  [[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:self.capturePath atomically:YES];
 }];
 [self.web evaluateJavaScript:@"JSON.stringify({text:document.body.innerText,question:window.fixtureQuestionResult,questionChecks:window.fixtureQuestionChecks,questionLayout:window.fixtureQuestionLayout?.(),questionText:document.querySelector('.question-prompt')?.innerText,questionOverflow:Array.from(document.querySelectorAll('.question-prompt, .approval-bubble')).some(e=>e.scrollWidth>e.clientWidth+1),send:window.fixtureSendResult,mediaChecks:window.fixtureMediaChecks,mediaActions:window.fixtureMediaActions,avatar:window.fixtureAvatarResult,portraits:Array.from(document.querySelectorAll('[data-portrait]')).map(e=>({clip:e.dataset.portrait,frame:e.querySelector('canvas')?.dataset.frame})),media:Array.from(document.querySelectorAll('.media-thumbnail img')).map(i=>({width:i.naturalWidth,height:i.naturalHeight})),working:document.querySelector('.working-message')?.textContent,frames:window.fixtureFrames,expected:window.fixtureExpected,html:document.querySelector('[data-message-id=\"'+window.fixtureTarget+'\"] .markdown-body')?.innerHTML})" completionHandler:^(id result,NSError *error){
  if(result)[result writeToFile:[self.capturePath stringByAppendingString:@".json"] atomically:YES encoding:NSUTF8StringEncoding error:nil];
 }];
}
- (BOOL)windowShouldClose:(NSWindow *)sender { [NSApp terminate:nil];return YES; }
@end
int main(int argc,char **argv){@autoreleasepool{
 if(argc<3||argc>5)return 2;
 NSApplication *app=NSApplication.sharedApplication;[app setActivationPolicy:NSApplicationActivationPolicyRegular];
 ChatPreview *preview=[ChatPreview new];preview.url=[NSString stringWithUTF8String:argv[1]];preview.capturePath=[NSString stringWithUTF8String:argv[2]];preview.autoCapture=argc>=4&&strcmp(argv[3],"auto")==0;preview.dark=argc>=5&&strcmp(argv[4],"dark")==0;app.delegate=preview;[app run];
}return 0;}
