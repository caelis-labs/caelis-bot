import AppKit

// Disposable menu target with an app-owned effect log.
let args = CommandLine.arguments
func option(_ name: String, _ fallback: String) -> String {
    guard let i = args.firstIndex(of: name), i + 1 < args.count else { return fallback }
    return args[i + 1]
}
let logPath = option("--log", NSTemporaryDirectory() + "rc2-menu-events.jsonl")
func record(_ event: String, _ value: String) {
    let row = ["event": event, "value": value]
    guard var data = try? JSONSerialization.data(withJSONObject: row) else { return }
    data.append(10)
    if !FileManager.default.fileExists(atPath: logPath) {
        _ = FileManager.default.createFile(atPath: logPath, contents: nil)
    }
    guard let file = FileHandle(forWritingAtPath: logPath) else { return }
    defer { try? file.close() }
    file.seekToEndOfFile()
    file.write(data)
}

final class Fixture: NSObject, NSApplicationDelegate {
    var window: NSWindow!
    var count = 0
    var label: NSTextField!
    func applicationDidFinishLaunching(_ notification: Notification) {
        let menu = NSMenu()
        let top = NSMenuItem(title: "Acceptance", action: nil, keyEquivalent: "")
        let sub = NSMenu(title: "Acceptance")
        sub.addItem(withTitle: "Increment Fixture Counter", action: #selector(increment(_:)), keyEquivalent: "")
        top.submenu = sub
        menu.addItem(top)
        NSApp.mainMenu = menu
        window = NSWindow(contentRect: NSRect(x: 200, y: 250, width: 440, height: 220),
                          styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.title = option("--title", "RC2 Menu Fixture")
        label = NSTextField(labelWithString: "Menu count: 0")
        label.frame = NSRect(x: 30, y: 100, width: 380, height: 45)
        label.font = .systemFont(ofSize: 22)
        window.contentView?.addSubview(label)
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        record("ready", window.title)
        record("pid", String(ProcessInfo.processInfo.processIdentifier))
    }
    @objc func increment(_ sender: Any?) {
        count += 1
        label.stringValue = "Menu count: \(count)"
        record("menu_count", String(count))
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
}

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let owner = Fixture()
app.delegate = owner
app.run()
