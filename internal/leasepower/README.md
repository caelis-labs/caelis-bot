# Managed sleep fencing

`Bind(ctx, suspend, wake)` opens a private system D-Bus connection using the
already pinned `github.com/godbus/dbus/v5 v5.2.2`. The native owner supplies a
synchronous suspend callback: revoke admission, freeze and kill the exact owned
root/descendants, then confirm their termination within four seconds. The wake
callback irreversibly revokes old ownership; it must never resume a runtime or
claim a new lease.

Binding subscribes to the unique logind owner's `PrepareForSleep` and watches
`NameOwnerChanged` before acquiring `Inhibit("sleep", ..., "delay")`. It checks
`PreparingForSleep` after acquisition to close the startup race and requires
`InhibitDelayMaxUSec` to cover at least five seconds: four for the native fence
and a delivery margin. `BindWithOptions` can raise that requirement. A missing
bus, service, subscription, Unix FD transport, property or inhibitor permission
returns `UnavailableError` with a capability reason. Managed activation must
fail when binding fails. This package changes no system settings or services.

On sleep preparation, the native suspend callback returns before the inhibitor
FD closes. On wake, old ownership is revoked and a fresh delay inhibitor is
requested for the next sleep cycle. Bus loss, service replacement, malformed
trusted signals, cancellation or explicit release also fence the owner before
closing the local inhibitor. The inhibitor FD is close-on-exec, so native child
processes cannot retain a duplicate. `release` is synchronous and idempotent.

Logind's deadline still applies, including callback scheduling time. This hook
covers sleep mediated by logind, not privileged or low-level kernel bypass.
Native process termination proof and lease fencing remain the lifecycle owner's
responsibility. Fake D-Bus fixtures exercise the same calls and signal handling
without causing sleep or requiring a running system bus. They prove ordering
and failure handling, not live logind or kernel acceptance.

Sources: [systemd inhibitor protocol](https://systemd.io/INHIBITOR_LOCKS/),
[login1 interface source](https://github.com/systemd/systemd/blob/main/man/org.freedesktop.login1.xml),
[godbus upstream](https://github.com/godbus/dbus).

## Darwin headless and desktop owners

Darwin with cgo uses the same `Bind` API through IOKit. The bridge registers on
a dedicated pthread's Core Foundation run loop, so a headless owner needs no
application main loop, AppKit, Wails or GUI initialization. The desktop entry
point is a narrow wrapper around this shared implementation. Darwin without
cgo returns an explicit unavailable reason and cannot claim managed eligibility.

On `kIOMessageSystemWillSleep`, the synchronous Go callback completes the native
fence before `IOAllowPowerChange`. `kIOMessageCanSystemSleep` allows the separate
idle-sleep proposal; the actual preparation callback still owns the fence.
Wake invokes only the ownership-revoking callback. Release/cancellation fences
the owner before unregistering, then joins the native thread and removes the Go
callback registry entry. A queued run-loop stop also handles immediate release
before the native loop has started. Callbacks must not invoke release themselves.
IOKit's OS preparation deadline still applies; the owner must finish its native
fence within four seconds. Linux-specific delay options do not change that API.

The native acknowledgement fixture compiles the production message handler with
a fake acknowledgement function and causes no OS power event. The opt-in
`CAELIS_BOT_POWER_REGISTRATION_CENSUS=1` test only registers and releases the
hook; it never requests sleep or starts a runtime. Neither is live sleep/wake
or application UI acceptance. The protocol follows
[Apple's IOKit sleep/wake guide](https://developer.apple.com/library/archive/qa/qa1340/_index.html).
