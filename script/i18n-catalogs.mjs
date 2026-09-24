import { readdirSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const tokens=text=>[...new Set([...text.matchAll(/\{([A-Za-z][A-Za-z0-9_]*)\}/g)].map(m=>m[1]))].sort();
export function validateCatalogs(en,zh,label='catalog') {
 const fail=message=>{throw new Error(`${label}: ${message}`);};
 if(JSON.stringify(Object.keys(en).sort())!==JSON.stringify(Object.keys(zh).sort()))fail('language keys differ');
 for(const key of Object.keys(en)) {
  if(!/^[a-zA-Z][a-zA-Z0-9_.]*$/.test(key))fail(`invalid key ${key}`);
  let expected;
  for(const [locale,catalog] of [['en',en],['zh-CN',zh]]) {
   const entry=catalog[key], plural=typeof entry!=='string';
   if(plural!== (typeof en[key]!=='string'))fail(`${key}: string/plural mismatch`);
   if(plural&&(!entry||typeof entry!=='object'||Array.isArray(entry)||typeof entry.other!=='string'||(locale==='en'&&typeof entry.one!=='string')||Object.keys(entry).some(k=>!['one','other'].includes(k))))fail(`${key}: invalid plural forms`);
   const forms=plural?Object.values(entry):[entry];
   for(const text of forms){
    if(typeof text!=='string'||!text.trim())fail(`${key}: empty message`);
    const actual=JSON.stringify(tokens(text));
    expected??=actual;if(expected!==actual)fail(`${key}: placeholders differ`);
    if(plural&&!tokens(text).includes('count'))fail(`${key}: plural requires {count}`);
    if(/[{}]/.test(text.replace(/\{[A-Za-z][A-Za-z0-9_]*\}/g,'')))fail(`${key}: unsupported placeholder syntax`);
   }
  }
 }
}
export function checkCatalogs(){
 const root=resolve('internal/i18n/locales');
 const en=readdirSync(`${root}/en`).filter(f=>f.endsWith('.json')).sort();
 const zh=readdirSync(`${root}/zh-CN`).filter(f=>f.endsWith('.json')).sort();
 if(JSON.stringify(en)!==JSON.stringify(zh))throw new Error('language namespaces differ');
 for(const name of en)validateCatalogs(JSON.parse(readFileSync(`${root}/en/${name}`)),JSON.parse(readFileSync(`${root}/zh-CN/${name}`)),name);
 return en;
}
if(process.argv[1]&&resolve(process.argv[1])===fileURLToPath(import.meta.url)){
 const namespaces=checkCatalogs();console.log(`i18n: ${namespaces.length} bilingual catalogs verified`);
}
