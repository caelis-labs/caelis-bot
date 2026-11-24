// Keep the embedded runtime metadata accepted by the product asset contract.
import {readFileSync, writeFileSync} from 'node:fs';
import {runtimeExtras} from '../../script/asset-pack.mjs';

const [input, output] = process.argv.slice(2);
if (!input || !output) throw Error('Usage: node art/tools/finish-glb.mjs INPUT.glb OUTPUT.glb');
const source = readFileSync(input);
const end = 20 + source.readUInt32LE(12);
const document = JSON.parse(source.subarray(20, end));
function clean(value) {
  if (!value || typeof value !== 'object') return;
  if (value.extras) {
    for (const name of Object.keys(value.extras)) {
      if (!runtimeExtras.has(name)) delete value.extras[name];
    }
    if (!Object.keys(value.extras).length) delete value.extras;
  }
  for (const [name, child] of Object.entries(value)) {
    if (name !== 'extras') clean(child);
  }
}
clean(document);
const json = Buffer.from(JSON.stringify(document));
const padding = Buffer.alloc((4 - json.length % 4) % 4, 32);
const header = Buffer.from(source.subarray(0, 20));
header.writeUInt32LE(20 + json.length + padding.length + source.length - end, 8);
header.writeUInt32LE(json.length + padding.length, 12);
writeFileSync(output, Buffer.concat([header, json, padding, source.subarray(end)]));
