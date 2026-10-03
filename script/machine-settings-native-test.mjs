import {createServer} from 'vite';
import {spawn,execFileSync} from 'node:child_process';
import {mkdirSync,copyFileSync,writeFileSync,readFileSync,existsSync} from 'node:fs';
import {resolve,dirname} from 'node:path';

// Development-only data; no daily Bot profile, SSH or account calls.
const server=await createServer({server:{host:'127.0.0.1',port:0,strictPort:false}});
await server.listen();
try {
 mkdirSync('.cache/machine-settings-regression',{recursive:true});
 for(const lang of ['zh-CN','en']) {
  // Give each WebKit host its own bundle identity, including its XPC services.
  const bundle=resolve(dirname(process.argv[2]),`${lang}.app`), executable=resolve(bundle,'Contents/MacOS/fixture');
  mkdirSync(dirname(executable),{recursive:true});copyFileSync(process.argv[2],executable);
  writeFileSync(resolve(bundle,'Contents/Info.plist'),`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleExecutable</key><string>fixture</string><key>CFBundleIdentifier</key><string>dev.caelis.machine-regression.${lang}.${process.pid}</string><key>CFBundlePackageType</key><string>APPL</string></dict></plist>`);
  execFileSync('/usr/bin/codesign',['--force','--sign','-',bundle],{stdio:'inherit'});
  const url=`${server.resolvedUrls.local[0]}runtime-settings-preview.html?machine=caelis-advanced-reopen&lang=${lang}`;
  process.stdout.write(`Native machine fixture: ${url}\n`);
  const capture=resolve(`.cache/machine-settings-regression/${lang}-${process.pid}.png`), log=resolve(dirname(process.argv[2]),`${lang}.log`);
  const child=spawn('/usr/bin/open',['-W','-n','--stdout',log,'--stderr',log,bundle,'--args',url,resolve('script/machine-settings-regression.js'),capture],{stdio:'inherit'});
  const code=await new Promise((done,reject)=>{child.once('error',reject);child.once('exit',done);});
  if(existsSync(log))process.stdout.write(readFileSync(log));
  const result=existsSync(capture+'.json')?JSON.parse(readFileSync(capture+'.json','utf8')):null;
  if(code!==0||!result?.ok)throw Error(`Machine settings native regression failed (${lang}): ${result?.error??'host did not complete'}`);
  copyFileSync(capture,resolve(`.cache/machine-settings-regression/${lang}.png`));
 }
} finally {await server.close();}
