// Isolated real OpenSSH transport; generated test keys, no user SSH config/keys.
import {execFileSync,spawn} from 'node:child_process';
import {writeFile,mkdir} from 'node:fs/promises';
import {createServer} from 'node:net';
import {join} from 'node:path';
import {userInfo} from 'node:os';
export async function sshFixture(root,children){
 const dir=join(root,'ssh');await mkdir(dir,{mode:0o700});
 const key=join(dir,'identity'),hostkey=join(dir,'hostkey');
 for(const p of [key,hostkey])execFileSync('/usr/bin/ssh-keygen',['-q','-t','ed25519','-N','','-f',p]);
 const s=createServer();await new Promise(r=>s.listen(0,'127.0.0.1',r));const port=s.address().port;await new Promise(r=>s.close(r));
 const quote=p=>'"'+p.replaceAll('"','\\"')+'"',user=userInfo().username;
 const file=join(dir,'sshd_config');await writeFile(file,`Port ${port}\nListenAddress 127.0.0.1\nHostKey ${quote(hostkey)}\nPidFile ${quote(join(dir,'pid'))}\nAuthorizedKeysFile ${quote(key+'.pub')}\nStrictModes no\nPasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nAllowUsers ${user}\n`,{mode:0o600});
 const child=spawn('/usr/sbin/sshd',['-D','-e','-f',file],{stdio:['ignore','ignore','pipe']});children.push(child);let diagnostic='';child.stderr.on('data',v=>diagnostic+=v);
 await new Promise(r=>setTimeout(r,150));if(child.exitCode!==null)throw Error('isolated SSH fixture did not start: '+diagnostic);
 const config=`Host e2e-bot\n HostName 127.0.0.1\n Port ${port}\n User ${user}\n IdentityFile ${quote(key)}\n IdentitiesOnly yes\n UserKnownHostsFile ${quote(join(dir,'known_hosts'))}\n`;
 const fingerprint=execFileSync('/usr/bin/ssh-keygen',['-lf',hostkey+'.pub'],{encoding:'utf8'}).split(' ')[1];return {config,fingerprint};
}
