# Runtime setup prototype — design QA

final result: passed (interactive draft scope only)

## Evidence and comparison

- Reference: user-provided settings screenshot, `codex-clipboard-3b38e45b-868f-494e-92d7-8e7479b5fd2e.png` (1916 × 964 pixels).
- Render: http://127.0.0.1:9256/ — onboarding and settings. Initial review at 1039 × 968 CSS px; compact review at 860 × 760; final reference comparison at 1280 × 720.
- Combined input: http://127.0.0.1:9256/compare-focus.html. Browser capture in the task contains the reference and rendered grouped rows together. Reference normalized to 958 × 482 CSS px (assumed 2× source capture); rendered controls use CSS pixels. No local screenshot file was produced by the capture tool.
- This adapts the reference's grouping, compact descriptions and hierarchy to Bot-specific content. It is not a pixel-identical clone. The browser draft cannot prove native glass or macOS window behavior.

## Findings resolved

- P2: key settings rows initially fell below the window fold. Reduced content padding/group spacing while retaining 40 px controls; final desktop capture shows current-model selection and More Management within the window.
- P2: adding a model from settings reused numbered first-run steps. Now it is a focused management flow with Return to Settings.
- P2: model form draft affected the selected model before saving. Split draft and active model state; adding a second model preserves the first default.
- P2: switch dialog had an invalid accessible name. Replaced it with a plain label; fresh DOM reports “切换运行时”.
- P2: dark mode scrollbar used a light scheme. Set color-scheme on the dark surface; compact dark capture confirms consistent native controls/scrollbar.

## Required surfaces

- Typography: system macOS/CJK stack; readable 14 px row labels, 12–13 px explanations, 25–28 px page headings. Description sits directly below its label. No claim of exact source font metrics.
- Spacing: 40 px controls and 72+ px rows, quiet grouped borders, independent content scrolling. Final focused comparison shows breathing room around two-line rows and controls.
- Color: neutral surfaces with slight sidebar variation; dark theme preserves distinction without bright separators. Semantic status colors are restrained. No fake glass added to the content area.
- Assets: existing finished Caelis avatar reused without alteration; standard Phosphor icons. No new generated/custom character artwork, copied competitor assets or private authoring resources.
- Copy: action-led buttons, short inline explanations, no protocol/process/journal language in product screens. Draft simulation labels stay outside the product window.

## Interaction checks

Passed in the browser: missing runtime → install → model connection; second Caelis model → saved list with prior default preserved; manage Codex without activating it; login wait/cancel/restart/complete; confirmed active runtime switch; return to Caelis with both models preserved; installation failure and retry/manual fallback; rejected relative executable path; no-compatible-release fallback. Console errors: none observed.

Source implements keyboard focus containment, Escape and focus restore for dialogs; these have not been exhaustively verified with assistive technology. Active-work switch guards are specified in the design document, not implemented in this simulation.

Build: npm run build passed. Runtime install, credentials, billing calls and native application acceptance are intentionally outside this draft. Production files remain unchanged after checkpoint e6699a8.

## Handoff checklist

- [x] Light/dark and compact desktop reviewed.
- [x] Reference and rendered rows inspected in one comparison input.
- [x] Main setup and management interactions reviewed.
- [x] Sources archived separately from the product build.
- [x] Remaining native/protocol gates documented before implementation.

## Revision 02 — 2026-09-23

final result: passed (scoped visual draft).

User-requested refinements: the avatar now has a complete rounded silhouette, transparent margins and preserved pink hair / orange eyes / star / airplane / wing. A built-in ImageGen edit produced the 1254×1254 PNG with alpha; production character assets were not replaced. The avatar uses a 96 px image frame with 3 px, 7-second restrained float; reduced-motion disables it. Browser computed styling confirmed avatar-breathe active.

The welcome title is now “欢迎使用 Caelis Bot”; completion copy also uses the product name. The default character does not define the brand. Removed the auto-detection welcome note, decorative download icon from the missing-runtime row, and repeated official-install note. Install/update/error status icons remain only where they communicate actual state.

Evidence: original and revised avatars were captured together on light/dark backgrounds at `http://127.0.0.1:9256/compare-avatar-v2.html` (120 px equal image frames; intentional transparent margin reduces apparent filled area). Welcome and missing-Caelis screens were inspected in both themes at 1039×968. Complete outline has no square edge; existing typography, rows and palette remain consistent. Revised copy was confirmed in the accessibility tree. Console errors: none. `npm run build` passed. No new runtime capability was implemented or claimed.
