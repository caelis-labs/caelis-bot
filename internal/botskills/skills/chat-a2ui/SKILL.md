---
name: chat-a2ui
description: Add an optional read-only summary card or comparison to a reply in Caelis Bot desktop chat using A2UI Express.
---

# Present a read-only summary in desktop chat

Give the conclusion, important facts, and next steps in ordinary Markdown so
the reply makes sense on its own. Add one or two small A2UI blocks only when
the user asks for one or a compact display makes the answer easier to scan.
Use ordinary Markdown for replies intended for text channels and for questions,
approvals, task controls, linked files, or anything that requires user action.

In your assistant reply, put `<a2ui>` and `</a2ui>` at column one on their own
lines. Leave a blank line before and after the block. Emit the tags and Express
directly in the reply. Use a fenced code block when showing the syntax itself.

Start each independent block with an assignment named `root`. It may refer to
other named components in that block. Use short ASCII names starting with a
letter and double-quoted text in the user's language. The desktop chat accepts
these static components:

- `Text("text", "title")` for a heading, `Text("text")` for body text, or
  `Text("text", "caption")` for a short label. The text is displayed literally.
- `Card(child)` for a bordered group.
- `Column([first, second])` for a vertical group and `Row([first, second])` for
  a compact horizontal group. Keep each group to 12 children or fewer.
- `Divider()` for a visual break.

Keep each block under 40 components. Use plain text and static component
references. Omit HTML, Markdown inside Text, URLs as components, data bindings,
functions, surface directives, and action definitions. The display cannot submit
or authorize anything.

Example reply:

```text
The three review items include two ready to close and one still waiting for feedback.

<a2ui>
root = Card(summary)
summary = Column([heading, Divider(), counts])
heading = Text("Review summary", "title")
counts = Row([Text("2 ready"), Text("1 waiting")])
</a2ui>

The open item is the architecture review, which still needs a reviewer response.
```
