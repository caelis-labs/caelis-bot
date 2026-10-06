import {readFileSync} from 'node:fs';
import {pathToFileURL} from 'node:url';

export const pin = JSON.parse(readFileSync(new URL('../resources/desktop-world/release.json', import.meta.url)));

export function targetPin(target) {
  const selected = pin.targets[target];
  if (!selected || !['darwin-arm64', 'windows-amd64'].includes(target)) {
    throw new Error(`Unsupported Desktop World target: ${target}`);
  }
  if (!selected.archive.startsWith(`desktop-world-${pin.version}-${target}.`) ||
      !/^[a-f0-9]{64}$/.test(selected.sha256) ||
      !/^[a-f0-9]{64}$/.test(selected.binary_sha256)) throw new Error('Invalid Desktop World pin');
  return selected;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [target, field] = process.argv.slice(2);
  const selected = targetPin(target);
  if (!['archive', 'sha256', 'binary', 'binary_sha256'].includes(field)) throw new Error('Unknown pin field');
  console.log(selected[field]);
}
