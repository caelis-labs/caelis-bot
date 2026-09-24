import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

test('native chat shortcut and Dock policy distinguish foreground, covered, minimised and closed windows', {skip:process.platform!=='darwin'}, () => {
 const dir=mkdtempSync(join(tmpdir(),'caelis-window-lifecycle-'));
 try {
  const source=join(dir,'test.m'),binary=join(dir,'test');
  // Fake AppKit property owners exercise the production bridge without opening
  // windows or posting input. Real Dock/close behavior is checked in the app.
  writeFileSync(source,`#import <Cocoa/Cocoa.h>
   #include "native_darwin.h"
   #include <assert.h>
   @interface TestWindow : NSObject
   @property(getter=isVisible) BOOL visible;
   @property(getter=isMiniaturized) BOOL miniaturized;
   @property(getter=isKeyWindow) BOOL keyWindow;
   @property NSObject *attachedSheet;
   @property int closeCount;
   - (void)performClose:(id)sender;
   @end
   @implementation TestWindow
   - (void)performClose:(id)sender { self.closeCount++; self.visible=NO; self.miniaturized=NO; self.keyWindow=NO; }
   @end
   @interface TestApplication : NSObject
   @property(getter=isActive) BOOL active;
   @property(readonly) NSApplicationActivationPolicy activationPolicy;
   @property int transitions;
   @property NSImage *applicationIconImage;
   @property TestWindow *keyWindow;
   @property TestWindow *mainWindow;
   @property NSArray *orderedWindows;
   - (BOOL)setActivationPolicy:(NSApplicationActivationPolicy)value;
   @end
   @implementation TestApplication
   - (BOOL)setActivationPolicy:(NSApplicationActivationPolicy)value { _activationPolicy=value;_transitions++;return YES; }
   @end
   int main(void) { @autoreleasepool {
    TestApplication *app=[TestApplication new];NSApp=(NSApplication *)app;
    NSData *icon=[NSData dataWithContentsOfFile:@"${resolve('internal/desktop/assets/app-icon.png')}"];
    assert(bot_install_app_icon((unsigned char *)icon.bytes,(int)icon.length));
    NSImage *installed=app.applicationIconImage;assert(installed.representations.count==1);
    assert(!bot_install_app_icon(NULL,0));assert(app.applicationIconImage==installed);
    TestWindow *chat=[TestWindow new],*settings=[TestWindow new];
    void *c=(__bridge void *)chat,*s=(__bridge void *)settings;
    bot_sync_dock(c,s,0);assert(app.activationPolicy==NSApplicationActivationPolicyAccessory);
    app.applicationIconImage=nil;
    bot_sync_dock(c,s,1);assert(app.activationPolicy==NSApplicationActivationPolicyRegular);
    assert(app.applicationIconImage==installed);
    chat.visible=YES;chat.keyWindow=YES;app.active=YES;
    assert(bot_window_open(c)&&bot_window_can_hide(c));
    int changes=app.transitions;
    app.active=NO;assert(!bot_window_can_hide(c));bot_sync_dock(c,s,0);
    assert(app.activationPolicy==NSApplicationActivationPolicyRegular&&app.transitions==changes);
    app.active=YES;chat.keyWindow=NO;assert(!bot_window_can_hide(c));
    chat.keyWindow=YES;chat.attachedSheet=[NSObject new];assert(!bot_window_can_hide(c));
    chat.attachedSheet=nil;chat.miniaturized=YES;chat.visible=NO;
    assert(bot_window_open(c)&&!bot_window_can_hide(c));bot_sync_dock(c,s,0);
    assert(app.activationPolicy==NSApplicationActivationPolicyRegular);
    chat.miniaturized=NO;chat.keyWindow=NO;settings.visible=YES;
    assert(!bot_window_open(c));bot_sync_dock(c,s,0);
    assert(app.activationPolicy==NSApplicationActivationPolicyRegular);
    settings.visible=NO;bot_sync_dock(c,s,0);
    assert(app.activationPolicy==NSApplicationActivationPolicyAccessory);
    // Window quit uses the same close action and leaves the other window open.
    chat.visible=YES;settings.visible=YES;app.keyWindow=settings;app.mainWindow=chat;
    app.orderedWindows=@[chat,settings];
    bot_close_context_window(c,s);
    assert(settings.closeCount==1&&chat.closeCount==0&&chat.visible);
    // A native sheet must not lose its owning window.
    app.keyWindow=chat;chat.attachedSheet=[NSObject new];
    bot_close_context_window(c,s);assert(chat.closeCount==0);
    chat.attachedSheet=nil;app.active=NO;app.keyWindow=nil;app.mainWindow=nil;
    bot_close_context_window(c,s);assert(chat.closeCount==1&&!chat.visible);
    // A minimised window can close without being recalled first.
    settings.miniaturized=YES;app.orderedWindows=@[];
    bot_close_context_window(c,s);assert(settings.closeCount==2&&!settings.miniaturized);
    bot_close_context_window(c,s);assert(settings.closeCount==2&&chat.closeCount==1);
    assert(!bot_system_termination());
    NSApp=nil;
   } return 0; }
  `);
  const build=spawnSync('clang',['-fobjc-arc','-framework','Cocoa','-I',resolve('internal/desktop'),source,resolve('internal/desktop/window_lifecycle_darwin.m'),'-o',binary],{encoding:'utf8'});
  assert.equal(build.status,0,build.stderr);
  const run=spawnSync(binary,[],{encoding:'utf8',timeout:10000});assert.equal(run.status,0,run.stderr);
 } finally {rmSync(dir,{recursive:true,force:true});}
});
