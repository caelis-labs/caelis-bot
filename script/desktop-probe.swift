// Disposable native acceptance fixture. No product surface, backend or user data.
// Build/run instructions are in docs/native-acceptance.md.
import AppKit
final class Probe: NSObject, NSApplicationDelegate {
    var window: NSWindow!
    let counter = NSTextField(labelWithString: "Clicks: 0")
    var clicks = 0
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        let screen = NSScreen.main!.visibleFrame
        window = NSWindow(contentRect:NSRect(x:screen.maxX-650,y:screen.minY+80,width:620,height:520),styleMask:[.titled,.closable,.resizable],backing:.buffered,defer:false)
        window.title = "Caelis native acceptance underlay"
        let content = NSView(frame:NSRect(x:0,y:0,width:620,height:520))
        let button = NSButton(title:"Click-through target",target:self,action:#selector(clicked))
        button.frame=content.bounds
        button.autoresizingMask=[.width,.height]
        content.addSubview(button)
        counter.frame=NSRect(x:24,y:450,width:500,height:30)
        counter.font = .systemFont(ofSize:22)
        content.addSubview(counter)
        let text = NSTextField(string:"")
        text.placeholderString="Focus target — type here before dragging the pet"
        text.frame=NSRect(x:24,y:405,width:570,height:30)
        content.addSubview(text)
        let full = NSButton(title:"Toggle full screen",target:self,action:#selector(fullscreen))
        full.frame=NSRect(x:24,y:20,width:160,height:30)
        content.addSubview(full)
        window.collectionBehavior = [.fullScreenPrimary]
        window.contentView=content
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps:true)
    }
    @objc func fullscreen() { window.toggleFullScreen(nil) }
    @objc func clicked() { clicks += 1; counter.stringValue="Clicks: \(clicks)" }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender:NSApplication)->Bool { true }
}
let app=NSApplication.shared
let delegate=Probe()
app.delegate=delegate
app.run()
