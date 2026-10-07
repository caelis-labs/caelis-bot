# UI/UX implementation principles

## Visual thesis

A quiet desktop workspace: neutral surfaces, one restrained action accent, clear type and spacing, with the character as the only expressive visual anchor.

## Each surface has one job

- First run: name the assistant, choose optional desktop abilities, and reach a usable chat or a precise connection step.
- Chat: show the conversation, current work, necessary decisions, and the next available input action.
- Settings: show current state first; let one primary action open the detail needed to change it. Keep advanced controls behind disclosure.

## Interaction rules

1. One state, one next action. A pending check ends in ready, a specific missing requirement, or a recoverable error. Labels in chat and settings come from the same underlying facts.
2. Prefer direct feedback near the control. Show validation where the value is entered; preserve nonsecret drafts and original receipts when an outcome is uncertain. Clear unsaved credentials on navigation or close. No decorative progress or explanatory product copy.
3. Use short, consistent transitions only for navigation, disclosure, and result feedback. Respect keyboard focus, reduced motion, system appearance and contrast. Native macOS treatment stays in the macOS adapter; shared UI remains readable on other platforms.

## Caelis default theme

`frontend/src/theme/caelis.css` defines one built-in Caelis color set. System light and dark appearances select variants of the same set; there is no theme picker or stored theme preference. The semantic colors cover window, sidebar, content, raised surfaces, chat, composer and popovers; primary, secondary and disabled text; border, focus and selection; action hover, press and disabled states; and independent success, warning and error colors. Legacy CSS aliases keep existing component structure separate from the palette. A future palette can replace these values without changing layout, type, spacing or behavior.

The palette uses neutral light and dark planes with a restrained blue action color. The character remains the brand anchor; green indicates success only. Sidebar material is installed by the macOS native adapter and keeps an opaque, neutral fallback for increased contrast or reduced transparency. Shared CSS supplies corresponding WebView colors on every platform. This follows Apple's [Color](https://developer.apple.com/design/human-interface-guidelines/color) guidance to use color consistently for state and interaction and its [Materials](https://developer.apple.com/design/human-interface-guidelines/materials) guidance to choose material by purpose and verify legibility. The Telegram and map reference images informed the requested balance of color and hierarchy; their private details are not retained here and their layouts are not reproduced.
