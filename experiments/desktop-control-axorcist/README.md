# Native dependency feasibility probe

This experiment checks whether a small macOS Computer Use path can use the MIT
AXorcist library without Cua's MPL dependencies. It is not a production driver,
an MCP service, or a model acceptance test. The existing Cua experiment remains
unchanged and disabled by default.

The probe reads Accessibility component metadata and bounds, invokes `AXPress`,
and verifies both checkbox state and visible task count. Only the disposable
`dev.caelis.desktop-control-fixture` window and its `filter-incomplete` control
are eligible. Ten toggles restore the initial state on success. No screenshots,
global installations, model calls, or upstream source changes are involved.
If an effect cannot be confirmed, the probe fails without replaying the input;
the disposable fixture may then need a manual reset.

## Reproduce

Requires macOS 14+, Xcode/Swift 6.2 or newer, and Accessibility permission for the
actual launching host. Tested with Swift 6.4. This does not establish permission
or signing behavior for the distributed Bot application.

From the repository root:

```bash
swift build --package-path experiments/desktop-control-axorcist \
  --scratch-path .cache/axorcist-probe --force-resolved-versions
./script/build_and_run.sh --desktop-control-preview
probe_bin_dir=$(swift build --package-path experiments/desktop-control-axorcist \
  --scratch-path .cache/axorcist-probe --show-bin-path)
"$probe_bin_dir/desktop-dependency-probe"
```

The preview launches only the disposable native fixture. It does not restart the
daily Bot. After the probe, close/quit that fixture. A successful probe prints one
JSON record with `passed`, `runs`, `boundsObserved`, `stateRestored`, and
`screenshots: 0`. Permission denial does not prompt or claim a driver failure.

## Dependency and evidence boundaries

`Package.resolved` pins AXorcist v0.2.0 to commit
`c3838bfa47358202331c4cd4b71a783893825fd9` (MIT), swift-log 1.15.1
(Apache-2.0 with NOTICE), and Commander 0.3.0 (MIT). Commander is resolved through
the upstream package's CLI dependency but is not linked into this library probe.
Retain each applicable license/notice if distributing a future built artifact.

On 2026-09-28 the native probe passed 10/10 toggles with matching task counts,
valid component bounds, zero screenshots, and restored initial state. Observed
round trips were 187–251 ms in this small fixture; this is not a comparison with
the broader Cua observation path or a general performance guarantee.

Typed `Attribute<Int>` / `Attribute<String>` reads avoid an optional-nil boxing
problem observed with this revision's Any-valued `.value()` convenience method
under Swift 6.4. Production adoption still requires broader component testing.

See the [dependency audit](../../docs/architecture.md)
for the exact Cua dependency chain, MPL usage/distribution distinction, alternate
path, and remaining production gates.
