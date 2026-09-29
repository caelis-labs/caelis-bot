// Native WebKit fixture using the production bundle and shared placement policy.
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#include "bubble_layout.h"
#import "material_darwin.h"
@interface PreviewPanel:NSPanel @end
@implementation PreviewPanel
- (BOOL)canBecomeKeyWindow { return NO; }
@end
@interface Preview:NSObject<NSApplicationDelegate,WKScriptMessageHandler,WKNavigationDelegate,NSWindowDelegate>
@property NSWindow *controls;
@property PreviewPanel *panel;
@property WKWebView *web;
@property NSTextField *status;
@property double height;
@property BOOL top;
@property NSString *url;
@property NSString *capturePath;
@end
@implementation Preview
- (void)place {
 NSRect b=NSScreen.mainScreen.visibleFrame;
 NSRect pet=NSMakeRect(NSMidX(b)+60,self.top?NSMaxY(b)-250:NSMinY(b)+90,180,240);
 BotBubbleLayout l=bot_bubble_layout(pet.origin.x,pet.origin.y,pet.size.width,pet.size.height,b.origin.x,b.origin.y,b.size.width,b.size.height,self.height);
 [self.web evaluateJavaScript:[NSString stringWithFormat:@"document.documentElement.style.setProperty('--bubble-max-height','%gpx')",l.maxHeight] completionHandler:nil];
 [self.panel setFrame:NSMakeRect(l.x,l.y,l.width,l.height) display:YES];
 self.status.stringValue=[NSString stringWithFormat:@"气泡 %.0f pt / 上限 %.0f pt · 非激活窗口",l.height,l.maxHeight];
}
- (void)applicationDidFinishLaunching:(NSNotification *)note {
 self.controls=[[NSWindow alloc] initWithContentRect:NSMakeRect(100,160,390,376) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable backing:NSBackingStoreBuffered defer:NO];
 self.controls.title=@"Caelis Bubble Preview — 合成验收";self.controls.delegate=self;self.controls.releasedWhenClosed=NO;
 NSArray *names=@[@"短消息",@"长正文",@"读文件",@"网络搜索",@"执行命令",@"编辑文件",@"自动审查",@"审批",@"已完成",@"停止中",@"切换顶部",@"展开 / 收起",@"Issue 表格",@"流式打字",@"跟随系统",@"浅色",@"深色",@"实色回退",@"审批恢复回归"];
 for(NSUInteger i=0;i<names.count;i++) {
  NSButton *button=[NSButton buttonWithTitle:names[i] target:self action:@selector(select:)];button.tag=i;
  button.frame=NSMakeRect(12+(i%3)*124,332-(i/3)*42,118,32);[self.controls.contentView addSubview:button];
 }
 NSButton *capture=[NSButton buttonWithTitle:@"保存气泡截图" target:self action:@selector(capture:)];capture.frame=NSMakeRect(12,40,150,30);[self.controls.contentView addSubview:capture];
 self.status=[NSTextField labelWithString:@"Loading…"];self.status.frame=NSMakeRect(16,18,360,24);[self.controls.contentView addSubview:self.status];
 self.panel=[[PreviewPanel alloc] initWithContentRect:NSMakeRect(500,400,360,68) styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
 self.panel.title=@"Caelis 消息气泡";self.panel.opaque=NO;self.panel.backgroundColor=NSColor.clearColor;self.panel.level=NSFloatingWindowLevel;self.panel.hidesOnDeactivate=NO;
 WKWebViewConfiguration *configuration=[WKWebViewConfiguration new];[configuration.userContentController addScriptMessageHandler:self name:@"preview"];
 self.web=[[WKWebView alloc] initWithFrame:self.panel.contentView.bounds configuration:configuration];
 [self.web setValue:@NO forKey:@"drawsBackground"];self.web.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;self.web.navigationDelegate=self;
 NSView *surface=[[NSView alloc] initWithFrame:self.panel.contentView.bounds];[surface addSubview:self.web];self.panel.contentView=surface;
 bot_install_bubble_material(self.panel,28,6);
 [self.web loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:self.url]]];
 [self.controls makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
}
- (void)webView:(WKWebView *)web didFinishNavigation:(WKNavigation *)navigation { bot_sync_material_pages();[self place];[self.panel orderFrontRegardless]; }
- (void)userContentController:(WKUserContentController *)controller didReceiveScriptMessage:(WKScriptMessage *)message {
 NSDictionary *body=message.body;NSString *method=body[@"method"];
 if([method isEqual:@"SetBubbleHeight"]) { self.height=[body[@"args"][0] doubleValue];[self place]; }
 else if([method isEqual:@"BubbleReplayResult"]) {
  NSDictionary *result=body[@"result"];self.status.stringValue=[result[@"ok"] boolValue]?@"PASS：审批恢复与末尾打字回归":result[@"error"];
  NSData *data=[NSJSONSerialization dataWithJSONObject:result options:NSJSONWritingPrettyPrinted error:nil];
  [data writeToFile:[self.capturePath stringByAppendingString:@".replay.json"] atomically:YES];
 }
 else if([method isEqual:@"SetBubbleVisible"]) { if([body[@"args"][0] boolValue])[self.panel orderFrontRegardless];else[self.panel orderOut:nil]; }
 else if([method isEqual:@"OpenHistory"] || [method isEqual:@"OpenMessageLink"] || [method isEqual:@"CopyText"]) self.status.stringValue=[@"已捕获：" stringByAppendingString:method];
}
- (void)select:(NSButton *)button {
 if(button.tag==18){[self.web evaluateJavaScript:@"fixtureBubbleReplay().then(result=>window.webkit.messageHandlers.preview.postMessage({method:'BubbleReplayResult',result}))" completionHandler:nil];return;}
 if(button.tag>=14){
  if(button.tag==17){[self.web evaluateJavaScript:@"document.documentElement.dataset.nativeMaterial='solid'" completionHandler:nil];return;}
  NSAppearance *appearance=button.tag==14?nil:[NSAppearance appearanceNamed:button.tag==15?NSAppearanceNameAqua:NSAppearanceNameDarkAqua];
  self.controls.appearance=appearance;self.panel.appearance=appearance;bot_sync_material_pages();return;
 }
 if(button.tag==10){self.top=!self.top;[self place];return;}
 if(button.tag==12 || button.tag==13){[self.web evaluateJavaScript:button.tag==12?@"window.fixtureSet('table')":@"window.fixtureSet('stream')" completionHandler:nil];return;}
 if(button.tag==11){[self.web evaluateJavaScript:@"document.querySelector('.bubble-shell').dispatchEvent(new MouseEvent(document.querySelector('.message-bubble').classList.contains('reading')?'mouseout':'mouseover',{bubbles:true}))" completionHandler:nil];return;}
 NSArray *scenes=@[@"short",@"long",@"read",@"web",@"execute",@"edit",@"review",@"approval",@"completed",@"stop"];
 [self.web evaluateJavaScript:[NSString stringWithFormat:@"window.fixtureSet('%@')",scenes[button.tag]] completionHandler:nil];
}
- (void)capture:(id)sender {
 [self.web takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image,NSError *error){
  if(!image){self.status.stringValue=error.localizedDescription;return;}
  NSBitmapImageRep *bitmap=[[NSBitmapImageRep alloc] initWithData:image.TIFFRepresentation];
  [[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:self.capturePath atomically:YES];
  [self.web evaluateJavaScript:@"JSON.stringify({material:document.documentElement.dataset.nativeMaterial,fill:getComputedStyle(document.querySelector('.message-bubble')).backgroundColor,expanded:document.querySelector('.message-bubble').classList.contains('reading'),height:document.querySelector('.bubble-shell').getBoundingClientRect().height,viewport:innerHeight,scrollHeight:document.querySelector('.bubble-copy').scrollHeight,clientHeight:document.querySelector('.bubble-copy').clientHeight,html:!!document.querySelector('.bubble-copy strong'),code:!!document.querySelector('.bubble-copy code'),table:!!document.querySelector('.bubble-copy table'),complete:document.querySelector('.bubble-copy').textContent.includes('END'),textWidth:document.querySelector('.bubble-copy').getBoundingClientRect().width,actionsWidth:document.querySelector('.bubble-actions').getBoundingClientRect().width,distinctFrames:new Set(window.fixtureFrames).size})" completionHandler:^(id result,NSError *error){
   if(result)[result writeToFile:[self.capturePath stringByAppendingString:@".json"] atomically:YES encoding:NSUTF8StringEncoding error:nil];
  }];
  self.status.stringValue=@"已保存原生气泡截图和布局数据";
 }];
}
- (BOOL)windowShouldClose:(NSWindow *)sender { [NSApp terminate:nil];return YES; }
@end
int main(int argc,char **argv) { @autoreleasepool {
 if(argc!=3)return 2;
 NSApplication *app=NSApplication.sharedApplication;[app setActivationPolicy:NSApplicationActivationPolicyRegular];
 Preview *preview=[Preview new];preview.url=[NSString stringWithUTF8String:argv[1]];preview.capturePath=[NSString stringWithUTF8String:argv[2]];app.delegate=preview;[app run];
}return 0;}
