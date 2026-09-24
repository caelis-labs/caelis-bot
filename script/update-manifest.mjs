import {createHash, createPublicKey, verify} from 'node:crypto';
import {readFileSync, writeFileSync} from 'node:fs';
import {join} from 'node:path';
import {pathToFileURL} from 'node:url';
import {validateTag} from './release-version.mjs';
import {validateUpdateKey} from './configure-updates.mjs';

export const digest = data => createHash('sha256').update(data).digest('hex');
export function verifySignature(data, signature, key) {
  validateUpdateKey(key, true);
  const publicKey = createPublicKey({format:'der', type:'spki', key:Buffer.concat([
    Buffer.from('302a300506032b6570032100','hex'), Buffer.from(key,'base64'),
  ])});
  if (!/^[A-Za-z0-9+/]{86}==$/.test(signature) || !verify(null, data, publicKey, Buffer.from(signature,'base64'))) {
    throw new Error('Invalid update signature');
  }
}
export function validateManifest(value, tag) {
  const version = validateTag(tag);
  if (version.includes('-')) throw new Error('R2 only publishes stable releases');
  if (value.schema !== 1 || value.tag !== tag || value.version !== version ||
      value.file !== `Caelis-Bot-${version}-macos-arm64.dmg` ||
      !/^[a-f0-9]{40}$/.test(value.source) || !/^[a-f0-9]{64}$/.test(value.sha256) ||
      !/^[a-f0-9]{64}$/.test(value.appcastSHA256) || !Number.isSafeInteger(value.length) || value.length <= 0) {
    throw new Error('Invalid update manifest');
  }
  return value;
}
export function verifySignedFeed(feed, key) {
  // Sparkle 2.10 signs the exact prefix and appends this byte-length receipt.
  const xml=feed.toString('utf8');
  const receipt=xml.match(/<!-- sparkle-signatures:\s*edSignature: ([A-Za-z0-9+/=]+)\s*length: (\d+)\s*-->\s*$/);
  if(!receipt || Number(receipt[2])!==Buffer.byteLength(xml.slice(0,receipt.index))) throw new Error('Missing or malformed signed feed receipt');
  verifySignature(feed.subarray(0,Number(receipt[2])),receipt[1],key);
}
export function verifyDirectory(directory, tag, key) {
  const bytes = readFileSync(join(directory, 'latest.json'));
  verifySignature(bytes, readFileSync(join(directory, 'latest.json.sig'),'utf8').trim(), key);
  const manifest = validateManifest(JSON.parse(bytes), tag);
  const dmg = readFileSync(join(directory, manifest.file));
  const feed = readFileSync(join(directory, 'appcast.xml'));
  if (digest(dmg) !== manifest.sha256 || dmg.length !== manifest.length || digest(feed) !== manifest.appcastSHA256) {
    throw new Error('Update artifact digest mismatch');
  }
  // Verify that the feed offers these exact bytes and that its archive signature
  // matches the public key embedded in the app (wrong CI key must fail closed).
  const xml = feed.toString('utf8');
  verifySignedFeed(feed, key);
  const enclosure = xml.match(/<enclosure\s[^>]*>/g) ?? [];
  if (enclosure.length !== 1 || !enclosure[0].includes(`url="https://releases.caelis.dev/caelis-bot/releases/${tag}/${manifest.file}"`) ||
      !enclosure[0].includes(`length="${manifest.length}"`) || !xml.includes(`<sparkle:version>${manifest.version}</sparkle:version>`)) {
    throw new Error('Appcast does not describe the verified release');
  }
  verifySignature(dmg, enclosure[0].match(/sparkle:edSignature="([^"]+)"/)?.[1] ?? '', key);
  const checksum = readFileSync(join(directory, `${manifest.file}.sha256`),'utf8').trim();
  if (checksum !== `${manifest.sha256}  ${manifest.file}`) throw new Error('DMG checksum mismatch');
  return manifest;
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [command, directory] = process.argv.slice(2), tag = process.env.BOT_RELEASE_TAG;
  if (command === 'create') {
    const version = validateTag(tag), file = `Caelis-Bot-${version}-macos-arm64.dmg`;
    const bytes = readFileSync(join(directory, file));
    const manifest = validateManifest({schema:1, tag, version, file, source:process.env.BOT_RELEASE_SOURCE_SHA,
      length:bytes.length, sha256:digest(bytes), appcastSHA256:digest(readFileSync(join(directory,'appcast.xml')))}, tag);
    writeFileSync(join(directory, 'latest.json'), JSON.stringify(manifest, null, 2)+'\n');
  } else if (command === 'verify') {
    verifyDirectory(directory, tag, process.env.BOT_SPARKLE_PUBLIC_KEY);
  } else throw new Error('Expected create or verify');
}
