# Worker URL challenge: confirmed open gate

The B-loopback URL gate is **not closed**. This checkpoint adds a real application
positive/negative control and records the native dependency; it does not add a
proxy or advertise local/remote URL equivalence. Keep #221 draft.

## Consumed contract and its boundary

The checked-in Codex schema is generated from 0.153.4. Its URL variant supplies
`threadId`, nullable `turnId`, `serverName`, `mode`, `message`, `url`,
`elicitationId`, and optional metadata. The reply is the original JSON-RPC
request's `accept`/`decline`/`cancel`. The matching
[0.153.4 native type](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/app-server-protocol/src/protocol/v2/mcp.rs#L694)
and the installed CLI's
[0.162.1 native type](https://github.com/openai/codex/blob/rust-v0.162.1/codex-rs/app-server-protocol/src/protocol/v2/mcp.rs#L728)
were both checked. Neither URL variant defines remote browser handoff, callback
rebinding or an authorized HTTP resource/origin set. This is not evidence that
all other native APIs have been exhaustively audited.

The native
[App Server lifecycle](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/app-server/README.md#mcp-server-elicitations)
also allows `serverRequest/resolved` for lifecycle cleanup. It is not a webpage
completion receipt. Retain native item/turn evidence for actual continuation.
The [MCP URL mode](https://modelcontextprotocol.io/specification/2025-11-25/client/elicitation#url-mode-elicitation-requests)
keeps sensitive web interaction outside the MCP client's structured forms.
Converting an arbitrary webpage into a Bot approval form would change that
contract and expose web secrets to another interaction path.

## Reproduction through real Apps

Run with an existing Playwright installation and browser (no download):

```sh
WORKER_PEER_PLAYWRIGHT_MODULE=/absolute/existing/playwright/index.mjs \
  bash script/e2e-worker-peer.sh --url-contract
```

The ordinary `make e2e-worker-peer` vertical journey remains unchanged in scope.
The mode uses production A/B App, HTTP, React, real Bot MCP tools, task manager
and Codex adapter. Only the native process is synthetic. It checks:

- A local Worker: original URL opens in the actual browser. GET is insufficient;
  a foreign Origin is refused; native page nonce submission and original consent
  are independently required. Duplicate native page submission has one challenge
  effect. Original native reply and continuation each occur once. The completed
  URL and original product navigation handle are no longer usable.
- Same-config B Worker: the original URL arrives in A's approval card but opening
  is refused by the shared original-owner resolver. Two clicks produce no popup,
  native reply, challenge effect or continuation. This **confirms a regression**.
- A UI reload retains the same pending remote request. Losing a real peer URL
  read response does not decide or replay work. Decline on A reaches the original
  B request once; the original handle subsequently fails closed.
- Native fixture tests cover wrong request/nonce/origin, decline/cancel/interrupt,
  and a late consent reply after original turn termination, with zero effects.

The Web read path also preserves the fixed, owner-certified unreachable-link
reason instead of displaying “expired” for a still-pending original request. Only
two allowlisted navigation codes cross this read boundary; arbitrary native
diagnostics and URL bytes remain private. No write receipt is created or changed.

A passing probe means its assertions about the open gate hold. It is **not** a
successful remote challenge, native GUI acceptance, account journey or proof of
URL recovery across a dead native owner. No test submits the remote challenge
by going directly to B. Timer expiry, redirect/browser-credential flows and a
successful remote challenge remain unverified. Existing desktop/Web tests prove
they share `backend.Service.ApprovalURL` and original `WorkDecider`, not remote
web completion.

The first browser positive control exposed a fixture bug: `no-referrer` made
Chromium serialize the form POST's Origin as `null`; the fixture then refused
it. The fixture now uses `same-origin` Referrer-Policy and still rejects foreign
Origin, wrong Host, nonce and closed requests. No product security check changed.

## Smallest path to a real closure

Prefer a challenge-provider-issued, A-browser-reachable HTTPS URL whose server
completes the **same original B elicitation**. For a provider already offering
such a URL, the existing original-owner navigation and decision routes can be
reused; test native continuation rather than treating `accept` as completion.
This does not grant A B's account/model credentials or create another authority.

For an existing provider that issues only B-local webpages, the concrete missing
piece is an official provider/native handoff that can mint that reachable URL or
explicitly declare a request-scoped remote web channel. Its minimum semantics:

- B validates authenticated source, original task, original native owner and
  elicitation; the provider derives the target, never an Agent/client URL.
- Original challenge origin/port, allowed resources/callbacks and redirects must
  be explicit. No arbitrary Host/DNS target, redirect expansion or forwarding of
  Bot peer tokens, browser cookies or B account credentials.
- Short-lived single-challenge credentials and finalization/revocation belong to
  the original provider/request. Repeated clicks cannot duplicate native effects;
  uncertain writes reconcile the original receipt, not a replacement request.
- Native consent remains the original choice/ID; completion remains original
  native evidence. Owner/request loss invalidates the handoff without substituting
  another owner. A remains the only task/interaction UI; B exposes no task card.

This is a proposed upstream/provider dependency, **not an implemented extension**
or private `_meta` convention. Caelis's absent local general form/URL capability
is not expanded.

A raw HTTP relay for only the initial URL could transport this fixture's simple
HTML form, but that would not prove the native URL contract: real pages can use
scripts, absolute resources, redirects, origin-bound credentials and callbacks.
Rewriting them into A's product origin or proxying arbitrary follow-up resources
would introduce the general browser/credential proxy excluded from this change.
A separate A loopback listener also fails Web clients that can reach A only via
its existing forwarded HTTP endpoint. Neither approach is called parity.

## Remaining merge gates

A successful request/owner-scoped remote URL journey and its disconnect,
expiry, duplicate/unknown-write, redirect/credential and dead-owner boundaries
are still blocking. Then run same-SHA real Codex/Caelis machine/account journeys,
full check/smoke/build and independent review. Fixtures and green CI do not close
those gates. No production App/Fedora/tunnel, real credential or model was used.
