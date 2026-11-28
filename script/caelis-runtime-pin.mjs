import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {appendFileSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join, resolve} from 'node:path';
import {pathToFileURL} from 'node:url';
import {releaseCommand} from './release-network.mjs';
import {validateTag} from './release-version.mjs';

const coreRepo = 'caelis-labs/caelis';
const botRepo = 'caelis-labs/caelis-bot';
const branch = 'automation/caelis-runtime-pin';
const branchRef = `refs/heads/${branch}`;
const managedCommit = 'Caelis-Bot-Pin: generated';
const runtimeFile = 'resources/caelis-runtime/release.json';
const protocolFile = 'protocol/caelis/manifest.json';
const schemaFile = 'protocol/caelis/openapi.json';
const wireFile = 'internal/backend/caelis/wire/control_v1.gen.go';
const licenseFile = 'protocol/caelis/LICENSE';
const pinFiles = [runtimeFile, protocolFile, schemaFile, wireFile, licenseFile];
const sha256 = /^[0-9a-f]{64}$/;
const sha1 = /^[0-9a-f]{40}$/;
const digest = bytes => createHash('sha256').update(bytes).digest('hex');
const json = path => JSON.parse(readFileSync(path, 'utf8'));
const formatJSON = value => JSON.stringify(value, null, 2) + '\n';

function compareTags(left, right) {
  const a = validateTag(left).split('.').map(BigInt);
  const b = validateTag(right).split('.').map(BigInt);
  const component = a.findIndex((number, index) => number !== b[index]);
  return component < 0 ? 0 : a[component] > b[component] ? 1 : -1;
}

export function selectRelease(current, release) {
  const tag = release.tag_name;
  if (release.draft !== false || release.prerelease !== false || !release.published_at) {
    return {message: 'Waiting for a published stable Caelis release.'};
  }
  const version = validateTag(tag);
  if (compareTags(tag, current.version) <= 0) {
    return {message: `No upgrade: main pins ${current.version}, GitHub Latest is ${tag}.`};
  }

  const targets = Object.fromEntries(Object.keys(current.targets).map(target => [
    target, `caelis_${version}_${target.replace('-', '_')}.tar.gz`,
  ]));
  const assets = {};
  for (const name of ['checksums.txt', ...Object.values(targets)]) {
    const matching = release.assets.filter(asset => asset.name === name);
    if (matching.length !== 1 || matching[0].state !== 'uploaded' || !Number.isSafeInteger(matching[0].size) || matching[0].size <= 0 ||
        !/^sha256:[0-9a-f]{64}$/.test(matching[0].digest)) {
      return {message: `Waiting for ${tag}: missing uploaded asset or digest for ${name}.`};
    }
    assets[name] = {sha256: matching[0].digest.slice(7), size: matching[0].size};
  }
  return {tag, targets, assets};
}

export function publishedChecksums(bytes, {assets, targets}) {
  if (digest(bytes) !== assets['checksums.txt'].sha256) throw Error('Caelis checksum file digest mismatch');
  const result = {};
  for (const artifact of Object.values(targets)) {
    const hashes = bytes.toString('utf8').split(/\r?\n/).flatMap(line => {
      const match = /^([0-9a-f]{64})\s+\*?(\S+)$/.exec(line);
      return match?.[2] === artifact ? [match[1]] : [];
    });
    if (hashes.length !== 1 || hashes[0] !== assets[artifact].sha256) {
      throw Error(`Caelis checksum missing, duplicated or different from GitHub asset digest: ${artifact}`);
    }
    result[artifact] = hashes[0];
  }
  return result;
}

function api(path, args = []) {
  return JSON.parse(releaseCommand('gh', ['api', path, ...args], {timeout: 60000}));
}

function botAPI(path, args) {
  return api(`repos/${botRepo}/${path}`, args);
}

export function pinRequests(pulls, filenames) {
  const owned = pulls.filter(pr => pr.head.ref === branch && pr.head.repo?.full_name === botRepo);
  if (owned.length > 1 || owned.some(pr => pr.base.ref !== 'main')) {
    throw Error(`Resolve the open ${branch} pull requests before updating the pin.`);
  }
  const competing = pulls.find(pr => pr.base.ref === 'main' && pr !== owned[0] &&
    filenames(pr.number).some(path => pinFiles.includes(path)));
  return {owned: owned[0], competing};
}

function openPinRequests() {
  const pages = ['--paginate', '--slurp'];
  return pinRequests(botAPI('pulls?state=open&per_page=100', pages).flat(), number => {
    const files = botAPI(`pulls/${number}/files?per_page=100`, pages).flat();
    // GitHub caps the endpoint at 3,000 files. An incomplete diff cannot rule out a pin.
    if (files.length >= 3000) throw Error(`Cannot check the full file list of PR #${number}.`);
    return files.flatMap(file => [file.filename, file.previous_filename]);
  });
}

function publicDownload(url, options = [], maxBytes = 8 * 1024 * 1024) {
  return releaseCommand('curl', [
    '--fail', '--silent', '--show-error', '--location', '--proto', '=https',
    '--max-time', '120', '--max-filesize', String(maxBytes), ...options, url,
  ], {encoding: 'buffer'});
}

function coreAPI(path) {
  return JSON.parse(publicDownload(`https://api.github.com/repos/${coreRepo}/${path}`));
}

function downloadPin(current, candidate) {
  const {tag, targets, assets} = candidate;
  const download = `https://github.com/${coreRepo}/releases/download/${tag}`;
  const commit = coreAPI(`commits/${tag}`).sha;
  if (!sha1.test(commit)) throw Error(`Caelis tag ${tag} did not resolve to a commit.`);
  const checksums = publishedChecksums(publicDownload(`${download}/checksums.txt`, [], 65536), candidate);
  const directory = mkdtempSync(join(tmpdir(), 'caelis-runtime-pin-'));
  const verified = {};
  try {
    for (const [target, artifact] of Object.entries(targets)) {
      const archive = join(directory, artifact);
      publicDownload(`${download}/${artifact}`, ['--output', archive], assets[artifact].size);
      const binary = target.startsWith('windows-') ? 'caelis.exe' : 'caelis';
      const binaryHash = command('python3', ['script/caelis-runtime.py', 'inspect-pin-archive', archive, binary, checksums[artifact]]).trim();
      if (!sha256.test(binaryHash)) throw Error(`Invalid Caelis binary digest for ${target}.`);
      verified[target] = {artifact, sha256: checksums[artifact], binary_sha256: binaryHash};
    }
  } finally {
    rmSync(directory, {recursive: true, force: true});
  }

  const source = path => publicDownload(`https://raw.githubusercontent.com/${coreRepo}/${commit}/${path}`);
  const schema = source('api/control/v1/openapi.json');
  const upstreamWire = source('control/appserver/wirev1/generated/control_v1.gen.go').toString('utf8');
  const wire = upstreamWire.replace(/^package generated$/m, 'package wire');
  if (wire === upstreamWire) throw Error('Unexpected package in generated Caelis Go wire.');
  const license = source('LICENSE');
  return {
    [runtimeFile]: formatJSON({version: tag, commit, team: current.team, targets: verified}),
    [protocolFile]: formatJSON({
      ...json(protocolFile), commit, schemaSha256: digest(schema), wireSha256: digest(wire), version: tag,
    }),
    [schemaFile]: schema,
    [wireFile]: wire,
    [licenseFile]: license,
  };
}

function command(name, args, options = {}) {
  return execFileSync(name, args, {encoding: 'utf8', timeout: 60000, maxBuffer: 16 * 1024 * 1024, ...options});
}

const git = (...args) => command('git', args).trim();

function deferForPR(pr) {
  console.log(`Open PR #${pr.number} already changes the Caelis pin or contract. Waiting for its resolution.`);
}

function prepare(candidateOnly) {
  const current = json(runtimeFile);
  const release = coreAPI('releases/latest');
  const candidate = selectRelease(current, release);
  if (!candidate.tag) return console.log(candidate.message);
  if (!candidateOnly) {
    const {competing} = openPinRequests();
    if (competing) return deferForPR(competing);
  }
  if (git('status', '--porcelain')) throw Error('Prepare the Caelis pin in a clean worktree.');
  for (const [path, contents] of Object.entries(downloadPin(current, candidate))) {
    writeFileSync(path, contents);
  }
  command('node', ['script/check-caelis-protocol.mjs'], {stdio: 'inherit'});
  console.log(`Verified ${candidate.tag} archive digests and generated the runtime and protocol pin.`);
  if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, 'ready=true\n');
}

function branchHead() {
  return botAPI(`git/matching-refs/heads/${branch}`).find(item => item.ref === branchRef)?.object.sha;
}

function stillCurrent(main, basePin, pin) {
  const candidate = selectRelease(basePin, coreAPI('releases/latest'));
  if (candidate.tag !== pin.version || coreAPI(`commits/${pin.version}`).sha !== pin.commit ||
      Object.entries(pin.targets).some(([target, info]) =>
        candidate.targets[target] !== info.artifact || candidate.assets[info.artifact].sha256 !== info.sha256) ||
      botAPI('git/ref/heads/main').object.sha !== main) {
    console.log('Main or GitHub Latest changed during preparation. Deferring to the next run.');
    return false;
  }
  return true;
}

function publish() {
  if (process.env.GITHUB_REPOSITORY !== botRepo || process.env.GITHUB_REF !== 'refs/heads/main' ||
      !process.env.GH_TOKEN || !/^[a-zA-Z0-9-]+$/.test(process.env.PIN_APP_SLUG ?? '')) {
    throw Error('Publishing requires the Bot main workflow and its repository-scoped GitHub App token.');
  }
  const pin = json(runtimeFile);
  const main = git('rev-parse', 'HEAD');
  const basePin = JSON.parse(git('show', `HEAD:${runtimeFile}`));
  if (compareTags(pin.version, basePin.version) <= 0) throw Error('Expected a newer Caelis pin.');
  command('node', ['script/check-caelis-protocol.mjs'], {stdio: 'inherit'});

  const {owned, competing} = openPinRequests();
  if (competing) return deferForPR(competing);
  const author = `${process.env.PIN_APP_SLUG}[bot]`;
  if (owned && owned.user?.login !== author) throw Error('The pin branch has a PR opened outside automation.');
  const previous = branchHead();
  if (owned && owned.head.sha !== previous) throw Error('Pin PR head changed while reading the branch.');
  if (previous) {
    git('fetch', '--no-tags', 'origin', previous);
    const base = git('merge-base', main, previous);
    const changes = git('diff', '--name-only', base, previous).split('\n').filter(Boolean);
    const message = git('log', '-1', '--format=%B', previous);
    if (!message.endsWith(`\n\n${managedCommit}`) || changes.some(path => !pinFiles.includes(path))) {
      throw Error(`The ${branch} branch contains changes outside automation ownership.`);
    }
    const previousPin = JSON.parse(git('show', `${previous}:${runtimeFile}`));
    if (compareTags(previousPin.version, pin.version) > 0) {
      return console.log(`The managed branch already pins the newer ${previousPin.version}.`);
    }
  }

  git('add', '--', ...pinFiles);
  if (git('diff', '--cached', '--name-only').split('\n').some(path => path && !pinFiles.includes(path))) {
    throw Error('The staged pin includes changes outside automation ownership.');
  }
  const tree = git('write-tree');
  const title = `fix: bundle Caelis ${pin.version}`;
  const body = [
    `Pin the bundled Caelis runtime and public Control contract to [${pin.version}](https://github.com/${coreRepo}/releases/tag/${pin.version}) at \`${pin.commit}\`.`,
    'Verified the published archive checksums against GitHub asset digests and recorded the bundled executable digests. Product checks and maintainer review apply to this PR.',
  ].join('\n\n');

  if (!stillCurrent(main, basePin, pin)) return;
  if (branchHead() !== previous) throw Error('Pin branch changed during preparation.');
  const sameTreeAndBase = previous && git('rev-parse', `${previous}^{tree}`) === tree &&
    git('merge-base', main, previous) === main;
  let head = previous;
  if (!sameTreeAndBase) {
    // Keep current main as an ancestor while preserving the existing PR branch history.
    const parents = previous ? ['-p', previous] : [];
    if (!previous || git('merge-base', main, previous) !== main) parents.push('-p', main);
    const actorID = api(`users/${encodeURIComponent(author)}`).id;
    if (!Number.isSafeInteger(actorID) || actorID <= 0) throw Error('Could not resolve the GitHub App bot identity.');
    head = git('-c', `user.name=${author}`, '-c', `user.email=${actorID}+${author}@users.noreply.github.com`,
      'commit-tree', tree, ...parents, '-m', title, '-m', managedCommit);
    git('-c', 'credential.helper=', '-c', 'credential.helper=!gh auth git-credential', 'push', 'origin', `${head}:${branchRef}`);
  }
  const current = openPinRequests();
  if (current.competing) return deferForPR(current.competing);
  if ((current.owned && (current.owned.head.sha !== head || current.owned.user?.login !== author)) ||
      branchHead() !== head) {
    throw Error('Pin branch changed before updating the PR.');
  }
  if (!current.owned) {
    // A push may have succeeded on a previous run even if PR creation did not.
    const result = writePull('POST', 'pulls', {base: 'main', head: branch, title, body});
    console.log(`Opened ${result.html_url}`);
  } else if (current.owned.title !== title || current.owned.body !== body) {
    const result = writePull('PATCH', `pulls/${current.owned.number}`, {title, body});
    console.log(`Updated ${result.html_url}`);
  } else {
    console.log(sameTreeAndBase ? `The managed PR is current: ${current.owned.html_url}` :
      `Updated the managed PR head: ${current.owned.html_url}`);
  }
}

function writePull(method, path, fields) {
  const args = Object.entries(fields).flatMap(([key, value]) => ['-f', `${key}=${value}`]);
  return botAPI(path, ['--method', method, ...args]);
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const [mode] = process.argv.slice(2);
  try {
    if (mode === 'prepare') prepare(false);
    else if (mode === 'candidate') prepare(true);
    else if (mode === 'publish') publish();
    else throw Error('Usage: node script/caelis-runtime-pin.mjs prepare|candidate|publish');
  } catch (error) {
    console.error(String(error.stderr || error.message).trim());
    process.exitCode = 1;
  }
}
