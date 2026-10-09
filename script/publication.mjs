import {execFileSync} from 'node:child_process';
import {mkdtempSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {join, resolve} from 'node:path';
import {tmpdir} from 'node:os';
import {pathToFileURL} from 'node:url';
import {digest} from './update-manifest.mjs';
import {validateTag} from './release-version.mjs';
import {validateWindowsAcceptance, windowsAcceptanceName} from './windows-acceptance.mjs';

const repository = 'caelis-labs/caelis-bot';
const platforms = new Set(['macos-arm64', 'windows-amd64']);
const channels = new Set(['stable']);

export function publicationKey(tag, os, arch, channel) {
  const version = validateTag(tag);
  if (!platforms.has(`${os}-${arch}`) || !channels.has(channel)) throw new Error('Unsupported publication target');
  return `Caelis-Bot-${version}-${os}-${arch}-${channel}.publication.json`;
}

export function receiptFor(directory, {tag, source, os, arch, channel, validation, artifact}) {
  const version = validateTag(tag);
  const key = publicationKey(tag, os, arch, channel);
  if (!/^[a-f0-9]{40}$/.test(source)) throw new Error('Invalid release source');
  if (os === 'windows' && validation !== 'windows-11-native-accepted') throw new Error('Windows native acceptance required');
  if (os === 'macos' && validation !== 'developer-id-notarized-stapled-gatekeeper') throw new Error('Mac distribution validation required');
  const file = os === 'macos' ? `Caelis-Bot-${version}-macos-arm64.dmg` : artifact;
  if (os === 'windows' && file !== `Caelis-Bot-${version}-windows-amd64.msix`) throw new Error('Invalid Windows release artifact');
  const names = [file, `${file}.sha256`];
  if (os === 'windows') names.push(windowsAcceptanceName(tag, channel));
  if (os === 'macos') names.push('appcast.xml', 'latest.json', 'latest.json.sig');
  const assets = names.map(name => {
    const bytes = readFileSync(join(directory, name));
    if (bytes.length === 0) throw new Error(`Empty release asset: ${name}`);
    return {name, sha256: digest(bytes), length: bytes.length};
  });
  const checksum = readFileSync(join(directory, `${file}.sha256`), 'utf8').trim();
  if (checksum !== `${assets[0].sha256}  ${file}`) throw new Error('Release checksum mismatch');
  if (os === 'windows') validateWindowsAcceptance(directory, {tag, source, channel, artifact:file, sha256:assets[0].sha256});
  if (os === 'macos') {
    const manifest = JSON.parse(readFileSync(join(directory, 'latest.json')));
    if (manifest.tag !== tag || manifest.source !== source || manifest.sha256 !== assets[0].sha256 ||
        manifest.appcastSHA256 !== assets[2].sha256 || manifest.channel !== undefined) throw new Error('Mac feed source or asset mismatch');
  }
  return {schema: 1, key: {tag, os, arch, channel}, source, validation,
    state: 'published', assets, receipt: key};
}

export function validateReceipt(value) {
  if (value?.schema !== 1 || value.state !== 'published' || typeof value.validation !== 'string' ||
      !/^[a-f0-9]{40}$/.test(value.source) || !Array.isArray(value.assets) || value.assets.length < 2 ||
      value.receipt !== publicationKey(value.key?.tag, value.key?.os, value.key?.arch, value.key?.channel)) throw new Error('Invalid publication receipt');
  const seen = new Set();
  for (const asset of value.assets) {
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(asset.name) || !/^[a-f0-9]{64}$/.test(asset.sha256) ||
        !Number.isSafeInteger(asset.length) || asset.length <= 0 || seen.has(asset.name)) throw new Error('Invalid publication asset');
    seen.add(asset.name);
  }
  return value;
}

// run is injectable so unknown upload outcomes and late platform arrival are
// covered without modifying public releases in tests.
export function publishPlatform(directory, options, run = (command, args) => execFileSync(command, args, {encoding: 'utf8'})) {
  const receipt = validateReceipt(receiptFor(directory, options));
  const {tag} = receipt.key;
  const commit = JSON.parse(run('gh', ['api', `repos/${repository}/commits/${tag}`]));
  if (commit.sha !== receipt.source) throw new Error('Immutable release tag source mismatch');
  const release = () => {
    try {
      return JSON.parse(run('gh', ['api', `repos/${repository}/releases/tags/${tag}`]));
    } catch (error) {
      // GitHub's release-by-tag endpoint can return 404 for an authenticated
      // draft. Only that response may fall back to the newest release page;
      // the exact tag and draft state still have to match before any upload.
      if (!String(error.stderr || error).includes('HTTP 404')) throw error;
      const releases = JSON.parse(run('gh', ['api', `repos/${repository}/releases?per_page=100`]));
      const matches = releases.filter(candidate => candidate.tag_name === tag && candidate.draft);
      if (matches.length !== 1) throw error;
      return matches[0];
    }
  };
  let current = release();
  if (current.tag_name !== tag || (!current.draft && current.prerelease)) {
    throw new Error('Release metadata does not match source tag');
  }
  const temporary = mkdtempSync(join(tmpdir(), 'caelis-publication-'));
  try {
    const ensure = (name, expected, source = join(directory, name)) => {
      const existing = current.assets?.find(asset => asset.name === name);
      if (existing) {
        const target = join(temporary, name);
        run('gh', ['release', 'download', tag, '--repo', repository, '--pattern', name, '--dir', temporary, '--clobber']);
        if (digest(readFileSync(target)) !== expected) throw new Error(`Immutable release asset differs: ${name}`);
        return;
      }
      let uploadError;
      try { run('gh', ['release', 'upload', tag, source, '--repo', repository]); }
      catch (error) { uploadError = error; }
      // Even an unknown upload outcome is reconciled against the same name
      // and exact bytes. A missing name remains unknown; never resubmit here.
      current = release();
      if (!current.assets?.some(asset => asset.name === name)) {
        if (uploadError) throw uploadError;
        throw new Error(`Release asset upload not visible: ${name}`);
      }
      const target = join(temporary, name);
      run('gh', ['release', 'download', tag, '--repo', repository, '--pattern', name, '--dir', temporary, '--clobber']);
      if (digest(readFileSync(target)) !== expected) throw new Error(`Immutable release asset differs: ${name}`);
    };
    for (const asset of receipt.assets) ensure(asset.name, asset.sha256);
    const path = join(temporary, receipt.receipt);
    writeFileSync(path, JSON.stringify(receipt, null, 2) + '\n');
    ensure(receipt.receipt, digest(readFileSync(path)), path);
    if (current.draft) {
      // Keep the release draft until the verified assets and receipt exist.
      let editError;
      try { run('gh', ['release', 'edit', tag, '--repo', repository, '--draft=false', '--prerelease=false', '--latest=false']); }
      catch (error) { editError = error; }
      current = release();
      if (current.draft && editError) throw editError;
    }
    if (current.draft) throw new Error('Release remains a draft');
    if (current.tag_name !== tag || current.prerelease) throw new Error('Published release metadata mismatch');
    return receipt;
  } finally { rmSync(temporary, {recursive: true, force: true}); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [directory, os = 'macos', arch = 'arm64', channel = 'stable'] = process.argv.slice(2);
  const options = {tag: process.env.BOT_RELEASE_TAG, source: process.env.BOT_RELEASE_SOURCE_SHA,
    os, arch, channel, validation: process.env.BOT_RELEASE_VALIDATION};
  console.log(JSON.stringify(publishPlatform(resolve(directory), options).key));
}
