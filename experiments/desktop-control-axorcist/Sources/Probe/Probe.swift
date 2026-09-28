import AppKit
import ApplicationServices
import AXorcist
import Foundation

// A dependency feasibility probe, not a general Computer Use server. Only the
// disposable fixture's checkbox can receive input; no screenshot API is used.
@main
struct Probe {
    struct Failure: Error { let message: String }
    struct Run: Encodable { let iteration: Int; let value: String; let elapsedMs: Int }
    struct Evidence: Encodable {
        let driver = "AXorcist 0.2.0"
        let screenshots = 0
        let passed: Bool
        let runs: [Run]
        let boundsObserved: Bool
        let stateRestored: Bool
    }

    @MainActor
    static func elements(_ root: Element) throws -> [Element] {
        var pending: [(Element, Int)] = [(root, 0)]
        var seen: Set<Element> = []
        var result: [Element] = []
        while let (element, depth) = pending.popLast() {
            guard depth <= 12, seen.count < 128 else { throw Failure(message: "tree_limit") }
            guard seen.insert(element).inserted else { continue }
            result.append(element)
            let children = try element.withMessagingTimeout(0.5) { $0.children() ?? [] }
            pending.append(contentsOf: children.reversed().map { ($0, depth + 1) })
        }
        return result
    }

    @MainActor
    static func state(_ app: Element) throws -> (Element, String, String, Bool) {
        let windows = try app.withMessagingTimeout(0.5) { $0.windows() ?? [] }
            .filter { $0.title() == "Caelis Desktop Control Fixture" }
        guard windows.count == 1 else { throw Failure(message: "fixture_window_missing_or_ambiguous") }
        let nodes = try elements(windows[0])
        let filters = nodes.filter { $0.role() == "AXCheckBox" && $0.identifier() == "filter-incomplete" }
        let counts = nodes.filter { $0.identifier() == "visible-task-count" }
        guard filters.count == 1, counts.count == 1,
              filters[0].isEnabled() != false, filters[0].isActionSupported("AXPress"),
              let value = filters[0].attribute(Attribute<Int>("AXValue")),
              let count = counts[0].attribute(Attribute<String>("AXValue"))
        else { throw Failure(message: "fixture_controls_unavailable") }
        let frame = filters[0].frame()
        // Use typed AX attributes: this revision's Any-valued value() convenience
        // path returns an optional-nil box under the tested Swift 6.4 toolchain.
        return (filters[0], String(value), count,
                frame.map { $0.width > 0 && $0.height > 0 } ?? false)
    }

    @MainActor
    static func main() async {
        do {
            guard AXIsProcessTrusted() else { throw Failure(message: "accessibility_permission_required") }
            let apps = NSRunningApplication.runningApplications(withBundleIdentifier: "dev.caelis.desktop-control-fixture")
            guard apps.count == 1 else { throw Failure(message: "expected_one_disposable_fixture") }
            let app = Element(AXUIElementCreateApplication(apps[0].processIdentifier))
            let initial = try state(app)
            guard initial.1 == "0" || initial.1 == "1", initial.3 else {
                throw Failure(message: "invalid_initial_state value=\(initial.1) bounds=\(initial.3)")
            }
            var current = initial
            var runs: [Run] = []
            for iteration in 1...10 {
                let wanted = current.1 == "0" ? "1" : "0"
                let wantedCount = "Visible tasks: \(wanted == "1" ? 2 : 3)"
                let start = ContinuousClock.now
                _ = try current.0.withMessagingTimeout(0.5) { try $0.performAction("AXPress") }
                // Poll only observations; never retry an input whose effect is uncertain.
                for _ in 0..<20 {
                    current = try state(app)
                    if current.1 == wanted && current.2 == wantedCount { break }
                    try await Task.sleep(for: .milliseconds(50))
                }
                guard current.1 == wanted, current.2 == wantedCount else { throw Failure(message: "effect_not_observed_do_not_replay") }
                let duration = start.duration(to: .now).components
                runs.append(Run(iteration: iteration, value: current.1,
                                elapsedMs: Int(duration.seconds * 1000 + duration.attoseconds / 1_000_000_000_000_000)))
            }
            guard current.1 == initial.1 else { throw Failure(message: "state_not_restored") }
            let data = try JSONEncoder().encode(Evidence(passed: true, runs: runs, boundsObserved: initial.3, stateRestored: true))
            print(String(decoding: data, as: UTF8.self))
        } catch {
            let message = (error as? Failure)?.message ?? "native_probe_failed"
            FileHandle.standardError.write(Data("AXorcist probe: \(message)\n".utf8))
            exit(1)
        }
    }
}
