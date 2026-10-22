// Isolated native WKWebView acceptance; only the transport is synthetic.
#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
@interface InputMenuPreview:NSObject<NSApplicationDelegate,NSWindowDelegate,WKNavigationDelegate>
@property NSWindow *window;
@property WKWebView *web;
@property NSString *url;
@property NSString *evidence;
@property NSArray *cases;
@property NSUInteger index;
@property BOOL autorun;
@end
@implementation InputMenuPreview
- (void)applicationDidFinishLaunching:(NSNotification *)note {
 self.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(160,100,760,720) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskResizable backing:NSBackingStoreBuffered defer:NO];
 self.window.title=@"Caelis Input Menu Preview — isolated synthetic data";self.window.delegate=self;self.window.releasedWhenClosed=NO;
 self.web=[[WKWebView alloc] initWithFrame:self.window.contentView.bounds];self.web.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;self.web.navigationDelegate=self;self.window.contentView=self.web;
 NSMenu *main=[NSMenu new];NSMenuItem *item=[main addItemWithTitle:@"Fixture" action:nil keyEquivalent:@""];NSMenu *menu=[NSMenu new];item.submenu=menu;
 NSArray *titles=@[@"Save evidence",@"Chat",@"Onboarding",@"Settings",@"Run all regressions",@"Quit fixture"];
 for(NSUInteger i=0;i<titles.count;i++){NSMenuItem *command=[menu addItemWithTitle:titles[i] action:@selector(command:) keyEquivalent:i==0?@"s":i==4?@"t":i==5?@"q":@""];command.tag=i;command.target=self;}
 NSMenuItem *editItem=[main addItemWithTitle:@"Edit" action:nil keyEquivalent:@""];NSMenu *edit=[NSMenu new];editItem.submenu=edit;
 [edit addItemWithTitle:@"Undo" action:@selector(undo:) keyEquivalent:@"z"];[edit addItemWithTitle:@"Cut" action:@selector(cut:) keyEquivalent:@"x"];[edit addItemWithTitle:@"Copy" action:@selector(copy:) keyEquivalent:@"c"];[edit addItemWithTitle:@"Paste" action:@selector(paste:) keyEquivalent:@"v"];[edit addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];NSApp.mainMenu=main;
 NSMutableArray *cases=[NSMutableArray arrayWithObject:@{ @"view":@"chat",@"width":@760,@"fallback":@NO }];
 for(NSNumber *width in @[@1040,@390])for(NSString *view in @[@"onboarding",@"settings"])for(NSNumber *fallback in @[@NO,@YES])[cases addObject:@{@"view":view,@"width":width,@"fallback":fallback}];self.cases=cases;
 [self load:@"chat" fallback:NO];[self.window makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
}
- (void)load:(NSString *)view fallback:(BOOL)fallback { [self.web loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:[NSString stringWithFormat:@"%@?view=%@%@",self.url,view,fallback?@"&fallback=1":@""]]]]; }
- (void)command:(NSMenuItem *)item {
 if(item.tag==5){[NSApp terminate:nil];return;}
 if(item.tag==0){[self capture:@"manual" completion:nil];return;}
 if(item.tag==4){self.autorun=YES;self.index=0;[self next];return;}
 [self load:@[@"",@"chat",@"onboarding",@"settings"][item.tag] fallback:NO];
}
- (void)next {
 if(self.index>=self.cases.count){self.autorun=NO;[self load:@"chat" fallback:NO];return;}
 NSDictionary *test=self.cases[self.index];[self.window setContentSize:NSMakeSize([test[@"width"] doubleValue],760)];[self load:test[@"view"] fallback:[test[@"fallback"] boolValue]];
}
- (void)webView:(WKWebView *)web didFinishNavigation:(WKNavigation *)navigation {
 if(!self.autorun)return;
 NSDictionary *test=self.cases[self.index];NSString *name=[NSString stringWithFormat:@"%@-%@-%@",test[@"view"],test[@"width"],[test[@"fallback"] boolValue]?@"fallback":@"popover"];
 NSString *function=[test[@"view"] isEqual:@"chat"]?@"runCompositionRegression":@"runMenuRegression";
 NSString *script=[NSString stringWithFormat:@"try { for(let i=0;i<300&&!window.uiFixtureReady;i++)await new Promise(r=>setTimeout(r,20)); return await window.%@(); } catch(error) { return {ok:false,error:String(error),stack:error.stack}; }",function];
 [web callAsyncJavaScript:script arguments:@{} inFrame:nil inContentWorld:WKContentWorld.pageWorld completionHandler:^(id result,NSError *error){
  NSDictionary *record=@{@"case":test,@"ok":@(!error&&[result isKindOfClass:NSDictionary.class]&&[result[@"ok"] boolValue]),@"result":result?:@{},@"error":error.userInfo.description?:@""};
  NSData *data=[NSJSONSerialization dataWithJSONObject:record options:NSJSONWritingPrettyPrinted error:nil];[data writeToFile:[self.evidence stringByAppendingPathComponent:[name stringByAppendingString:@".json"]] atomically:YES];
  [web evaluateJavaScript:@"if(document.querySelector('.composer-editor'))window.showCompositionPreview();else document.querySelector('.settings-select-trigger')?.click()" completionHandler:^(id value,NSError *err){[self capture:name completion:^{self.index++;[self next];}];}];
 }];
}
- (void)capture:(NSString *)name completion:(void (^)(void))done {
 [self.web evaluateJavaScript:@"JSON.stringify({nativePopover:typeof HTMLElement.prototype.showPopover==='function',state:window.fixtureUIState?.(),events:window.recordedInputEvents})" completionHandler:^(id value,NSError *error){if(value)[value writeToFile:[self.evidence stringByAppendingPathComponent:[name stringByAppendingString:@"-state.json"]] atomically:YES encoding:NSUTF8StringEncoding error:nil];}];
 [self.web takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image,NSError *error){if(image){NSBitmapImageRep *bitmap=[[NSBitmapImageRep alloc] initWithData:image.TIFFRepresentation];[[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:[self.evidence stringByAppendingPathComponent:[name stringByAppendingString:@".png"]] atomically:YES];}if(done)done();}];
}
- (BOOL)windowShouldClose:(NSWindow *)sender { [NSApp terminate:nil];return YES; }
@end
int main(int argc,char **argv){@autoreleasepool{if(argc!=4)return 2;NSApplication *app=NSApplication.sharedApplication;[app setActivationPolicy:NSApplicationActivationPolicyRegular];InputMenuPreview *preview=[InputMenuPreview new];preview.url=[NSString stringWithUTF8String:argv[1]];preview.evidence=[NSString stringWithUTF8String:argv[2]];preview.autorun=strcmp(argv[3],"1")==0;app.delegate=preview;[app run];}return 0;}
