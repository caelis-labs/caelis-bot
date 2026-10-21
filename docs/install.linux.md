# CaelisBot for Linux / WSL

Use the signed stable release assets and installer below. The production
Ed25519 identity is pinned in the installer and updater; QA packages and keys
are not production downloads.

The release format is a complete managed `tar.gz` for **Linux amd64 or arm64**,
including Caelis CLI, Web assets, notices, and a signed companion launcher.
WSL uses the same package. There are no AppImage/deb/rpm or formal Docker images.

## Install and start

Requirements: an ordinary Linux user, Python 3, OpenSSL with Ed25519 support,
`bubblewrap` with working unprivileged namespaces, and a running **systemd user
manager**. Install missing dependencies through your distribution's package
manager. The installer never invokes sudo, weakens sandboxing, changes firewall
rules, or enables linger. No Go or Node is needed for the published package.

Install the latest signed Linux release:

```sh
curl -fsSL https://releases.caelis.dev/caelis-bot/install.sh | sh
# Explicit upgrade, including launcher repairs; retains the existing profile:
curl -fsSL https://releases.caelis.dev/caelis-bot/install.sh | sh -s -- --upgrade
```

The HTTPS bootstrap pins the same public key as the updater. Before executing a
downloaded installer or launcher, it verifies the signed per-architecture index
and each listed file's size and SHA-256. Downloadable checksums are useful for
manual transfer checks, but are not a substitute for that signature. You may
save and inspect the bootstrap instead of piping it to a shell.

Installation uses `~/.local/bin/caelisbot-launcher`,
`$XDG_DATA_HOME/caelisbot-install` for immutable code and
`$XDG_DATA_HOME/caelisbot` for the separate private profile (default data base:
`~/.local/share`). Rerunning without `--upgrade` leaves existing code and user data
in place and enables/starts the owned user service. Foreign units, launcher paths,
and incomplete installations are refused for inspection, never deleted.

Open **http://127.0.0.1:9317**. Direct localhost needs no pairing; Host, Origin and
CSRF checks remain enabled. Closing the browser leaves accepted work running.

```sh
systemctl --user status caelisbot.service
systemctl --user stop caelisbot.service
systemctl --user start caelisbot.service
journalctl --user -u caelisbot.service
```

Choose Caelis to configure a model service; its CLI is already included. Codex is
not bundled: explicitly click **Install Codex**, then **Sign in with ChatGPT**.
The service starts native device authentication; enter the displayed user code on
the official verification page. The Bot does not customize that hosted page or
expose OAuth secrets. Existing login data is reused. API Key is a secondary option.
Paths and model gateway addresses always refer to the **service machine**.

## VM, WSL and private access

Guest localhost is not the host's localhost. Keep the guest service on loopback
and use a host-only SSH forward when needed:

```sh
ssh -N -L 127.0.0.1:9317:127.0.0.1:9317 user@guest
```

Do not share this trusted local forward through a public listener. Remote proxies
or Tailscale access must use explicit allowed origins and `--require-pairing`;
see the [network deployment contract](headless-web.md#tailscale-等私网). Private
network membership alone does not authorize a browser. The installer does not
configure a VPN, proxy, or remote bind automatically.

WSL **requires systemd user services**; there is no foreground install fallback.
[Microsoft's systemd guide](https://learn.microsoft.com/en-us/windows/wsl/systemd)
explains enabling `[boot] systemd=true` and restarting WSL. Verify
`systemctl --user show-environment` in your Linux login before installing. Systemd
services do not themselves keep WSL alive after its instance closes. This source
implementation has not been accepted on a physical WSL environment. Linux/WSL
Computer Use and Web terminals are unavailable; the interface hides those entries.

## Updates

- **Managed:** Settings → Updates checks, downloads with actual byte progress,
  and schedules installation through the existing idle handoff. `--upgrade` can
  stage the full package through the live owner without stopping the service.
  It replaces the on-disk launcher for its next start only after candidate health
  is confirmed. Busy work leaves the update scheduled and the old service running;
  rerun `--upgrade` after Settings confirms installation to finish launcher repair.
  For a stopped installation, the installer starts the original owned user unit
  and waits for the same handoff and candidate health before replacing the launcher.
  Startup or health failure preserves the old launcher and reports no completion.
- **External/manual:** stop the owner and replace its complete directory yourself;
  the Web surface does not claim it can install into arbitrary paths.
- **Image/development Docker:** updates belong to the image operator. Docker sandbox
  compatibility remains unaccepted; no official image is published here.

Updates preserve the profile, login, Notebook, history and original receipts.
They may carry a newer bundled Core; Core owns Host idle replacement and its actual
identity is read back. Code fallback is not user-data or Core-version downgrade.
