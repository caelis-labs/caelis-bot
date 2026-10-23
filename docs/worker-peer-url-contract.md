# Original Worker URL contract and remaining acceptance

The original-request HTTP-form slice is implemented in `de4b791c`;
[architecture, supported controls and repeatable command](worker-peer-url-forms.md).
It is one shared user-only Worker companion and the original Codex task receipt,
not a browser proxy, new native protocol or approval authority. Keep #221 Draft
until the stated parity and representative acceptance gates pass.

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

## Which local browser capabilities still regress remotely

| Native original URL/page | Local behavior | Current remote behavior / exact gap |
| --- | --- | --- |
| Public reachable HTTP(S), including JavaScript or OAuth with reachable callback | Original-owner-checked URL opens normal browser; original native reply remains separate | Same navigation path exists. OAuth/scripts alone are not a remote regression. Real provider native completion still needs representative evidence. |
| B literal-loopback HTTP, single supported static POST on exact original URL | Normal browser can submit | Shared bounded portal completes with real Codex/Mock MCP/React proof. |
| B-only localhost hostname, GET/multistep/redirect/JS or different form action | Browser handles ordinary website semantics | Portal refuses. This is a real regression when the original native provider issues such a request. |
| Public login with callback to B loopback | Browser on B reaches callback | A browser may reach login but callback hits A unless externally forwarded. Merely opening the login does not pass. |
| B HTTPS/local certificate or B-only browser credentials/profile | B browser may have required trust/state | Neither static portal nor ordinary SSH forwarding guarantees equivalent state. No credential copying or generic browser proxy. |

## Smallest user-executable path and decisions

For the B-only ordinary HTTP/JS/redirect/callback class, a candidate is:

1. The user externally forwards each **exact original origin/port**, e.g. an SSH
   `-L 127.0.0.1:P:127.0.0.1:P`. Bot does not install/manage that forwarding.
2. A exposes a user-only opt-in opener limited to the existing original request,
   retaining its exact original URL/origin, expiry and native decision. This opener
   is **not implemented yet**; current loopback refusal cannot be called support.
3. A's normal browser handles scripts/redirects using A's own legitimate login.
   B's original native result/continuation must independently confirm completion.
   A local port conflict, unavailable callback origin or changed original request
   must fail precisely; never silently rewrite callback/port or replay effects.

This needs an account-free actual A/B/browser/native representative JS + callback
probe before it can count as a supported user path. It does not solve B-only
certificates/profile-bound credentials; those require a concrete provider-supported
reachable handoff/device flow, or an explicit first-release scope decision.

Two real choices, if release scope must change: retain the current parity gate and
implement/verify that narrowly scoped external-forward opener; or explicitly defer
named B-only browser flows while shipping reachable normal-browser URLs plus the
bounded portal. No choice has been made to reduce scope. Safe refusal,
`capability=false`, going to B, or the broad phrase “OAuth unsupported” cannot close
these gates. No arbitrary URL/credential proxy is proposed.

## Remaining representative acceptance

Same-SHA macOS A → Fedora headless B, macOS desktop lifecycle/GUI and Caelis
v0.73.2 real Runtime paths are separate from synthetic native tests. Existing SSH
and release caches may be used only with isolated profiles after PID/path identity
checks. Real account/model access needs an explicitly configured isolated profile;
no keys or daily Store are copied. Final whole-PR independent review and matching
candidate/check/smoke/build/CI evidence remain required before ready. Merge/release
and changing formal services/tunnels remain unauthorised.
