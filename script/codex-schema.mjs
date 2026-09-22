import { spawnSync } from 'node:child_process';
import { mkdirSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const pin = JSON.parse(readFileSync(path.join(root, 'toolchain.json'), 'utf8')).codex;
const binary = process.env.CODEX_BIN ?? 'codex';
const version = spawnSync(binary, ['--version'], { encoding: 'utf8' });
if (version.error || version.status !== 0 || version.stdout.trim() !== `codex-cli ${pin}`) {
  throw new Error(`Expected codex-cli ${pin}; review protocol compatibility before changing toolchain.json`);
}
const output = path.join(root, '.cache/codex-schema', pin);
mkdirSync(output, { recursive: true });
const generated = spawnSync(binary, ['app-server', 'generate-ts', '--out', output], { stdio: 'inherit' });
if (generated.error || generated.status !== 0) throw new Error('Codex schema generation failed');
console.log(`Generated stable native protocol types in ${output}`);

const jsonOutput = path.join(root, '.cache/codex-json-schema', pin);
mkdirSync(jsonOutput, { recursive: true });
const native = spawnSync(binary, ['app-server', 'generate-json-schema', '--out', jsonOutput], { stdio: 'inherit' });
if (native.error || native.status !== 0) throw new Error('Codex JSON schema generation failed');
const vendored = path.join(root, 'internal/backend/codex/schema');
const manifest = JSON.parse(readFileSync(path.join(vendored, 'manifest.json'), 'utf8'));
if (manifest.codex !== pin) throw new Error('Native schema / toolchain version mismatch');
const experimentalOutput = path.join(root,'.cache/codex-json-schema-experimental',pin);
mkdirSync(experimentalOutput,{ recursive:true });
const experimental = spawnSync(binary,['app-server','generate-json-schema','--experimental','--out',experimentalOutput],{ stdio:'inherit' });
if (experimental.error || experimental.status !== 0) throw new Error('Experimental native schema generation failed');
for (const file of Object.keys(manifest.files)) {
  const generatedRoot = manifest.experimentalFiles?.includes(file) ? experimentalOutput : jsonOutput;
  if (!readFileSync(path.join(generatedRoot, file)).equals(readFileSync(path.join(vendored, file)))) {
    throw new Error(`Consumed native schema drifted: ${file}; review adapter compatibility`);
  }
}
console.log(`Consumed Go adapter schemas match Codex ${pin}.`);
