import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { spawnSync } from 'node:child_process';

test('native pet pointer capture cancels stale clicks and separates drag from click', {skip:process.platform!=='darwin'}, () => {
 const dir=mkdtempSync(join(tmpdir(),'caelis-pet-input-'));
 try {
  const source=join(dir,'test.m'),binary=join(dir,'test');
  // Headless unit fixture: no windows, event posting, accessibility, or desktop
  // automation. Actual AppKit click arbitration is checked in the running app.
  writeFileSync(source,`#import "pet_input_darwin.h"
   #include <assert.h>
   @interface BotPetInputView (TestAccess)
   @property NSClickGestureRecognizer *single;
   @property NSClickGestureRecognizer *doubleTap;
   @property NSPanGestureRecognizer *pan;
   - (void)recognized:(NSClickGestureRecognizer *)recognizer;
   - (void)panRecognized:(NSPanGestureRecognizer *)recognizer;
   @end
   @interface RecognizedClick : NSClickGestureRecognizer @end
   @implementation RecognizedClick
   - (NSGestureRecognizerState)state { return NSGestureRecognizerStateRecognized; }
   @end
   @interface RecognizedPan : NSPanGestureRecognizer @end
   @implementation RecognizedPan
   - (NSGestureRecognizerState)state { return NSGestureRecognizerStateBegan; }
   @end
   @interface CoordinateWindow : NSObject
   - (NSPoint)convertPointToScreen:(NSPoint)point;
   @end
   @implementation CoordinateWindow
   - (NSPoint)convertPointToScreen:(NSPoint)point { return point; }
   @end
   @interface TestInputView : BotPetInputView
   @property CoordinateWindow *coordinateWindow;
   @end
   @implementation TestInputView
   - (NSWindow *)window { return (NSWindow *)self.coordinateWindow; }
   @end
   int main(void) { @autoreleasepool {
    TestInputView *view=[[TestInputView alloc] initWithFrame:NSMakeRect(0,0,180,240)];
    view.coordinateWindow=[CoordinateWindow new];
    __block int single=0,doubled=0,drag=0;
    view.singleClick=^{single++;};view.doubleClick=^{doubled++;};view.beginDrag=^(NSEvent *e){drag++;};
    NSClickGestureRecognizer *one=view.single,*two=view.doubleTap;
    assert(one.numberOfClicksRequired==1 && two.numberOfClicksRequired==2);
    assert(!one.delaysPrimaryMouseButtonEvents && !two.delaysPrimaryMouseButtonEvents);
    assert([view gestureRecognizer:one shouldRequireFailureOfGestureRecognizer:two]);
    assert(![view gestureRecognizer:two shouldRequireFailureOfGestureRecognizer:one]);
    assert([view gestureRecognizer:two shouldRequireFailureOfGestureRecognizer:view.pan]);
    assert(![view gestureRecognizer:view.pan shouldRequireFailureOfGestureRecognizer:two]);
    NSEvent *(^event)(NSEventType,double)=^NSEvent *(NSEventType type,double time){
      return [NSEvent mouseEventWithType:type location:NSZeroPoint modifierFlags:0 timestamp:time windowNumber:0 context:nil eventNumber:1 clickCount:1 pressure:0];
    };
    [view mouseDown:event(NSEventTypeLeftMouseDown,10)];
    assert(single==0 && doubled==0); // The first press never invokes product UI.
    assert([view capturesPointer:NSMakePoint(1000,1000) atTime:10]);
    [view mouseUp:event(NSEventTypeLeftMouseUp,10.1)];
    assert([view capturesPointer:NSZeroPoint atTime:10.1+NSEvent.doubleClickInterval/2]);
    assert(![view capturesPointer:NSMakePoint(20,0) atTime:10.2]);
    assert(![view capturesPointer:NSZeroPoint atTime:10.1+NSEvent.doubleClickInterval+0.1]);
    RecognizedClick *recognized=[RecognizedClick new];view.doubleTap=recognized;
    [view recognized:recognized];
    assert(doubled==1 && single==0);
    [view recognized:recognized];assert(doubled==1); // One completed action only.
    [view mouseDown:event(NSEventTypeLeftMouseDown,20)];
    [view cancelInteraction];
    [view recognized:recognized];
    assert(doubled==1 && ![view capturesPointer:NSZeroPoint atTime:20]);
    [view mouseDown:event(NSEventTypeLeftMouseDown,30)];
    [view mouseUp:event(NSEventTypeLeftMouseUp,30.1)];
    [view cancelInteraction]; // Hide, outside click, context menu or Space change.
    [view recognized:recognized];assert(doubled==1);
    [view mouseDown:event(NSEventTypeLeftMouseDown,40)];
    [view panRecognized:[RecognizedPan new]];
    [view panRecognized:[RecognizedPan new]];
    [view mouseUp:event(NSEventTypeLeftMouseUp,40.3)];
    [view recognized:recognized];
    assert(drag==1 && doubled==1 && single==0);
    [view accessibilityPerformPress];assert(single==1 && drag==1);
    view.coordinateWindow=nil; // Restore NSView's unattached lifecycle before teardown.
   } return 0; }
  `);
  const build=spawnSync('clang',['-fobjc-arc','-fblocks','-framework','Cocoa','-I',resolve('internal/desktop'),source,resolve('internal/desktop/pet_input_darwin.m'),'-o',binary],{encoding:'utf8'});
  assert.equal(build.status,0,build.stderr);
  const run=spawnSync(binary,[],{encoding:'utf8',timeout:10000});
  assert.equal(run.status,0,run.stderr);
 } finally {rmSync(dir,{recursive:true,force:true});}
});
