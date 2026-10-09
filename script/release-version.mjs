import {readFileSync} from 'node:fs'
import {pathToFileURL} from 'node:url'

// These values also become file names, plist fields and linker arguments.
const versionPattern = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-([a-zA-Z0-9-]+(?:\.[a-zA-Z0-9-]+)*))?$/
function validVersion(version) {
  const match = versionPattern.exec(version)
  return match && match[0] === version && !(match[4] ?? '').split('.').some(part => /^0\d+$/.test(part))
}
export function validateTag(tag) {
  if (typeof tag !== 'string' || !tag.startsWith('v') || !validVersion(tag.slice(1))) {
    throw new Error('Expected a v-prefixed semantic release tag without build metadata')
  }
  return tag.slice(1)
}
export function releaseChannel(tag) {
  const version = validateTag(tag)
  return /-dev\.[1-9]\d*$/.test(version) ? 'dev' : version.includes('-') ? 'preview' : 'stable'
}
export function releaseVersion(version, tag) {
  if (!validVersion(version)) throw new Error('Invalid package version')
  if (tag) {
    const tagged = validateTag(tag)
    if (tagged !== version && !new RegExp(`^${version.replaceAll('.', '\\.')}\\-dev\\.[1-9]\\d*$`).test(tagged)) {
      throw new Error('Release tag does not match package.json')
    }
  }
  const published = tag ? validateTag(tag) : null;
  const channel = published ? releaseChannel(tag) : 'local';
  const base = version.split('-')[0];
  let bundleVersion = base;
  if (channel === 'stable' || channel === 'dev') {
    const [major, minor, patch] = base.split('.').map(Number);
    const iteration = channel === 'stable' ? 999 : Number(published.match(/-dev\.(\d+)$/)[1]);
    if (!Number.isSafeInteger(patch * 1000 + iteration) || iteration > 998 && channel === 'dev') {
      throw new Error('Dev iteration exceeds supported Sparkle build range (1..998)');
    }
    bundleVersion = `${major}.${minor}.${patch * 1000 + iteration}`;
  }
  return {version: published ?? `${version}${version.includes('-') ? '.' : '-'}dev`, bundleVersion};
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url)))
  console.log(JSON.stringify(releaseVersion(pkg.version, process.env.BOT_RELEASE_TAG)))
}
