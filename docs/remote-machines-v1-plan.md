# Remote machines

Remote environments are other running Bots. The first version has one flow:

1. A opens **Remote machines → Add machine**, enters B's reachable IP/address and
   optional port (default 9317), and sends a connection request.
2. B opens **Remote machines → Connection requests**, checks the source name,
   observed address and identity fingerprint, then allows or declines.
3. A checks manually or reenters Remote machines. Only an approved, native-ready
   connection is offered for delegation. A never waits continuously for approval.

A→B does not authorize B→A. There are no verification codes, account center, sharing
switch, helper environment or automatic discovery. Standard SSH channels can supply
connectivity through the same Bot protocol; Tailscale remains external. Connectivity
never grants execution. See [SSH channels](ssh-connections.md).

A's existing task manager remains the product task authority. B's native adapter
owns execution, with authenticated source/task/file/interaction isolation. B shows
connection authorization requests, never the delegated tasks or their approvals.
Questions, approvals, output, history and results return to A through shared Worker
contracts. Existing new-protocol tasks keep their native owner across disconnects;
this is execution correctness, not a compatibility layer for the retired experiment.

See [the implementation](bot-to-bot.md), [UI](remote-machines-ui.md) and
[bounded URL contract](worker-peer-url-contract.md). Real account/physical-machine
or GUI business acceptance is evidence to label honestly, not a current merge gate.
