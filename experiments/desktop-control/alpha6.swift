import AppKit

// Disposable provider fixture. Its own setters/events write evidence independently
// of the Bot's accessibility observation and execution receipt.
let args = CommandLine.arguments
func argument(_ name: String, _ fallback: String) -> String {
    guard let i = args.firstIndex(of: name), i + 1 < args.count else { return fallback }
    return args[i + 1]
}
let logPath = argument("--log", NSTemporaryDirectory() + "bot-alpha6.jsonl")
func record(_ event: String, _ value: String) {
    let row: [String: Any] = ["event": event, "value": value, "at": Date().timeIntervalSince1970]
    guard var data = try? JSONSerialization.data(withJSONObject: row) else { return }
    data.append(10)
    if !FileManager.default.fileExists(atPath: logPath) { FileManager.default.createFile(atPath: logPath, contents: nil) }
    guard let file = FileHandle(forWritingAtPath: logPath) else { return }
    defer { try? file.close() }
    file.seekToEndOfFile(); file.write(data)
}
final class Details: NSView {
    var expanded = false
    let label = NSTextField(labelWithString: "Details visible")
    override func isAccessibilityElement() -> Bool { true }
    override func accessibilityRole() -> NSAccessibility.Role? { .disclosureTriangle }
    override func accessibilityLabel() -> String? { "Acceptance details" }
    override func isAccessibilityExpanded() -> Bool { expanded }
    override func setAccessibilityExpanded(_ value: Bool) {
        expanded = value; label.isHidden = !value; needsDisplay = true
        record("expanded", String(value))
        NSAccessibility.post(element: self, notification: .valueChanged)
    }
    override func isAccessibilitySelectorAllowed(_ selector: Selector) -> Bool {
        selector == #selector(setAccessibilityExpanded(_:)) || super.isAccessibilitySelectorAllowed(selector)
    }
    override func draw(_ rect: NSRect) {
        let text = expanded ? "▼ Acceptance details" : "▶ Acceptance details"
        text.draw(at: NSPoint(x: 4, y: 5), withAttributes: [.foregroundColor: NSColor.labelColor])
    }
}
final class Row: NSView {
    let name: String
    var selected = false
    init(_ name: String, y: CGFloat) {
        self.name = name
        super.init(frame: NSRect(x: 8, y: y, width: 400, height: 30))
        let label = NSTextField(labelWithString: name)
        label.frame = bounds.insetBy(dx: 8, dy: 5); addSubview(label)
    }
    required init?(coder: NSCoder) { fatalError("unused") }
    override func isAccessibilityElement() -> Bool { true }
    override func accessibilityRole() -> NSAccessibility.Role? { .row }
    override func accessibilityLabel() -> String? { name }
    override func isAccessibilitySelected() -> Bool { selected }
    override func setAccessibilitySelected(_ value: Bool) {
        selected = value; needsDisplay = true
        record("selected", name + ":" + String(value))
        NSAccessibility.post(element: self, notification: .selectedRowsChanged)
    }
    override func isAccessibilitySelectorAllowed(_ selector: Selector) -> Bool {
        selector == #selector(setAccessibilitySelected(_:)) || super.isAccessibilitySelectorAllowed(selector)
    }
    override func draw(_ rect: NSRect) {
        (selected ? NSColor.selectedContentBackgroundColor : NSColor.controlBackgroundColor).setFill()
        rect.fill(); super.draw(rect)
    }
}
final class Delegate: NSObject, NSApplicationDelegate {
    var window: NSWindow!
    func applicationDidFinishLaunching(_ notification: Notification) {
        window = NSWindow(contentRect: NSRect(x: 200, y: 200, width: 460, height: 420), styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.title = argument("--title", "Alpha6 Semantic States")
        window.isReleasedWhenClosed = false
        let details = Details(frame: NSRect(x: 20, y: 355, width: 410, height: 30))
        details.label.frame = NSRect(x: 24, y: 325, width: 400, height: 25)
        details.label.isHidden = true
        window.contentView?.addSubview(details); window.contentView?.addSubview(details.label)
        let scroll = NSScrollView(frame: NSRect(x: 20, y: 30, width: 420, height: 280))
        scroll.hasVerticalScroller = true
        let document = NSView(frame: NSRect(x: 0, y: 0, width: 410, height: 2400))
        for i in 1...80 { document.addSubview(Row("Acceptance row \(i)", y: CGFloat(i-1)*30)) }
        scroll.documentView = document; window.contentView?.addSubview(scroll)
        window.makeKeyAndOrderFront(nil); NSApp.activate(ignoringOtherApps: true)
        record("ready", window.title)
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
}
let app = NSApplication.shared
app.setActivationPolicy(.regular)
let delegate = Delegate(); app.delegate = delegate; app.run()
