import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {createHash} from 'node:crypto';
import {readFileSync, readdirSync, readlinkSync, statSync} from 'node:fs';
import {basename, dirname, join, relative} from 'node:path';
import {gzipSync} from 'node:zlib';

const digest = path => createHash('sha256').update(readFileSync(path)).digest('hex');
const sum = values => values.reduce((total, value) => total + value, 0);

function contents(root) {
 const entries = {};
 const visit = directory => {
  for (const entry of readdirSync(directory, {withFileTypes: true})) {
   const path = join(directory, entry.name);
   const key = relative(root, path);
   if (entry.isDirectory()) visit(path);
   if (entry.isSymbolicLink()) entries[key] = {target: readlinkSync(path)};
   if (entry.isFile()) entries[key] = {size: statSync(path).size, sha256: digest(path)};
  }
 };
 visit(root);
 return entries;
}

function measure(checkout, packageDir) {
 const bundle = join(packageDir, 'Caelis Bot Dev.app');
 const archive = packageDir + '.zip';
 const extracted = join(dirname(packageDir), 'extracted-' + basename(packageDir), basename(bundle));
 const entries = contents(bundle);
 assert.deepEqual(entries, contents(extracted), 'Archive extraction changed bundle files or links');
 const files = Object.fromEntries(Object.entries(entries).filter(([, entry]) => 'size' in entry));
 const links = Object.values(entries).filter(entry => 'target' in entry);
 const metadata = execFileSync('plutil', ['-extract', 'CaelisSourceCommit', 'raw', join(bundle, 'Contents/Info.plist')], {encoding: 'utf8'});
 const listing = execFileSync('zipinfo', ['-v', archive], {encoding: 'utf8'});
 const result = {
  commit: metadata.trim(),
  appLogicalBytes: sum(Object.values(files).map(file => file.size)),
  appRegularFiles: Object.keys(files).length,
  appSymlinks: links.length,
  appSymlinkTargetBytes: sum(links.map(link => Buffer.byteLength(link.target))),
  mainExecutable: files['Contents/MacOS/caelis-bot'].size,
  coreSHA256: files['Contents/Helpers/caelis'].sha256,
  zipBytes: statSync(archive).size,
  zipSHA256: digest(archive),
  zipMethods: [...new Set([...listing.matchAll(/^\s*compression method:\s*(.+)$/gm)].map(match => match[1]))].sort(),
  extractedRegularFilesAndSymlinksMatch: true,
 };
 const assets = join(checkout, 'frontend/dist/assets');
 for (const type of ['js', 'css']) {
  const payloads = readdirSync(assets).filter(path => path.endsWith('.'+type)).map(path => readFileSync(join(assets, path)));
  assert.ok(payloads.length, `No built ${type} assets in ${assets}`);
  result[type+'Raw'] = sum(payloads.map(file => file.length));
  result[type+'Gzip'] = sum(payloads.map(file => gzipSync(file, {level: 6}).length));
 }
 for (const group of ['Contents/Frameworks/', 'Contents/Resources/']) {
  result[group] = Object.fromEntries(Object.entries(files).filter(([key]) => key.startsWith(group)));
 }
 return result;
}

const args = process.argv.slice(2);
assert.equal(args.length, 4, 'Pass baseline checkout, baseline package directory, POC checkout and POC package directory');
const baseline = measure(args[0], args[1]);
const poc = measure(args[2], args[3]);
assert.equal(baseline.coreSHA256, poc.coreSHA256, 'Embedded Core differs');
const same = {core: true};
for (const group of ['Contents/Frameworks/', 'Contents/Resources/']) {
 assert.deepEqual(baseline[group], poc[group], group+' differs');
 same[group] = true;
 delete baseline[group];
 delete poc[group];
}
const delta = Object.fromEntries(Object.entries(baseline).filter(([, value]) => typeof value === 'number').map(([key, value]) => [key, poc[key]-value]));
console.log(JSON.stringify({baseline, poc, delta, same}, null, 2));
