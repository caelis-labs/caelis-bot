# Worker peer application acceptance

## Run without downloads

Use the existing Node/Go caches, Chrome and an installed Playwright module:

```sh
export WORKER_PEER_PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs
make e2e-worker-peer
CAELIS_BOT_TEST_CODEX=/absolute/path/to/codex make e2e-worker-peer-real-url
```

The first command launches actual isolated A/B application entrypoints, authenticated
HTTP, React and Bot MCP. Only the native Runtime protocol is controlled. The second
uses the real installed Codex, an isolated Mock MCP and a controlled local model.
Neither mocks the product API, uses user credentials, downloads a Runtime or incurs
model charges. Each records source SHA/tree/dirty state, platform, steps, original
operation IDs and effect counts. A dirty diagnostic is not fixed-head evidence.
Processes/listeners/data belong only to the test; the daily App is never stopped.

## Current journey

1. A adds B by address/optional port and sends a request. B has no grant and A has
   no delegatable environment until B's local user approves.
2. A returns immediately. B approval does not continuously poll/wake A; A's manual
   check reads the original request and makes the ready environment available.
3. The reverse direction requires a separate request, decision and proof. Revoking
   A→B leaves B→A working. Reauthorization requires a new local B decision.
4. A uploads CSV/PDF/image input, selects B and creates/continues the original task.
5. B's original question/approval appears in A React. A's decision returns to the
   original native request/owner and B confirms continued execution once.
6. A's Agent reads actual result bytes; UI preview and authenticated download keep
   the original filename and content. Retrieved copies remain readable with B offline.
7. Create response loss reconciles the original operation without replay. Upload-
   period UI Stop cancels the original caller while uncertain remote effects remain
   unknown; a local Worker still completes without remote fallback or global failure.

Protocol/race tests additionally cover invalid addresses, request/decision loss,
fixed target identity, same-ID/different-content refusal, terminal IDs, expiry,
source/proof isolation, bounded inbox/body reads, generation-fenced Stop, 17 approvals,
completed-question history, failed SSE fallback and original-owner persistence.

## Native URL and evidence levels

The real-Codex command uses a temporary Mock MCP to trigger native URL elicitation,
then drives actual A/B/React. It asserts original task/thread/turn/request identity,
web side effect, native confirmation and exactly-once effects for six success/failure
scenarios. This is not third-party OAuth or real-account evidence.

The first version supports the [bounded URL contract](worker-peer-url-contract.md):
normal publicly reachable browser paths and the verified original-request static
HTTP form portal. B-only JS/GET/multistep/redirect/callback/browser-state cases remain
deferred. No automatic tunnel, arbitrary proxy or credential migration is added.

Real physical machines, account models and native GUI business journeys are labeled
separately and are not current merge blockers. Native preview screenshots prove only
shared React/WKWebView presentation with synthetic settings data.
