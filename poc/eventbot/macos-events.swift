// Read-only native event-source probe. No event taps, screen pixels, lock/unlock
// actions, LaunchAgent installation or scripts with background side effects.
import AppKit
import CoreGraphics
import Foundation

let duration = Double(CommandLine.arguments.dropFirst().first ?? "5") ?? 5
let start = Date()
func emit(_ source: String, _ extra: [String: Any] = [:]) {
    var data = extra
    data["idleSeconds"] = CGEventSource.secondsSinceLastEventType(.combinedSessionState, eventType: CGEventType(rawValue: UInt32.max)!)
    data["elapsedSeconds"] = Date().timeIntervalSince(start)
    let event: [String: Any] = ["id": UUID().uuidString, "source": source, "data": data]
    // AppKit's session-active notification is not proof of an unlocked screen.
    // Leave it unknown until a separately validated lock adapter exists.
    let envelope: [String: Any] = ["event": event, "presence": ["Awake": true, "Unlocked": NSNull()]]
    if let bytes = try? JSONSerialization.data(withJSONObject: envelope, options: [.sortedKeys]) {
        FileHandle.standardOutput.write(bytes)
        FileHandle.standardOutput.write(Data([10]))
    }
}
let center = NSWorkspace.shared.notificationCenter
var observers: [NSObjectProtocol] = []
for (name, source) in [
    (NSWorkspace.didActivateApplicationNotification, "desktop.appActivated"),
    (NSWorkspace.willSleepNotification, "desktop.willSleep"),
    (NSWorkspace.didWakeNotification, "desktop.didWake"),
    (NSWorkspace.sessionDidBecomeActiveNotification, "desktop.sessionActive"),
    (NSWorkspace.sessionDidResignActiveNotification, "desktop.sessionInactive")
] {
    observers.append(center.addObserver(forName: name, object: nil, queue: .main) { note in
        let app = note.userInfo?[NSWorkspace.applicationUserInfoKey] as? NSRunningApplication
        emit(source, app == nil ? [:] : ["application": app!.bundleIdentifier ?? "unknown"])
    })
}
emit("desktop.snapshot")
// A single deadline, not a repeating sample or a model loop.
RunLoop.main.run(until: Date().addingTimeInterval(min(90, max(1, duration))))
for observer in observers { center.removeObserver(observer) }
