import AppKit

// Installer artwork is presentation code, independent of character authoring.
let size = NSSize(width: 660, height: 430)
let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: 1320, pixelsHigh: 860,
    bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false,
    colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
bitmap.size = size
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
NSColor(calibratedWhite: 0.97, alpha: 1).setFill()
NSRect(origin: .zero, size: size).fill()
func text(_ value: String, y: CGFloat, font: NSFont, color: NSColor) {
    let style = NSMutableParagraphStyle(); style.alignment = .center
    (value as NSString).draw(in: NSRect(x: 30, y: y, width: 600, height: 36),
        withAttributes: [.font: font, .foregroundColor: color, .paragraphStyle: style])
}
text("Caelis Bot", y: 349, font: .systemFont(ofSize: 29, weight: .semibold), color: .init(calibratedWhite: 0.16, alpha: 1))
text("拖入 Applications，即可安装", y: 309, font: .systemFont(ofSize: 16), color: .init(calibratedWhite: 0.36, alpha: 1))
let arrow = NSImage(systemSymbolName: "arrow.right", accessibilityDescription: nil)!
let configured = arrow.withSymbolConfiguration(.init(pointSize: 28, weight: .medium))!
configured.draw(in: NSRect(x: 313, y: 192, width: 34, height: 28))
text("安装后，从“应用程序”打开 Caelis Bot。", y: 41, font: .systemFont(ofSize: 12), color: .init(calibratedWhite: 0.42, alpha: 1))
text("Drag to Applications, then open Caelis Bot.", y: 17, font: .systemFont(ofSize: 11), color: .init(calibratedWhite: 0.49, alpha: 1))
NSGraphicsContext.restoreGraphicsState()
try bitmap.tiffRepresentation!.write(to: URL(fileURLWithPath: CommandLine.arguments[1]))
