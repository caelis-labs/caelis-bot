import {createHash} from 'node:crypto';
import {readFileSync} from 'node:fs';
import assert from 'node:assert/strict';
const manifest=JSON.parse(readFileSync('protocol/caelis/manifest.json','utf8'));
const hash=path=>createHash('sha256').update(readFileSync(path)).digest('hex');
assert.match(manifest.commit,/^[0-9a-f]{40}$/);
assert.equal(hash('protocol/caelis/openapi.json'),manifest.schemaSha256,'Caelis public schema drift');
assert.equal(hash('internal/backend/caelis/wire/control_v1.gen.go'),manifest.wireSha256,'Caelis generated wire drift');
const schemas=JSON.parse(readFileSync('protocol/caelis/openapi.json','utf8')).components.schemas;
const profile=schemas.ApplicationProfile;
const patch=schemas.ApplicationConfigurationPatch;
for(const field of ['mcp_servers','skill_dirs','skill_roots']){
  assert.ok(profile.properties[field],`Caelis creation profile lacks ${field}`);
  assert.ok(patch.properties[field],`Caelis configuration patch lacks ${field}`);
  assert.equal(patch.properties[field]['x-go-omitzero'],true,`Caelis patch cannot distinguish omitted from [] for ${field}`);
}
assert.equal(schemas.CreateApplicationSessionRequest.allOf[1].properties.profile.$ref,'#/components/schemas/ApplicationProfile');
assert.equal(schemas.ApplicationConfiguration.properties.profile.$ref,'#/components/schemas/ApplicationProfile');
assert.ok(readFileSync('protocol/caelis/LICENSE','utf8').length>100);
console.log(`Caelis public protocol pinned to ${manifest.commit.slice(0,12)}.`);
