#import <Cocoa/Cocoa.h>

@interface Fixture : NSObject <NSApplicationDelegate>
@property NSWindow *window;
@property NSButton *filter;
@property NSTextField *count;
@property NSTextField *scrollStatus;
@end
@implementation Fixture
- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    // Match a normal AppKit app: command-key editing routes through its Edit
    // menu. Without it Cmd+A has no Select All action in this standalone fixture.
    NSMenu *menu=[NSMenu new];NSMenuItem *edit=[NSMenuItem new];[menu addItem:edit];
    edit.submenu=[[NSMenu alloc] initWithTitle:@"Edit"];
    [edit.submenu addItemWithTitle:@"Select All" action:@selector(selectAll:) keyEquivalent:@"a"];
    NSApp.mainMenu=menu;
    self.window=[[NSWindow alloc] initWithContentRect:NSMakeRect(180,250,620,560) styleMask:NSWindowStyleMaskTitled|NSWindowStyleMaskClosable|NSWindowStyleMaskResizable backing:NSBackingStoreBuffered defer:NO];
    self.window.title=@"Caelis Desktop Control Fixture";
    NSView *view=self.window.contentView;
    NSTextField *title=[NSTextField labelWithString:@"Desktop control — semantic target test"];
    title.font=[NSFont systemFontOfSize:22 weight:NSFontWeightMedium];title.frame=NSMakeRect(30,495,560,40);[view addSubview:title];
    NSTextField *detail=[NSTextField labelWithString:@"Read the controls, switch the filter, verify the changed value."];
    detail.frame=NSMakeRect(30,450,560,30);[view addSubview:detail];
    self.filter=[NSButton checkboxWithTitle:@"Only incomplete" target:self action:@selector(toggle:)];
    self.filter.frame=NSMakeRect(30,390,230,36);self.filter.accessibilityIdentifier=@"filter-incomplete";[view addSubview:self.filter];
    self.count=[NSTextField labelWithString:@"Visible tasks: 3"];
    self.count.frame=NSMakeRect(30,335,500,40);self.count.font=[NSFont systemFontOfSize:20];self.count.accessibilityIdentifier=@"visible-task-count";[view addSubview:self.count];
    NSTextField *input=[[NSTextField alloc] initWithFrame:NSMakeRect(30,300,500,30)];
    input.placeholderString=@"Verification text";input.accessibilityLabel=@"Verification text";
    input.accessibilityIdentifier=@"verification-text";[view addSubview:input];
    self.scrollStatus=[NSTextField labelWithString:@"Scroll position: top"];
    self.scrollStatus.frame=NSMakeRect(30,270,550,25);[view addSubview:self.scrollStatus];
    NSScrollView *scroll=[[NSScrollView alloc] initWithFrame:NSMakeRect(30,80,550,180)];
    scroll.hasVerticalScroller=YES;scroll.accessibilityLabel=@"Verification scroll area";
    NSTextView *document=[[NSTextView alloc] initWithFrame:NSMakeRect(0,0,530,1200)];
    document.editable=NO;document.accessibilityLabel=@"Verification rows";
    NSMutableString *rows=[NSMutableString new];
    for(int i=1;i<=60;i++) [rows appendFormat:@"Verification row %d\n",i];
    document.string=rows;scroll.documentView=document;[view addSubview:scroll];
    scroll.contentView.postsBoundsChangedNotifications=YES;
    [[NSNotificationCenter defaultCenter] addObserverForName:NSViewBoundsDidChangeNotification object:scroll.contentView queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
      self.scrollStatus.stringValue=scroll.contentView.bounds.origin.y>1?@"Scroll position: scrolled":@"Scroll position: top";
    }];
    NSTextField *note=[NSTextField labelWithString:@"Disposable local fixture. No files, accounts or external effects."];
    note.frame=NSMakeRect(30,40,560,25);[view addSubview:note];
    [self.window makeKeyAndOrderFront:nil];[NSApp activateIgnoringOtherApps:YES];
}
- (void)toggle:(id)sender { self.count.stringValue=self.filter.state==NSControlStateValueOn?@"Visible tasks: 2":@"Visible tasks: 3"; }
- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender { return YES; }
@end
int main(void) {@autoreleasepool {[NSApplication sharedApplication];[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];Fixture *owner=[Fixture new];NSApp.delegate=owner;[NSApp run];}return 0;}
