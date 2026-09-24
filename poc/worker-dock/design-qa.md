# 任务气泡设计检查

2026-09-24. Scope: local browser prototype only.

## Source and comparison

- Latest user direction: identical bubbles, original prompt on hover, terminal on
  click. No categories/icons/extra panels. This replaces the initial Dock design.
- Visual reference: `/var/folders/hn/r4ffst5510s89657cwrcjj6w0000gn/T/codex-clipboard-2b3e3227-d74b-40be-9d33-a1dabbcbd410.png`.
  This 740 × 352 crop supplies the restrained translucent prompt bubble, not an
  exact whole-page layout or character asset to clone.
- Implementation: `http://127.0.0.1:4175/`.
- Evidence: `../../.cache/worker-dock/bubbles.png` and
  `../../.cache/worker-dock/bubbles-narrow.png` (local ignored artifacts).
- Browser CSS viewports: 1036 × 964 and 390 × 844. Device pixel ratio reported 2;
  CUA screenshot output is normalized to viewport dimensions. The source crop and
  implementation were emitted together in one comparison. Compared component
  hierarchy, shape, legibility and character clearance; no pixel-exact claim
  across the different crops. A separate close crop is unnecessary: prompt and
  task circles are readable in the supplied views.
- State: task circles expanded; original prompt visible and clamped to two lines.

## Findings and revisions

1. Initial full Dock tooltip covered the character. Moved it away from the face
   and checked again; evidence retained in `expanded.png` and `edge.png`.
2. User rejected category icons and additional task controls as too complex.
   Replaced them with three uniform numbered circles; removed task detail,
   results and approvals surfaces, status categories and icon dependency.
3. Latest screenshots show no actionable P0/P1/P2 visual findings for this scope.
   Prompt is above the character and remains within the narrow viewport.

## Required surfaces

- Typography: system macOS/Chinese fonts; 14px original prompt, 11px ordinal.
  Two-line clamping preserves original words instead of producing a summary.
- Layout: 36px circles with 10px gaps; collapsed 44 × 23px entry; prompt width
  bounded by viewport; no nested navigation or large task card.
- Tokens: pale neutral translucent surfaces, fine white border, restrained shadow.
  Prompt text uses a dark readable color; focus ring is visible independently of
  hover. No category color encoding.
- Imagery: existing licensed Caelis GLB and avatar, no new character art. Circles
  are actual requested UI controls, not substitutes for icons or illustration.
- Copy: original sample task prompts only; no inferred title, status summary or
  category. Demo footer explicitly says terminal launching is mocked.

## Interaction evidence

- Expanded via pointer/button; hovered second task shows its original prompt.
- Clicking second task displays the matching simulated terminal-open response.
- Escape collapses; reopening and ArrowRight selects the first original prompt.
- Narrow viewport retains prompt, pet and task controls without horizontal crop.
- Default viewport restored; preview marked as a user deliverable.
- Browser console: no captured errors. One earlier duplicate-Three warning from
  the initial prototype was addressed with Vite dedupe; no additional warning
  appeared after reload. Build passes; the 3D bundle size warning remains.

## Limits

No real Worker/terminal/approval integration. No native focus, pointer hit mask,
multi-display, Space, VoiceOver or reconnect acceptance claimed. Animation code
reuses existing clips, but this is not a new native gesture acceptance test.
Task retention and more-than-three-task overflow remain production integration
decisions; the current example intentionally contains only three tasks.

final result: passed
