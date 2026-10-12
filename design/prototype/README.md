# Caelis Bot interactive prototype

The web prototype is the editable visual reference for this renovation. Native source and DevBot behavior remain authoritative for functionality. Figma is no longer part of the workflow. This directory is separate from the shipped app.

## Design direction

- Visual thesis: a quiet, warm desktop surface with neutral backgrounds, readable messages, precise alignment, and one restrained sage accent; the character supplies the identity.
- Content plan: conversation and the composer form the primary surface; attachments and necessary decisions appear in context; settings share the window and retain the draft's purpose-based navigation; onboarding stays a separate first-use flow.
- Interaction thesis: a short transition between conversation and settings, an attachment menu anchored to the composer, and a compact decision panel that exists only while a request needs attention. Reduced-motion preferences remove these transitions.

## Iteration order

1. Chat: keep the current DevBot shape. Remove duplicate headings, status bars and persistent prototype controls from the application surface. Refine attachments, questions, approvals, partial replies and draft continuity.
2. General settings: unify row alignment and width, remove duplicate separators, and replace unnecessary inline expansion with direct editing.
3. AI and accounts: clarify connection, account and model ownership; simplify the management dialog and advanced editing.
4. Appearance, messaging, plugins, machines, capture, privacy and updates: review one page at a time using the same layout rules.
5. Onboarding and desktop: preserve the useful draft direction and verify it against the app's actual capabilities.

## Files and preview

- `index.html`: current interactive prototype entry point; preview it through the local server.
- `styles/base.css`: imported visual foundations and the draft's other pages.
- `styles/chat.css`: the current chat iteration and shared window polish.
- `styles/settings.css`: aligned setting rows, section spacing and dialog polish.
- `scripts/app.js`: navigation, window shell, local persistence and dialogs.
- `scripts/settings.js`: settings flows.
- `scripts/experience.js`: conversation, desktop, capture and onboarding flows.
- `assets/`: images extracted from the supplied HTML.
- `reference/CaelisBot-Prototype.original.html`: unchanged original, moved from Downloads.

Local preview:

```sh
python3 -m http.server 8765 --bind 127.0.0.1 --directory design/prototype
```

Open `http://127.0.0.1:8765/`. This loopback preview is the verified route. The files are static and require no frontend build or remote dependencies. All connections, permission results and tasks are simulations. No runtime, account, OS permission or production setting is changed.

## Evidence and implementation boundary

The source baseline is `e311c0d76d5d88cedc73f2b062595087a0e6def0`. Native DevBot screenshots and the product inventory remain the current-product evidence. This prototype describes candidate presentation; its buttons do not establish native approval or execution authority.

The prototype's model and permission options are examples. A future app implementation must project the original request, target, scope and offered options, confirm native receipts, and preserve uncertain results. Hiding a decision panel never decides the request. Changing views preserves the message draft and does not stop work.

This iteration changes only the standalone prototype, so the Bot-facing runtime skill does not need an update.

## First revision

Chat now follows the current DevBot's compact shape. The attachment menu is anchored to the add button; question and approval panels occupy their own space above the composer; a collapsed request remains pending. Choices require user selection, submission disables controls, and local fixture confirmation precedes a resolved state. Keyboard navigation and return focus are covered. Drafts, attachments and quotes survive a visit to settings.

General settings use direct terminal editing. AI settings separate conversation and background work, and the connection dialog opens a dedicated location editor. These pages no longer use inline accordion sections or repeated row dividers. Other settings retain their draft flows while using the shared row alignment and spacing rules. Onboarding retains the supplied direction.

Browser checks cover the refined flows and a narrow desktop viewport. These checks establish prototype behavior only; they do not replace a future native DevBot acceptance pass.
