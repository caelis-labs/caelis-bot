import AppKit
import WebKit

// Disposable WebKit provider fixture. The system's AXScrollToVisible action
// owns the scroll; this app only records independent page callbacks.
let arguments = CommandLine.arguments
func argument(_ name: String, _ fallback: String) -> String {
    guard let index = arguments.firstIndex(of: name), index + 1 < arguments.count else { return fallback }
    return arguments[index + 1]
}
let logPath = argument("--log", NSTemporaryDirectory() + "rc2-scroll-events.jsonl")
func record(_ event: String, _ value: String) {
    let row: [String: String] = ["event": event, "value": value]
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

final class Fixture: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKScriptMessageHandler {
    var window: NSWindow!
    var web: WKWebView!
    func applicationDidFinishLaunching(_ notification: Notification) {
        window = NSWindow(contentRect: NSRect(x: 180, y: 220, width: 520, height: 420),
                          styleMask: [.titled, .closable], backing: .buffered, defer: false)
        window.title = argument("--title", "RC2 Scroll Fixture")
        let config = WKWebViewConfiguration()
        config.userContentController.add(self, name: "fixture")
        web = WKWebView(frame: NSRect(x: 20, y: 20, width: 480, height: 370), configuration: config)
        web.navigationDelegate = self
        window.contentView?.addSubview(web)
        web.loadHTMLString("""
        <!doctype html><html lang="en"><head><meta charset="utf-8"><title>RC2 Scroll Document</title>
        <style>body{font:20px system-ui;margin:20px}button{font:20px system-ui;padding:14px}
        .gap{height:1000px;background:linear-gradient(#fff,#eef2ff)}</style></head><body>
        <h1>RC2 WebKit Scroll Fixture</h1><p>Target starts below the visible viewport.</p>
        <div class="gap"></div><button id="distant" aria-label="Distant acceptance button"
        onclick="send('clicked',visible()?'visible':'offscreen')">Distant acceptance button</button>
        <script>
        const send=(event,value)=>window.webkit.messageHandlers.fixture.postMessage({event,value});
        const target=document.getElementById('distant');
        const visible=()=>{const r=target.getBoundingClientRect();return r.bottom>0&&r.top<innerHeight};
        let logged=false;
        addEventListener('scroll',()=>{if(!logged&&visible()){logged=true;send('scrolled','true')}});
        </script></body></html>
        """, baseURL: nil)
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }
    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        record("ready", window.title)
    }
    func userContentController(_ userContentController: WKUserContentController, didReceive message: WKScriptMessage) {
        guard message.name == "fixture", let payload = message.body as? [String: String],
              let event = payload["event"], let value = payload["value"] else { return }
        record(event, value)
    }
    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }
}

let app = NSApplication.shared
app.setActivationPolicy(.regular)
let owner = Fixture()
app.delegate = owner
app.run()
