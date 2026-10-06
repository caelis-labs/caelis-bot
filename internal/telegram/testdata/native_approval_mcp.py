#!/usr/bin/env python3
"""Disposable MCP tool for opt-in native approval acceptance."""

import json
import os
import sys


def reply(request, value=None, error=None):
    response = {"jsonrpc": "2.0", "id": request["id"]}
    response["error" if error else "result"] = error if error else value
    print(json.dumps(response, separators=(",", ":")), flush=True)


for line in sys.stdin:
    try:
        request = json.loads(line)
    except ValueError:
        break
    if "id" not in request:
        continue
    method = request.get("method")
    if method == "initialize":
        reply(request, {"protocolVersion": "2025-06-18", "capabilities": {"tools": {}},
                        "serverInfo": {"name": "bot-approval-fixture", "version": "1"}})
    elif method == "ping":
        reply(request, {})
    elif method == "tools/list":
        reply(request, {"tools": [{"name": "fixture_approval_probe",
                                  "description": "Record one synthetic approval probe. Call only when explicitly instructed by the test user.",
                                  "inputSchema": {"type": "object", "properties": {}, "additionalProperties": False}}]})
    elif method == "tools/call" and request.get("params", {}).get("name") == "fixture_approval_probe":
        marker = os.environ["BOT_APPROVAL_TEST_MARKER"]
        with open(marker, "x", encoding="utf-8") as output:
            output.write("one synthetic call\n")
        reply(request, {"content": [{"type": "text", "text": "APPROVAL_PROBE_DONE"}]})
    else:
        reply(request, error={"code": -32601, "message": "unsupported fixture method"})
