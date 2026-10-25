# Remote machines

Settings opens one compact machine list: name, Bot/SSH address, current status and
row actions. Empty state has one sentence and Add. Authorization/native setup and
stale observations are distinct from online; bounded background checks never hold
the shared routing lock across SSH/network calls.

Add has two methods:

- **Bot connection**: paste connection information, or enter address + one-time
  code. The authenticated target's machine name is presentation metadata only;
  an explicit local rename wins. It never changes the original pairing intent,
  identity, proof or task owner.
- **SSH**: enter a system Host alias. Literal alias discovery is suggestions;
  OpenSSH interprets Include, Match, ProxyJump/ProxyCommand, identity/trust files.
  New alias connections use system trust with strict host verification. Existing
  app-pinned profiles keep their pins and bindings. Direct address setup remains
  under a secondary action. The existing Linux helper path and its original
  capabilities remain unchanged; the shared list does not upgrade the helper.

**Connect to this machine** is a secondary tab for allowing connections, copying
connection information and revoking a named source. Listener/published-address
configuration stays in details. Loopback information explicitly says it needs
external forwarding or a reachable configured listener; Tailscale only supplies
the network. A→B and B→A need separate handshakes/proofs; revocation is directional
and is not Stop. Existing unknown pairing recovery remains on its original
operation and profile, with explicit fresh authorization when required.

## Evidence

The screenshot is a real macOS WKWebView using the shared React settings and a
clearly synthetic preview transport. It proves layout, not real SSH/account or
Worker execution. Product acceptance uses separate actual A/B applications,
authenticated HTTP and React, with a controlled native fixture. Its extended
journey checks wrong-code correction, unknown original pairing recovery, two
independent handshakes, one-way revocation/restoration and an existing SSH alias
coexisting without being adopted by a mistyped Bot address. It then covers
attachments, original questions/approvals, output/download, UI Stop and an
offline copied result. Real Codex URL acceptance uses the existing isolated Mock
MCP/controlled model path; no real third-party OAuth/account claim is made.

![Native remote-machine list with synthetic data](images/remote-machines-native.png)

Native OpenSSH tests use isolated -F/Include files to verify effective multi-hop
ProxyJump and ProxyCommand configuration without connecting or reading/copying
user keys. Fault tests hold SSH inspect/model/configuration/team/terminal calls
while local and another peer Stop reach native control. Independent native-boundary
negative/positive tests cover same-profile concurrent pin publication and restoring
original pins/credentials after a certified unsupported-platform refusal. Network
or helper-install uncertainty remains unknown; it is never converted to refusal
or success. This batch does not change the bounded URL support contract.

The active user Dev bundle/data directory is not a test fixture. A new candidate
is built in a separate output root; updating/reopening the user's window requires
coordination. No daily service, SSH configuration, account or existing task moves.
