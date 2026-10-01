#!/usr/bin/env python3
"""Standard native setup protocol fixture: no threads, turns or auth effects."""
import json
import sys

if '--version' in sys.argv:
    print('codex-cli 0.159.2')
    raise SystemExit(0)
for line in sys.stdin:
    request = json.loads(line)
    if 'id' not in request:
        continue
    method = request.get('method')
    if method == 'initialize':
        result = {'userAgent': 'caelis-node-setup-fixture'}
    elif method == 'account/read':
        result = {'requiresOpenaiAuth': True, 'account': {'type': 'apiKey', 'apiKey': 'NATIVE_ONLY_FIXTURE_SECRET'}}
    elif method == 'model/list':
        result = {'data': [{'model': 'fixture-model', 'displayName': 'Fixture model',
                            'isDefault': True, 'defaultReasoningEffort': 'medium',
                            'supportedReasoningEfforts': [{'reasoningEffort': 'medium'}, {'reasoningEffort': 'high'}],
                            'serviceTiers': []}], 'nextCursor': None}
    else:
        print(json.dumps({'id': request['id'], 'error': {'code': -32601, 'message': 'No execution capability'}}), flush=True)
        continue
    print(json.dumps({'id': request['id'], 'result': result}), flush=True)
