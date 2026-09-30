# Linux Codex primary Bot acceptance

The opt-in fixture at `4235d3bc4bfc3ff820689fcb243932d845841c93` passed against the existing Linux-B Codex CLI
0.159.2. The production product code was unchanged from baseline
`a04d6e77289110c68ad76ea442ac20f86ad8b49e`. This was a real foreground Linux
`serve-bot` owner with a synthetic loopback Responses provider, fresh private
HOME/CODEX_HOME and an allowlisted environment. No user authentication was read,
no runtime was installed and no paid model request was made. Existing strict SSH
trust was preserved without keys or forwarding.

The final `TestNativeLinuxCodexHeadlessProduct` run passed with `-race -count=1`:

- Native Bot initialization, a resident thread and native turns were observed.
- One accepted product response was deliberately dropped. The client reported
  unknown, then recovered the original accepted receipt on a new paired stdio
  connection. The submitted native intent reached the provider exactly once.
- Closing the observer during an active native turn retained the owner, native
  children and listener. Reconnection recovered the original receipt; releasing
  the synthetic provider completed that turn without replay.
- The Bot invoked one local Worker through the native advertised MCP namespace.
  The actual resident rollout records the MCP call on its activating native turn;
  the Worker has a native thread/turn and completed with outcome accepted. Exactly
  one synthetic Worker request reached the provider. The synthetic review response
  was valid JSON for this exact authorized action; native approval policy stayed
  unchanged. Local work retains its existing native admission semantics; this
  evidence does not invent a foreign `WorkDispatchSource` grant.
- Explicit Stop returned its accepted receipt, exited the owner and released its
  listener. All 23 captured descendant process identities were gone, checked with
  PID plus `/proc` start time. The foreground supervisor exited and its own random
  temporary root was removed.

`make check`, `make smoke`, ad-hoc `make build`, and affected
`go test -race ./internal/productrpc ./cmd/caelis-node` all passed on the frozen
source. Smoke initialized only: no conversation or model request. The build was
not launched, installed, signed for release or notarized.

SHA-256 evidence:

| Artifact | SHA-256 |
| --- | --- |
| Linux node binary | `f3f7e4fffae44b0a89e262b80570959f8c7d8e4e3ba7ac4bd1c0d654c2a73ee4` |
| Target fixture helper | `492474b9fb4f9331002b24b1590a5929feaf4958afa3909343aa7b50442afe00` |
| Final native test log | `5046e05c5336e197fccc5c4d18238a58f8e872495eedc844a43e78be36f5c028` |
| affected-race-final log | `b33b93e9350aaa10319fe72be0e163e1c9d2cfa0cf75547960b834d7ac8dd8df` |
| make-check log | `00f747511cee0a71c292e6b62afc5bdced21efd36f66b8413d08eff606bba491` |
| make-smoke log | `6e6988fb62fde8acfac059843f5d3a845965e06ef72baa9b5b046bd4d1065287` |
| make-build log | `835495cb7a651b4fac88691eb68aff075628ae2d04a17f8e7793c412ac0276f7` |

Earlier fixture attempts are retained in private ignored evidence. They corrected
an evidence path (Codex uses the profile's legacy conversation layout), namespace
framing, invalid synthetic review JSON and a mistaken foreign-source assertion
on the local route. Their owned processes and roots were cleaned. Core-only passes
before those corrections did not establish Worker execution; the final test now
requires native MCP dispatch and accepted Worker completion.

Linux-B to another Linux host over SSH, paid-model entitlement, user credential
handoff, persistent service installation and native Mac renderer behavior remain
separate gates. No Bot skill update is needed: this change adds opt-in acceptance
coverage without changing capabilities or tool workflows. Setup and lifecycle
instructions are in `internal/productrpc/testdata/codex_headless_fixture.md`.
