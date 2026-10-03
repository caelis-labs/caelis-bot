#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

@interface MachineSettingsRegression:NSObject<NSApplicationDelegate,WKNavigationDelegate,WKScriptMessageHandler>
@property NSWindow *window;
@property WKWebView *web;
@property NSString *url;
@property NSString *script;
@property NSString *capture;
@end
@implementation MachineSettingsRegression
- (void)applicationDidFinishLaunching:(NSNotification *)note {
 self.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(80,80,860,640) styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO];
 self.window.title=@"Machine Settings Regression";
 WKWebViewConfiguration *config=[WKWebViewConfiguration new];
 config.websiteDataStore=[WKWebsiteDataStore nonPersistentDataStore];
 [config.userContentController addScriptMessageHandler:self name:@"regression"];
 self.web=[[WKWebView alloc] initWithFrame:self.window.contentView.bounds configuration:config];
 self.web.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;self.web.navigationDelegate=self;
 [self.window.contentView addSubview:self.web];[self.window makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
 [self.web loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:self.url]]];
 dispatch_after(dispatch_time(DISPATCH_TIME_NOW,40*NSEC_PER_SEC),dispatch_get_main_queue(),^{fprintf(stderr,"Machine settings regression timed out\n");exit(1);});
}
- (void)webView:(WKWebView *)web didFinishNavigation:(WKNavigation *)navigation {
 fprintf(stderr,"Machine fixture page loaded\n");
 [web evaluateJavaScript:self.script completionHandler:^(id value,NSError *error){if(error){fprintf(stderr,"Machine regression script failed\n");exit(1);}}];
}
- (void)webViewWebContentProcessDidTerminate:(WKWebView *)web {fprintf(stderr,"Machine regression WebKit process terminated\n");exit(1);}
- (void)webView:(WKWebView *)web didFailProvisionalNavigation:(WKNavigation *)navigation withError:(NSError *)error {fprintf(stderr,"Machine regression navigation failed: %s\n",error.localizedDescription.UTF8String);exit(1);}
- (void)userContentController:(WKUserContentController *)controller didReceiveScriptMessage:(WKScriptMessage *)message {
 NSDictionary *result=message.body;
 if(![result[@"ok"] boolValue]){[[NSJSONSerialization dataWithJSONObject:result options:0 error:nil] writeToFile:[self.capture stringByAppendingString:@".json"] atomically:YES];fprintf(stderr,"Machine settings regression: %s\n",[result[@"error"] UTF8String]);exit(1);}
 [self.web takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image,NSError *error){
  if(!image){fprintf(stderr,"Machine regression snapshot failed\n");exit(1);}
  NSBitmapImageRep *bitmap=[[NSBitmapImageRep alloc] initWithData:image.TIFFRepresentation];
  if(![[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:self.capture atomically:YES])exit(1);
  [[NSJSONSerialization dataWithJSONObject:result options:0 error:nil] writeToFile:[self.capture stringByAppendingString:@".json"] atomically:YES];
  fprintf(stdout,"Machine settings native regression PASS (%s, %ld requests, 860x640)\n",[result[@"locale"] UTF8String],(long)[result[@"requests"] integerValue]);exit(0);
 }];
}
@end
int main(int argc,char **argv){@autoreleasepool{
 if(argc!=4)return 2;
 NSApplication *app=NSApplication.sharedApplication;[app setActivationPolicy:NSApplicationActivationPolicyRegular];
 MachineSettingsRegression *fixture=[MachineSettingsRegression new];fixture.url=[NSString stringWithUTF8String:argv[1]];fixture.script=[NSString stringWithContentsOfFile:[NSString stringWithUTF8String:argv[2]] encoding:NSUTF8StringEncoding error:nil];fixture.capture=[NSString stringWithUTF8String:argv[3]];app.delegate=fixture;[app run];
}return 0;}
