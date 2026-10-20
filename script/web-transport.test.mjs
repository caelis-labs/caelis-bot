import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {webRoutes} from '../frontend/src/web-routes.ts';

test('browser action paths match the explicit server allowlist',()=>{
 const source=readFileSync(new URL('../internal/websurface/routes.go',import.meta.url),'utf8');
 const paths=new Set([...source.matchAll(/Path: "([^"]+)"/g)].map(m=>m[1]));
 for(const [name,route] of Object.entries(webRoutes)){assert(paths.has(route.path),`${name} route missing`);assert(!/\/(?:wails|rpc|exec|shell)(?:\/|$)/i.test(route.path))}
 const names=Object.keys(webRoutes);
 for(const name of ['desktop.OpenMachineTerminal','desktop.OpenTaskTerminal','desktop.Notify','desktop.RequestSystemPermission','desktop.StartDrag','desktop.RevealArtifact'])assert(!names.includes(name),`${name} must remain a platform capability`);
});
