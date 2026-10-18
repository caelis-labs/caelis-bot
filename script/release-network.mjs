import {execFileSync} from 'node:child_process';
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';

export const networkDelays = [5000, 15000, 30000, 60000];
export function transientNetwork(error) {
  // execFile's message includes argv (and may include "certificate" in a URL).
  // Classify the diagnostic, never the name of the resource being fetched.
  const output = String(error.stderr || '').trim() || String(error.message || error);
  if (/certificate|SSL|TLS|unauthorized|forbidden|invalid|mismatch|revoked|rejected|(?:HTTP(?:\/\d[.\d]*)?\s+|returned error:\s*)(?:400|401|403|404|422)\b/i.test(output)) return false;
  const apple = [...output.matchAll(/Domain=NSURLErrorDomain\s+Code=(-?\d+)/g)].map(m => Number(m[1]));
  if (apple.length) return apple.every(code => [-1001, -1004, -1005].includes(code));
  return /ECONNRESET|ETIMEDOUT|EAI_AGAIN|connection (?:(?:was )?reset|refused|was lost)|network connection was lost|unexpected EOF|Empty reply from server|transfer closed with .* outstanding|end of response with .* missing|i\/o timeout|Could not (?:resolve host|connect to server)|Failed to connect|Operation timed out|(?:HTTP(?:\/\d[.\d]*)?\s+|returned error:\s*)(?:429|500|502|503|504)\b/i.test(output);
}
export function safeNetworkRead(command, args) {
  if (command === 'xcrun') return args[0] === 'notarytool' && ['info','log'].includes(args[1]);
  if (command === 'gh') return args[0] === 'api' && !args.some(a => ['-X','--method','-f','-F','--field','--raw-field','--input'].includes(a)) ||
    args[0] === 'release' && args[1] === 'download' && args.includes('--clobber');
  if (command === 'curl' || command === 'curl.exe') return args.includes('--fail') && !args.some(a => ['-X','--request','-d','--data','-T','--upload-file','-k','--insecure'].includes(a));
  return false;
}
// Only reads are replayed. Uploads and publication mutations reconcile their
// existing object/receipt at the owning boundary instead of replaying requests.
export function releaseCommand(command, args, options = {}, run = execFileSync, wait = ms => Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms)) {
  for (let attempt = 0; ; attempt++) {
    try { return run(command, args, {encoding:'utf8', timeout:180000, maxBuffer:16*1024*1024, ...options}); }
    catch (error) {
      if (attempt === networkDelays.length || !safeNetworkRead(command,args) || !transientNetwork(error)) throw error;
      process.stderr.write(`Temporary release network failure; retry ${attempt+1}/${networkDelays.length}.\n`);
      wait(networkDelays[attempt]);
    }
  }
}
if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const [command,...args] = process.argv.slice(2);
  if (!safeNetworkRead(command,args)) throw Error('Only allowlisted release reads may use this retry command');
  try { process.stdout.write(releaseCommand(command,args)); }
  catch (error) { process.stderr.write(String(error.stderr || error.message)); process.exitCode=error.status || 1; }
}
