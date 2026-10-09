import {readFileSync} from 'node:fs';
import {pathToFileURL} from 'node:url';

export function verifyCandidateRun(run, repository, id) {
  if (run.id !== Number(id) || run.repository?.full_name !== repository ||
      run.path !== '.github/workflows/release.yml' || run.event !== 'workflow_dispatch' ||
      run.head_branch !== 'main' || run.status !== 'completed' || run.conclusion !== 'success') {
    throw new Error('Candidate must come from a successful main-branch preparation run in this repository');
  }
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  verifyCandidateRun(JSON.parse(readFileSync(process.argv[2])), process.argv[3], process.env.CANDIDATE_RUN_ID);
}
