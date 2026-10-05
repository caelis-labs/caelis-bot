#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
@interface Fixture:NSObject<NSApplicationDelegate,WKNavigationDelegate>
@property NSWindow *window;
@property WKWebView *web;
@property NSString *url;
@property NSString *output;
@property NSString *script;
@end
@implementation Fixture
- (void)applicationDidFinishLaunching:(NSNotification *)note {
 if([self.url containsString:@"theme=light"])NSApp.appearance=[NSAppearance appearanceNamed:NSAppearanceNameAqua];
 if([self.url containsString:@"theme=dark"])NSApp.appearance=[NSAppearance appearanceNamed:NSAppearanceNameDarkAqua];
 CGFloat width=[self.url containsString:@"width=640"]?640:860;
 self.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(60,60,width,640) styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO];
 self.window.title=@"Settings fixture";
 WKWebViewConfiguration *config=[WKWebViewConfiguration new];config.websiteDataStore=[WKWebsiteDataStore nonPersistentDataStore];
 self.web=[[WKWebView alloc] initWithFrame:self.window.contentView.bounds configuration:config];self.web.autoresizingMask=NSViewWidthSizable|NSViewHeightSizable;self.web.navigationDelegate=self;
 [self.window.contentView addSubview:self.web];[self.window makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
 [self.web loadRequest:[NSURLRequest requestWithURL:[NSURL URLWithString:self.url]]];
 dispatch_after(dispatch_time(DISPATCH_TIME_NOW,20*NSEC_PER_SEC),dispatch_get_main_queue(),^{fprintf(stderr,"Fixture timed out\n");exit(1);});
}
- (void)webView:(WKWebView *)web didFinishNavigation:(WKNavigation *)navigation {
 dispatch_after(dispatch_time(DISPATCH_TIME_NOW,2*NSEC_PER_SEC),dispatch_get_main_queue(),^{
  [web takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image,NSError *error){
   if(!image){fprintf(stderr,"Snapshot failed\n");exit(1);}
   NSBitmapImageRep *bitmap=[[NSBitmapImageRep alloc] initWithData:image.TIFFRepresentation];
   if(![[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:self.output atomically:YES])exit(1);
   if(self.script.length){[web evaluateJavaScript:self.script completionHandler:^(id value,NSError *failure){
    if(failure){fprintf(stderr,"Fixture interaction failed\n");exit(1);}
    dispatch_after(dispatch_time(DISPATCH_TIME_NOW,500*NSEC_PER_MSEC),dispatch_get_main_queue(),^{
     [web evaluateJavaScript:@"JSON.stringify(window.__fixtureResult)" completionHandler:^(id result,NSError *error){
      if(error||![result isKindOfClass:NSString.class]){fprintf(stderr,"Fixture result missing\n");exit(1);}
      [[result dataUsingEncoding:NSUTF8StringEncoding] writeToFile:[self.output stringByAppendingString:@".json"] atomically:YES];
      fprintf(stdout,"Captured %s\n",self.output.UTF8String);exit(0);
     }];
    });
   }];}else{fprintf(stdout,"Captured %s\n",self.output.UTF8String);exit(0);}
  }];
 });
}
@end
int main(int argc,char **argv){@autoreleasepool{if(argc!=4)return 2;NSApplication *app=NSApplication.sharedApplication;[app setActivationPolicy:NSApplicationActivationPolicyRegular];Fixture *f=[Fixture new];f.url=[NSString stringWithUTF8String:argv[1]];f.output=[NSString stringWithUTF8String:argv[2]];if(strcmp(argv[3],"-")!=0)f.script=[NSString stringWithContentsOfFile:[NSString stringWithUTF8String:argv[3]] encoding:NSUTF8StringEncoding error:nil];app.delegate=f;[app run];}return 0;}
