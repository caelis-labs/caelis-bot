# Worker peer final audit

Current scope: [Bot-only asynchronous target approval](remote-machines-ui.md).
The retired code/SSH/helper experiment is not a compatibility target. Local Worker
and new-protocol original-owner correctness remain required.

## Current review closure checklist

| Finding | Fix/evidence required |
| --- | --- |
| Different target certifies old request | Persist B identity before POST; request Target check; original address/peer/proof/epoch read; address repair and publication CAS tests. |
| Terminal ID revival | Retain original identity/content evidence; capacity refuses new requests rather than deleting terminal IDs. |
| HTTP error becomes known refusal | Only complete bounded expected 4xx fixed responses are terminal; truncation/5xx/loss remain unknown. |
| Unauthenticated partial body holds connections | Pair-only short read deadline; no global short deadline on SSE/uploads. |
| Failed archive hides recovery | Restore the prior profile when persistence fails. |
| Broken incoming registry breaks App | Peer-only fail-closed issue/503; local engine, Workers and ordinary HTTP remain available. |
| First host exit before delivery | Persist profile/proof/intent before ingress; pin before POST; lack of pin with no active invocation certifies no dispatch. |

The independent reviewer records exact-source conclusions before Ready. Test evidence
identifies source SHA/tree and whether the worktree was dirty. Earlier audit SHAs,
including a2e94a82, remain historical evidence only; their green CI is not reused as
the new request protocol's final gate.

## Required regression boundaries

Native owner isolation, same native IDs across sources, frozen task routing, guarded
Stop, original decision receipts, full pending questions/approvals, bounded observation,
output/history preservation, scoped files/offline copies and local health remain in
the suite. No pending machine becomes an execution authority or an A-wide lifecycle
lock. B shows connection requests but never foreign Worker tasks or interactions.

## Support and acceptance

Keep the [bounded URL contract](worker-peer-url-contract.md). Publicly reachable
browser paths (including reachable callback prerequisites) and verified static forms
are supported; B-only JS/callback/stateful-browser paths are deferred explicitly.
Real account, physical-machine and GUI business evidence are not current merge
blockers and are never claimed from fixtures or synthetic native screenshots.

A candidate must bind final source to check/smoke/build, affected races, both actual
application E2E commands and Product CI. No automatic merge, release, installation,
daily-service restart or credential copy is authorized by passing those checks.
