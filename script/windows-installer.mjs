import {readdirSync, writeFileSync} from 'node:fs';
import {join, relative, resolve} from 'node:path';
import {pathToFileURL} from 'node:url';

// Escape compile-time text independently from NSIS runtime variables. Paths
// containing quotes/newlines cannot be represented safely as installed names.
function literal(value) {
  if (/[\r\n"\0]/.test(value)) throw new Error('Unsupported installer path');
  return value.replaceAll('$', '$$');
}

export function installerInventory(directory) {
  const root = resolve(directory), files = [], folders = [];
  const walk = dir => {
    for (const entry of readdirSync(dir, {withFileTypes:true}).sort((a,b)=>a.name.localeCompare(b.name,'en'))) {
      const path = join(dir,entry.name), name = relative(root,path).replaceAll('\\','/');
      literal(name);
      if (entry.isSymbolicLink()) throw new Error('Installer payload must not contain links');
      if (entry.isDirectory()) {folders.push(name); walk(path);}
      else if (entry.isFile()) files.push(name);
      else throw new Error('Unsupported installer payload');
    }
  };
  walk(root);
  if (!files.includes('caelis-bot.exe') || !files.includes('build.json') || !files.includes('Resources/LICENSE')) throw new Error('Incomplete Windows payload');
  return {files,folders};
}

export function generateInstaller(app, work, {name,id,version,output,icon,nsisLicense,revision=1}) {
  if (!['dev.caelis.bot.dev','dev.caelis.bot.preview'].includes(id)) throw new Error('Unsigned NSIS is limited to development/preview');
  if (!/^\d+\.\d+\.\d+(?:-[\w.-]+)?$/.test(version) || !Number.isInteger(revision) || revision<0 || revision>65535) throw new Error('Invalid installer version');
  const {files,folders}=installerInventory(app);
  const definitions={BOT_NAME:name,BOT_ID:id,BOT_VERSION:version,BOT_NUMERIC_VERSION:`${version.split('-')[0]}.${revision}`,BOT_OUTPUT:resolve(output),BOT_ICON:resolve(icon),BOT_LICENSE:resolve(app,'Resources/LICENSE'),BOT_NSIS_LICENSE:resolve(nsisLicense)};
  const lines=Object.entries(definitions).map(([key,value])=>`!define ${key} "${literal(value)}"`);
  lines.push('!macro GuardPayloadDirectories');
  for (const name of folders) {
    lines.push(`  System::Call 'kernel32::GetFileAttributesW(w "$INSTDIR\\${literal(name.replaceAll('/','\\'))}") i.r0'`,
      '  ${If} $0 != -1','    IntOp $1 $0 & 0x400','    ${If} $1 != 0',
      '      SetErrorLevel 104','      IfSilent +2','        MessageBox MB_OK|MB_ICONSTOP "$(WrongOwner)"','      Quit','    ${EndIf}','  ${EndIf}');
  }
  lines.push('!macroend');
  lines.push('!macro InstallPayload');
  for (const name of files) {
    const parts=name.split('/'); parts.pop();
    const target=parts.length ? '\\'+literal(parts.join('\\')) : '';
    lines.push(`  SetOutPath "$INSTDIR${target}"`,`  File "${literal(resolve(app,name))}"`);
  }
  lines.push('!macroend','!macro UninstallPayload');
  for (const name of files) lines.push(`  Delete "$INSTDIR\\${literal(name.replaceAll('/','\\'))}"`);
  for (const name of folders.sort((a,b)=>b.split('/').length-a.split('/').length)) lines.push(`  RMDir "$INSTDIR\\${literal(name.replaceAll('/','\\'))}"`);
  lines.push('!macroend','');
  writeFileSync(join(work,'package.nsh'),lines.join('\n'));
  writeFileSync(join(work,'inventory.json'),JSON.stringify({files,folders},null,2)+'\n');
}

if (process.argv[1] && import.meta.url===pathToFileURL(process.argv[1]).href) {
  const [app,work,options]=process.argv.slice(2);
  generateInstaller(app,work,JSON.parse(options));
}
