import {createInterface} from 'node:readline';
import * as sdk from '@trycua/cua-driver';
import {Desktop, envelope} from './desktop.mjs';
import {randomUUID} from 'node:crypto';

const reply = value => process.stdout.write(JSON.stringify(value, (_, v) => typeof v === 'bigint' ? String(v) : v) + '\n');
const requests = createInterface({input: process.stdin, crlfDelay: Infinity})[Symbol.asyncIterator]();
const focusWindow = async target => {
  // Only the native owner can answer this private pipe exchange. It is never
  // an MCP tool or model-supplied native PID/window handle.
  const id = randomUUID();
  reply({nativeFocus: {...target, id}});
  const next = await requests.next();
  if (next.done || Buffer.byteLength(next.value) > 1024) return false;
  const result = JSON.parse(next.value);
  return result.focusResult === id && result.ok === true;
};
const desktop = new Desktop(sdk.CuaDriver.create(), sdk);
try {
  for await (const line of {[Symbol.asyncIterator]: () => requests}) {
    try {
      if (Buffer.byteLength(line) > 65536) throw new Error('request_too_large');
      const r = JSON.parse(line);
      desktop.setTurn(r.turn);
      desktop.focusWindow = r.nativeFocusAvailable === true ? focusWindow : undefined;
      let state;
      if (r.name === 'bot_desktop_observe') state = await desktop.observe(r.arguments);
      else if (r.name === 'bot_desktop_authorize') state = await desktop.authorize(r.arguments);
      else if (r.name === 'bot_desktop_perform') state = await desktop.perform(r.arguments);
      else throw new Error('unknown_tool');
      reply(envelope(state));
    } catch (error) {
      // Native error strings can contain app content. Emit only bounded error
      // codes we own, not raw exception text or supplied text/credentials.
      const state = {status: error.mayHaveActed ? 'unknown' : 'rejected',
        message: error.code && /^[a-z_]{1,90}$/.test(error.code) ? error.code : 'desktop_driver_error',
        instruction: 'Check app permissions and observe again. Never replay an input with an unknown effect.'};
      reply({...envelope(state), isError: true});
    }
  }
} finally { await desktop.close(); }
