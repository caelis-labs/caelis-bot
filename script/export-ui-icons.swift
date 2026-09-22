// Export Apple's native SF Symbols at 2x for the WKWebView; no extra icon package.
import AppKit
import Foundation
let output = URL(fileURLWithPath:CommandLine.arguments[1],isDirectory:true)
try FileManager.default.createDirectory(at:output,withIntermediateDirectories:true)
for symbol in ["xmark","arrow.up","plus","paperclip","eye.slash","puzzlepiece.extension"] {
    let config = NSImage.SymbolConfiguration(pointSize:16,weight:.medium)
    let image = NSImage(systemSymbolName:symbol,accessibilityDescription:nil)!.withSymbolConfiguration(config)!
    let bitmap = NSBitmapImageRep(bitmapDataPlanes:nil,pixelsWide:40,pixelsHigh:40,bitsPerSample:8,samplesPerPixel:4,hasAlpha:true,isPlanar:false,colorSpaceName:.deviceRGB,bytesPerRow:0,bitsPerPixel:0)!
    bitmap.size=NSSize(width:20,height:20)
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current=NSGraphicsContext(bitmapImageRep:bitmap)
    image.draw(in:NSRect(x:2,y:2,width:16,height:16))
    NSGraphicsContext.restoreGraphicsState()
    try bitmap.representation(using:.png,properties:[:])!.write(to:output.appendingPathComponent(symbol+".png"))
}
