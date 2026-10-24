# Original Worker URL contract and first-release scope

The original-request HTTP-form slice is implemented in `de4b791c`;
[architecture, supported controls and repeatable command](worker-peer-url-forms.md).
It is one shared user-only Worker companion and the original Codex task receipt,
not a browser proxy, new native protocol or approval authority. Keep #221 Draft
until the stated first-release and representative acceptance gates pass.

## Confirmed native contract

Checked-in Codex schema 0.153.4 and real installed 0.162.1 expose original
`threadId`, optional `turnId`, server/message/URL/elicitation ID and original native
`accept`/`decline`/`cancel` reply. The public
[0.153.4 type](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/app-server-protocol/src/protocol/v2/mcp.rs)
and [0.162.1 type](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server-protocol/src/protocol/v2/mcp.rs)
provide no generic remote browser/callback handoff. This is a bounded contract
observation, not proof that every provider needs new native features. Native
`serverRequest/resolved` is not webpage completion evidence.

Core v0.73.2/source `c882776c01c6116f1918d796c54ed66dccebc61a` preserves its
pinned public schema/wire. This Bot-direct-Codex path does not add general forms
or URL support to the Caelis adapter where local Core has no such capability.

## Actual product evidence

The historical `78fbaaa` native-fixture probe established local browser nonce POST
plus original consent and reproduced the B-loopback gap. Its old negative-control
assertions are historical; `--url-contract` now forwards to the real-Runtime entry.

Clean exact SHA `de4b791c`, tree `cc9e1111`, passed
`make e2e-worker-peer-real-url`: real A/B applications, HTTP/React/Chrome/Bot MCP,
real Codex 0.162.1, temporary ordinary MCP and controlled local Responses model.
No user credential/daily MCP configuration or paid model was used. It asserts:

- Original task/thread/turn/native request/elicitation/source references remain.
- A completes the B-loopback form; original native consent, tool result and
  native continuation are independently required and each effect occurs once.
- Duplicate original operation does not POST again; conflicting content is refused.
- Lost peer reply reconciles original receipt; provider reply loss remains unknown
  even after native continuation, including UI reload. Neither permits replay.
- Decline, provider expiry and verified original-owner exit cause no successful
  webpage/native effect. B resident lists exclude foreign work.

This is real Runtime + mock model/MCP, **not** real accounts, third-party OAuth,
physical cross-machine or native-GUI acceptance. `check`, affected race and smoke
pass; CI39983328004 passed macOS/Linux/Windows/product at that exact head. Earlier
Windows durable-I/O timeout causes remain unproven, not labelled flaky.

The independent 34-file review added two concrete regressions: offline original
receipt retrieval (including B App factory/current resident readiness) and browser
control semantics. Receipt reads now retain authentication/source/original binding
without requiring native Connect, replacement or model selection; new work still
uses B's real selection/readiness. Readonly, disabled fieldset and inert template
forms are rejected, not projected into mutable/successful controls. Saved accepted
and unknown receipts remain readable through a disconnected native projection.

## First-release support decision

The user explicitly deferred the following B-only browser flows for the first
release. This is a scope decision, **not a repair or evidence of equivalence**.
The rest of the same-Runtime task/interaction contract remains unchanged.

| Native original URL/page | First-release behavior | Evidence / limit |
| --- | --- | --- |
| Public reachable HTTP(S), including JavaScript or OAuth with a reachable callback | Original-owner-checked URL opens A's normal browser; original native reply remains separate | Existing normal-browser route. Reachability includes every required callback. No claim of real third-party OAuth acceptance. |
| B literal-loopback HTTP, one supported static POST on exact original URL | A's bounded portal submits to B's original request; native consent and continuation remain separate | Real Codex/Mock MCP/React evidence, including physical macOS headless A → Fedora B at `8f203370`. |
| B-only localhost hostname, GET/multistep/redirect/JS or different form action | **Deferred**; current portal refuses and retains original pending/unknown facts | An actual local-to-remote limitation, not completed functionality. |
| Public login with callback to B loopback | **Deferred**, unless the original callback is independently reachable from A through the supported normal-browser path | Opening the public login alone cannot complete the callback. Bot does not create a forward or add an external-forward opener. |
| B HTTPS/local certificate or B-only browser credentials/profile | **Deferred** | No certificate/credential migration or browser-state relay. |

A native URL is a website entry, not a serializable completion API. A's browser
interprets localhost and a loopback callback as **A**, while the original service
lives on B. Multistep pages can change methods/actions, run scripts, redirect and
rely on origin-bound cookies or browser state. Supporting those semantics would
require broader browser/transport authority than the exact static form projection.
B-only certificates/profile credentials add separate trust and ownership barriers.
There is no generic native handoff contract that makes those steps transparent.

Publicly reachable scripts or OAuth are **not categorically unsupported**. They
use normal browser navigation if their entire required callback path is reachable.
The bounded portal does not execute scripts or relay OAuth credentials. A webpage
open, a safe refusal or a native consent alone never proves webpage completion or
native continuation. Users are not sent to B to approve foreign work.

## Deferred follow-up (not a first-release implementation gate)

Record concrete provider/native examples for B-only multistep/GET/JS/redirect or
different-action pages, B-loopback login callbacks and B-only trust/browser state.
Any future support needs an explicit original-request-scoped design and independent
native continuation evidence. No automatic tunnels, general proxy, certificate or
credential migration, or new external-forward opener is included in this release.
Do not expand this backlog into a general browser system during final audit.

## Remaining representative acceptance

`8f203370` has physical macOS headless A → Fedora headless B and a separate
macOS native GUI → released Core v0.73.2 journey with controlled local models.
Those older records are not final-head evidence or a native-GUI remote-Worker
journey. Final-head affected application checks and remaining representative
GUI/upgrade paths stay separate from synthetic native tests. Existing SSH
and release caches may be used only with isolated profiles after PID/path identity
checks. Real account/model access needs an explicitly configured isolated profile;
no keys or daily Store are copied. Final whole-PR independent review and matching
candidate/check/smoke/build/CI evidence remain required before ready. Merge/release
and changing formal services/tunnels remain unauthorised.

The isolated real-account/model input (Q22) has not been supplied. Dependent real
account steps remain blocked; no daily credentials/configuration may be copied
and no unanswered question is inferred to be authorization. Scope deferral does
not waive final review or exact-source automated gates. The latest review scope
explicitly makes physical-machine/account/native-GUI acceptance nonblocking
follow-up; missing Q22 input remains unauthorised, not implicitly answered.

Whole-PR original-receipt/source/Stop/storage/pairing repairs and the final-head
versus older hardware evidence distinction are recorded in the
[final audit map](worker-peer-final-audit.md). Named URL deferrals do not close
these other task and acceptance gates.
