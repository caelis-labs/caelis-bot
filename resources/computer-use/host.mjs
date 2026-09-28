import {createInterface} from 'node:readline';
import * as sdk from '@trycua/cua-driver';
import {Desktop} from './desktop.mjs';

const desktop = new Desktop(sdk.CuaDriver.create(), sdk);
const reply = value => process.stdout.write(JSON.stringify(value, (_, v) => typeof v === 'bigint' ? String(v) : v) + '\n');
function envelope(state) {
  const images = state._images ?? [];
  delete state._images;
  if (state.observation && typeof state.observation === 'object') delete state.observation._images;
  return {content: [{type: 'text', text: JSON.stringify(state)},
    ...images.map(i => ({type: 'image', mimeType: i.mimeType, data: i.dataBase64}))], structuredContent: state};
}
try {
  for await (const line of createInterface({input: process.stdin, crlfDelay: Infinity})) {
    try {
      if (Buffer.byteLength(line) > 65536) throw new Error('request_too_large');
      const r = JSON.parse(line);
      let state;
      if (r.name === 'bot_desktop_observe') state = await desktop.observe(r.arguments);
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
