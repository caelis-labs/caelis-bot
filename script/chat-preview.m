// Actual production History/Markdown in a native WKWebView, synthetic data only.
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
@interface ChatPreview:NSObject<NSApplicationDelegate,NSWindowDelegate>
@property NSWindow *window;
@property WKWebView *web;
@property NSString *url;
@property NSString *capturePath;
@end
@implementation ChatPreview
- (void)applicationDidFinishLaunching:(NSNotification *)note {
 self.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(240,100,760,824) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskResizable backing:NSBackingStoreBuffered defer:NO];
 self.window.title=@"Caelis Chat Preview — 聊天流式验收";self.window.delegate=self;self.window.releasedWhenClosed=NO;
 self.web=[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,760,672)];self.web.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;
 [self.window.contentView addSubview:self.web];
 NSArray *names=@[@"短段流式",@"大块流式",@"一次完成",@"完成前大块",@"重开聊天",@"保存截图",@"打瞌睡",@"醒来",@"打盹审批",@"桌宠打盹",@"返回聊天",@"气泡打盹",@"思考动作",@"查找动作",@"读取动作",@"流式与工具",@"完成动作",@"头像回归",@"发送回归",@"图片预览"];
 for(NSUInteger i=0;i<names.count;i++){
  NSButton *button=[NSButton buttonWithTitle:names[i] target:self action:@selector(select:)];button.tag=i;
  button.frame=NSMakeRect(12+(i%6)*122,682+(i/6)*34,116,28);button.autoresizingMask=NSViewMinYMargin;[self.window.contentView addSubview:button];
 }
 [self.web loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:self.url]]];
 [self.window makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
}
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
 [self.web evaluateJavaScript:@"JSON.stringify({send:window.fixtureSendResult,avatar:window.fixtureAvatarResult,portraits:Array.from(document.querySelectorAll('[data-portrait]')).map(e=>({clip:e.dataset.portrait,frame:e.querySelector('canvas')?.dataset.frame})),media:Array.from(document.querySelectorAll('.media-thumbnail img')).map(i=>({width:i.naturalWidth,height:i.naturalHeight})),working:document.querySelector('.working-message')?.textContent,frames:window.fixtureFrames,expected:window.fixtureExpected,html:document.querySelector('[data-message-id=\"'+window.fixtureTarget+'\"] .markdown-body')?.innerHTML})" completionHandler:^(id result,NSError *error){
  if(result)[result writeToFile:[self.capturePath stringByAppendingString:@".json"] atomically:YES encoding:NSUTF8StringEncoding error:nil];
 }];
}
- (BOOL)windowShouldClose:(NSWindow *)sender { [NSApp terminate:nil];return YES; }
@end
int main(int argc,char **argv){@autoreleasepool{
 if(argc!=3)return 2;
 NSApplication *app=NSApplication.sharedApplication;[app setActivationPolicy:NSApplicationActivationPolicyRegular];
 ChatPreview *preview=[ChatPreview new];preview.url=[NSString stringWithUTF8String:argv[1]];preview.capturePath=[NSString stringWithUTF8String:argv[2]];app.delegate=preview;[app run];
}return 0;}
