import {spawnSync} from 'node:child_process';
import {setTimeout} from 'node:timers/promises';
import {pathToFileURL} from 'node:url';

// EX_NOHOST alone also means not notarized. Require a known transient transport
// error; never retry invalid/revoked tickets, TLS trust failures or unknown errors.
export function transientTicketError(status, output) {
  if (status !== 68 || /invalid|revok|rejected|mismatch|not (?:code-)?signed|not (?:previously )?notarized|\bHTTP(?:\/[\d.]+)?\s+(?:401|403)\b/i.test(output)) return false;
  const codes = [...output.matchAll(/\bDomain=NSURLErrorDomain\s+Code=(-?\d+)\b/g)].map(match => Number(match[1]));
  return codes.length > 0 && codes.every(code => [-1001, -1004, -1005].includes(code));
}

export async function staple(command, artifact, {run = spawnSync, wait = setTimeout, log = text => process.stderr.write(text)} = {}) {
  if (!['staple', 'validate'].includes(command) || !artifact) throw new Error('Expected staple|validate and one artifact path');
  const delays = [5000, 15000];
  for (let attempt = 0; ; attempt++) {
    const result = run('xcrun', ['stapler', command, artifact], {encoding: 'utf8', timeout: 120000, maxBuffer: 8 * 1024 * 1024});
    const output = (result.stdout ?? '') + (result.stderr ?? '');
    log(output);
    if (result.error || result.signal) {
      log(`stapler did not complete: ${result.error?.message ?? result.signal}\n`);
      return 1;
    }
    if (result.status === 0) return 0;
    if (attempt === delays.length || !transientTicketError(result.status, output)) return result.status ?? 1;
    log(`Temporary Apple ticket network error; retry ${attempt + 1}/${delays.length} in ${delays[attempt] / 1000}s.\n`);
    await wait(delays[attempt]);
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  if (process.argv.length !== 4) throw new Error('Expected staple|validate and one artifact path');
  process.exitCode = await staple(...process.argv.slice(2));
}
