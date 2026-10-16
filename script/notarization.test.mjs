import {test} from 'node:test';
import assert from 'node:assert/strict';
import {mkdtempSync, rmSync, writeFileSync, readFileSync, mkdirSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {spawnSync} from 'node:child_process';
import {validateResumeRun} from './verify-notarization-run.mjs';
import {staple} from './notarization-stapler.mjs';

// Diagnostic shape from the v0.14.0 mounted-App validation failure.
const lostConnection = 'error is Error Domain=NSURLErrorDomain Code=-1005 "The network connection was lost."\n' +
  "CloudKit's response is inconsistent with expections: (null)\nThe validate action failed! Error 68.\n";
const networkFailure = {status: 68, stdout: '', stderr: lostConnection};
async function ticketSequence(results, command = 'validate') {
  const calls = [], waits = [], logs = [];
  const status = await staple(command, '/fixture with spaces/Caelis Bot.app', {
    run: (binary, args) => {
      calls.push([binary, ...args]);
      assert.ok(calls.length <= results.length, 'unexpected extra attempt');
      return results[calls.length - 1];
    }, wait: async delay => { waits.push(delay); }, log: text => { logs.push(text); },
  });
  return {status, calls, waits, logs};
}

for (const command of ['staple', 'validate']) {
  test(`${command} retries the observed ticket network failure without changing the artifact`, async () => {
    const result = await ticketSequence([networkFailure, networkFailure, {status: 0, stdout: 'verified\n'}], command);
    assert.equal(result.status, 0);
    assert.deepEqual(result.waits, [5000, 15000]);
    assert.deepEqual(result.calls, Array(3).fill(['xcrun', 'stapler', command, '/fixture with spaces/Caelis Bot.app']));
    assert.equal(result.logs.filter(log => log.includes(lostConnection)).length, 2);
  });
}

test('persistent temporary ticket errors stop after three attempts and retain the exit code', async () => {
  const result = await ticketSequence(Array(3).fill(networkFailure));
  assert.equal(result.status, 68);
  assert.equal(result.calls.length, 3);
  assert.deepEqual(result.waits, [5000, 15000]);
});

for (const code of [-1001, -1004]) {
  test(`ticket transport error ${code} can recover on the next attempt`, async () => {
    const result = await ticketSequence([{status: 68, stderr: `Error Domain=NSURLErrorDomain Code=${code}`}, {status: 0}]);
    assert.equal(result.status, 0);
    assert.deepEqual(result.waits, [5000]);
  });
}

for (const failure of [
  {status: 65, stderr: lostConnection},
  {status: 66, stderr: 'Ticket missing or invalid'},
  {status: 77, stderr: 'Ticket revoked'},
  {status: 68, stderr: 'Record not found; not notarized'},
  {status: 68, stderr: 'The validate action failed! Error 68.'},
  {status: 68, stderr: 'Error Domain=NSURLErrorDomain Code=-1202 certificate trust failure'},
  {status: 68, stderr: lostConnection + '\nError Domain=NSURLErrorDomain Code=-1200'},
  {status: 68, stderr: lostConnection + '\nTicket invalid'},
  {status: 68, stderr: lostConnection + '\nSignature mismatch'},
  {status: 68, stderr: 'HTTP 403 Forbidden'},
  {status: 68, stderr: lostConnection + '\nHTTP 403 Forbidden'},
  {status: null, signal: 'SIGTERM', stderr: lostConnection},
  {status: null, error: new Error('spawn ETIMEDOUT'), stderr: lostConnection},
]) {
  test(`ticket validation fails closed: ${failure.stderr}`, async () => {
    const result = await ticketSequence([failure]);
    assert.notEqual(result.status, 0);
    assert.equal(result.calls.length, 1);
    assert.deepEqual(result.waits, []);
  });
}

test('a validation failure after a network retry still blocks publication immediately', async () => {
  const result = await ticketSequence([networkFailure, {status: 65, stderr: 'Ticket invalid'}]);
  assert.equal(result.status, 65);
  assert.equal(result.calls.length, 2);
  assert.deepEqual(result.waits, [5000]);
});

test('successful ticket validation has no retry delay', async () => {
  const result = await ticketSequence([{status: 0}]);
  assert.equal(result.status, 0);
  assert.deepEqual(result.waits, []);
});

test('ticket command failure prevents the publishing shell from continuing', () => {
  const root = mkdtempSync(join(tmpdir(), 'caelis-stapler-'));
  try {
    writeFileSync(join(root, 'xcrun'), '#!/bin/sh\necho "Ticket invalid" >&2\nexit 65\n', {mode: 0o700});
    const result = spawnSync('/bin/bash', ['-c', 'set -e; node "$1" validate "$2"; echo PUBLISH',
      'fixture', resolve('script/notarization-stapler.mjs'), join(root, 'App with spaces.app')], {
      encoding: 'utf8', timeout: 10000, env: {...process.env, PATH: `${root}:${process.env.PATH}`},
    });
    assert.equal(result.status, 65);
    assert.match(result.stderr, /Ticket invalid/);
    assert.doesNotMatch(result.stdout, /PUBLISH/);
  } finally { rmSync(root, {recursive: true, force: true}); }
});

const submission = '00000000-1111-2222-3333-444444444444';
function fixture(run) {
  const root = mkdtempSync(join(tmpdir(), 'caelis-notary-'));
  try {
    const reports = join(root, 'reports'), calls = join(root, 'calls');
    mkdirSync(reports);
    const artifact = join(root, 'app.zip');
    writeFileSync(artifact, 'exact signed artifact bytes');
    writeFileSync(join(root, 'xcrun'), `#!/usr/bin/env node
      const fs=require('node:fs'),crypto=require('node:crypto');
      const [tool,command,...args]=process.argv.slice(2);
      fs.appendFileSync(process.env.FIXTURE_CALLS,command+'\\n');
      const id='${submission}', status=process.env.FIXTURE_STATUS;
      if(tool!=='notarytool') process.exit(2);
      if(command==='submit') console.log(JSON.stringify({id}));
      else if(command==='info') {
        if(process.env.FIXTURE_INFO_ERROR==='1') process.exit(1);
        const waited=fs.readFileSync(process.env.FIXTURE_CALLS,'utf8').includes('wait\\n');
        console.log(JSON.stringify({id,status:waited?status:'In Progress'}));
      } else if(command==='wait') {
        if(!args.includes('60m')) process.exit(2);
        process.exit(1);
      } else if(command==='log') {
        const sha256=crypto.createHash('sha256').update(fs.readFileSync(process.env.FIXTURE_ARTIFACT)).digest('hex');
        fs.writeFileSync(args.at(-1),JSON.stringify({jobId:id,status,sha256:process.env.FIXTURE_WRONG_HASH==='1'?'wrong':sha256}));
      } else process.exit(2);
    `, {mode: 0o700});
    const invoke = (status, extra = {}) => spawnSync('/bin/bash', [resolve('script/notarize.sh'), artifact, 'app'], {
      encoding: 'utf8', timeout: 10000,
      env: {...process.env, PATH: `${root}:${process.env.PATH}`, BOT_NOTARY_REPORTS: reports,
        BOT_SIGN_KEYCHAIN: 'unused-fixture', BOT_NOTARY_WAIT_TIMEOUT: '60m', FIXTURE_CALLS: calls,
        FIXTURE_ARTIFACT: artifact, FIXTURE_STATUS: status, ...extra},
    });
    run({invoke, artifact, reports, calls, commands: () => readFileSync(calls, 'utf8').trim().split('\n')});
  } finally { rmSync(root, {recursive: true, force: true}); }
}

test('a wait timeout followed by Accepted continues after verifying the Apple digest', () => fixture(({invoke, commands}) => {
  const result = invoke('Accepted');
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(commands(), ['submit', 'info', 'wait', 'info', 'log']);
}));

test('In Progress is resumable and reuses the exact artifact and submission', () => fixture(({invoke, reports, commands}) => {
  assert.equal(invoke('In Progress').status, 75);
  assert.equal(JSON.parse(readFileSync(join(reports, 'app-submission.json'))).id, submission);
  assert.equal(JSON.parse(readFileSync(join(reports, 'app.json'))).status, 'In Progress');
  assert.equal(invoke('Accepted').status, 0);
  assert.deepEqual(commands(), ['submit', 'info', 'wait', 'info', 'info', 'log']);
}));

for (const status of ['Invalid', 'Rejected', 'Unexpected']) {
  test(`${status} never authorizes publication`, () => fixture(({invoke, commands}) => {
    assert.equal(invoke(status).status, 1);
    assert.equal(commands().includes('log'), status !== 'Unexpected');
  }));
}

test('changed artifacts and mismatched Apple receipts cannot resume', () => fixture(({invoke, artifact, commands}) => {
  assert.equal(invoke('Accepted', {FIXTURE_WRONG_HASH: '1'}).status, 1);
  const before = commands();
  writeFileSync(artifact, 'different bytes');
  assert.equal(invoke('Accepted').status, 1);
  assert.deepEqual(commands(), before, 'must reject changed bytes before contacting Apple');
}));

test('status-query errors preserve the upload receipt without resubmission', () => fixture(({invoke, reports, commands}) => {
  assert.equal(invoke('Accepted', {FIXTURE_INFO_ERROR: '1'}).status, 1);
  assert.equal(JSON.parse(readFileSync(join(reports, 'app-submission.json'))).id, submission);
  assert.equal(invoke('Accepted').status, 0);
  assert.equal(commands().filter(command => command === 'submit').length, 1);
}));

test('resume downloads are limited to completed main release runs in this repository', () => {
  const run = {id: 123, repository: {full_name: 'caelis-labs/caelis-bot'}, head_branch: 'main',
    status: 'completed', event: 'workflow_dispatch', path: '.github/workflows/release.yml', head_sha: 'a'.repeat(40)};
  const verify = (value, id = '123') => validateResumeRun(value, 'caelis-labs/caelis-bot', id);
  assert.equal(verify(run), run.head_sha);
  assert.equal(verify({...run, event: 'push', path: '.github/workflows/release-please.yml'}), run.head_sha);
  for (const change of [{repository: {full_name: 'other/repo'}}, {head_branch: 'feature'},
    {status: 'in_progress'}, {event: 'pull_request'}, {path: '.github/workflows/ci.yml'}, {head_sha: 'invalid'}]) {
    assert.throws(() => verify({...run, ...change}));
  }
  assert.throws(() => verify(run, '124'));
});
