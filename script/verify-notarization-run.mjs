import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {pathToFileURL} from 'node:url';

export function validateResumeRun(run, repository, runId) {
  assert.equal(String(run.id), runId);
  assert.equal(run.repository?.full_name, repository);
  assert.equal(run.head_branch, 'main');
  assert.equal(run.status, 'completed');
  assert.ok(['push', 'workflow_dispatch'].includes(run.event));
  assert.ok(['.github/workflows/release.yml', '.github/workflows/release-please.yml'].includes(run.path));
  assert.match(run.head_sha, /^[a-f0-9]{40}$/);
  return run.head_sha;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [file, repository, runId] = process.argv.slice(2);
  console.log(validateResumeRun(JSON.parse(readFileSync(file, 'utf8')), repository, runId));
}
