import {execFileSync} from 'node:child_process';
import {pathToFileURL} from 'node:url';
import {compareStable} from './publish-r2.mjs';
import {validateTag} from './release-version.mjs';

const repository = 'caelis-labs/caelis-bot';
export function syncLatest(tag, run = (cmd, args) => execFileSync(cmd, args, {encoding:'utf8', stdio:['ignore','pipe','pipe']})) {
  validateTag(tag);
  const release = JSON.parse(run('gh', ['api', `repos/${repository}/releases/tags/${tag}`]));
  if (release.tag_name !== tag || release.draft || release.prerelease ||
      !release.assets?.some(asset => asset.name === `Caelis-Bot-${tag.slice(1)}-macos-arm64-stable.publication.json`)) {
    throw new Error('Stable macOS publication receipt is missing');
  }
  const published = JSON.parse(run('gh', ['api', `repos/${repository}/releases?per_page=100`]));
  if (published.some(item => !item.draft && !item.prerelease && /^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(item.tag_name) &&
      compareStable(item.tag_name, tag) > 0)) return 'Newer published Stable retained';
  let latest;
  try { latest = JSON.parse(run('gh', ['api', `repos/${repository}/releases/latest`])); }
  catch (error) { if (!String(error.stderr || error).includes('HTTP 404')) throw error; }
  if (latest?.tag_name === tag) return 'GitHub Latest already current';
  if (latest?.tag_name && compareStable(latest.tag_name, tag) > 0) return 'Newer GitHub Latest retained';
  run('gh', ['release', 'edit', tag, '--repo', repository, '--latest']);
  const after = JSON.parse(run('gh', ['api', `repos/${repository}/releases/latest`]));
  if (after.tag_name !== tag) throw new Error('GitHub Latest readback differs');
  return `GitHub Latest now ${tag}`;
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  console.log(syncLatest(process.env.BOT_RELEASE_TAG));
}
