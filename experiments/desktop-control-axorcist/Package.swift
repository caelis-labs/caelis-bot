// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "DesktopDependencyProbe",
    platforms: [.macOS(.v14)],
    products: [.executable(name: "desktop-dependency-probe", targets: ["Probe"])],
    dependencies: [
        .package(url: "https://github.com/openclaw/AXorcist.git", revision: "c3838bfa47358202331c4cd4b71a783893825fd9"),
        .package(url: "https://github.com/apple/swift-log.git", exact: "1.15.1"),
    ],
    targets: [
        .executableTarget(name: "Probe", dependencies: [.product(name: "AXorcist", package: "AXorcist")]),
    ]
)
