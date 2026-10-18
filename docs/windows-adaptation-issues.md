# Windows validation gaps

This guide lists remaining scope and targeted checks. Resolved defect diaries are retained in Git history. Current architecture and publishing instructions are in [implementation scope](windows-adaptation-plan.md) and [Windows release](windows-release.md).

| Area | Remaining check or limitation |
| --- | --- |
| Desktop World | Upstream DPI-unaware scaling, modal-provider stalls and post-effect errors: [DTW #36](https://github.com/caelis-labs/desktop-world/issues/36). Non-blocking for DTW v0.1.0; no general mixed-DPI, elevated-app or secure-desktop claim. |
| Platform coverage | Windows 10 22H2, Server interactive desktops and combinations of monitors/DPI need separate native verification. Windows 11 and CI do not establish those results. |
| Installed App updates | Component/install tests cover original receipts, custom paths and rollback. Full GUI App-to-App upgrade with active work, real accounts, low disk and interrupted downloads still needs targeted verification. |
| Character and bubble | Verify hide/show, preserved reply expansion, multi-monitor position, scaling and cold-start second-instance recall on native windows. |
| Capture | Verify first-paint transitions across GPUs/DPI and complete save/cancel paths. Do not infer zero flicker from still screenshots or contract tests. |
| Runtime/model settings | Verify measured native response and presentation under slow discovery, existing credentials, failure and return navigation. Model selection must remain editable while the choice is applied. |
| Terminals | Reliable original-client ownership is required. Thumbnail availability and every terminal/theme combination are not guaranteed; unavailable observation must not create a replacement task. |
| Login startup | Registration/readback is separate from an actual logout/login launch and OS policy. Do not edit private StartupApproved state to force availability. |
| Signing | Current Setup/ZIP are unsigned. Ed25519 update manifests do not provide Authenticode trust or bypass SmartScreen/organization policy. |

Follow-up coordination: [Bot #185](https://github.com/caelis-labs/caelis-bot/issues/185), [Bot #186](https://github.com/caelis-labs/caelis-bot/issues/186). These links track development work; inspect each issue's current status before treating it as open.

When filing a new defect, include the source/tag, package digest, Windows version, scaling, exact original operation and independent effect readback. Preserve `unknown` outcomes and original receipts. Never repeat input merely because an error followed it.
