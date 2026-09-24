import {spawnSync} from 'node:child_process';
import {pathToFileURL} from 'node:url';

export const updateURL = 'https://releases.caelis.dev/caelis-bot/appcast.xml';
export function validateUpdateKey(key, required) {
  if (!key && !required) return '';
  if (!/^[A-Za-z0-9+/]{43}=$/.test(key ?? '') || Buffer.from(key, 'base64').length !== 32) {
    throw new Error('BOT_SPARKLE_PUBLIC_KEY must be the persistent Sparkle Ed25519 public key (32 bytes, base64)');
  }
  return key;
}
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const key = validateUpdateKey(process.env.BOT_SPARKLE_PUBLIC_KEY, Boolean(process.env.BOT_RELEASE_TAG));
  // Development bundles never check or replace themselves with a production app.
  const enabled = Boolean(key && process.env.BOT_RELEASE_TAG && !process.env.BOT_RELEASE_TAG.includes('-'));
  const values = {
    CaelisAutoUpdatesEnabled: ['bool', enabled ? 'true' : 'false'],
    SUFeedURL: ['string', updateURL],
    SUEnableAutomaticChecks: ['bool', 'true'],
    SUScheduledCheckInterval: ['integer', '86400'],
    SUAutomaticallyUpdate: ['bool', 'false'],
    SUAllowsAutomaticUpdates: ['bool', 'false'],
    SUEnableSystemProfiling: ['bool', 'false'],
    SUVerifyUpdateBeforeExtraction: ['bool', 'true'],
    SURequireSignedFeed: ['bool', 'true'],
    ...(key ? {SUPublicEDKey: ['string', key]} : {}),
  };
  for (const [name, [type, value]] of Object.entries(values)) {
    const result = spawnSync('/usr/libexec/PlistBuddy', ['-c', `Add ${name} ${type} ${value}`, process.argv[2]], {stdio: 'inherit'});
    if (result.status !== 0) process.exit(1);
  }
}
