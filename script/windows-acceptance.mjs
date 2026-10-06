import {readFileSync} from 'node:fs';
import {join} from 'node:path';
import {validateTag} from './release-version.mjs';

export function windowsAcceptanceName(tag, channel) {
  if (channel !== 'stable' && channel !== 'preview') throw new Error('Invalid Windows channel');
  return `Caelis-Bot-${validateTag(tag)}-windows-amd64-${channel}.acceptance.json`;
}

const required = [
  'tray', 'petVisible', 'petDrag', 'bubble', 'chat', 'settings', 'history',
  'approvals', 'attachments', 'runtime', 'memory', 'tasks', 'telegramProtocol',
  'desktopWorld', 'namedPipePeer', 'credentialManager', 'ownedProcesses',
  'unicodePaths', 'webview2', 'cleanInstall', 'upgrade', 'uninstall', 'dataPreserved',
];

export function validateWindowsAcceptance(directory, {tag, source, channel, artifact, sha256}) {
  const record = JSON.parse(readFileSync(join(directory, windowsAcceptanceName(tag, channel))));
  if (record.schema !== 1 || record.tag !== tag || record.source !== source ||
      record.platform !== 'windows-amd64' || record.channel !== channel ||
      record.package?.name !== artifact || record.package?.sha256 !== sha256 ||
      record.signing?.authenticode !== true || record.signing?.timestamp !== true ||
      !/^[a-fA-F0-9]{40,64}$/.test(record.signing?.thumbprint ?? '') ||
      required.some(name => record.checks?.[name] !== true)) {
    throw new Error('Windows native acceptance manifest incomplete or mismatched');
  }
  return record;
}
