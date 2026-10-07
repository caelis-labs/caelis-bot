# UI/UX implementation principles

## Visual thesis

A quiet desktop workspace: neutral surfaces, one restrained action accent, clear type and spacing, with the character as the only expressive visual anchor.

## Each surface has one job

- First run: name the assistant, choose optional desktop abilities, and reach a usable chat or a precise connection step.
- Chat: show the conversation, current work, necessary decisions, and the next available input action.
- Settings: show current state first; let one primary action open the detail needed to change it. Keep advanced controls behind disclosure.

## Interaction rules

1. One state, one next action. A pending check ends in ready, a specific missing requirement, or a recoverable error. Labels in chat and settings come from the same underlying facts.
2. Prefer direct feedback near the control. Show validation where the value is entered; preserve drafts and original receipts when an outcome is uncertain. No decorative progress or explanatory product copy.
3. Use short, consistent transitions only for navigation, disclosure, and result feedback. Respect keyboard focus, reduced motion, system appearance and contrast. Native macOS treatment stays in the macOS adapter; shared UI remains readable on other platforms.
