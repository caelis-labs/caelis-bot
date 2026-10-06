// Independent business-state readback for the disposable local Chrome fixture.
// Chrome must use a private profile with --remote-debugging-port=0.
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';

const profile = process.argv[2];
if (!profile) throw new Error('pass the isolated Chrome profile directory');
const port = Number(readFileSync(resolve(profile, 'DevToolsActivePort'), 'utf8').split('\n')[0]);
const url = pathToFileURL(resolve('experiments/desktop-control/browser.html')).href;
const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const page = pages.find(item => item.type === 'page' && item.url === url);
if (!page || pages.filter(item => item.type === 'page').length !== 1) {
  throw new Error('isolated profile does not have exactly the synthetic page');
}
const result = await new Promise((resolveResult, reject) => {
  const socket = new WebSocket(page.webSocketDebuggerUrl);
  const timer = setTimeout(() => { socket.close(); reject(new Error('readback timeout')); }, 3000);
  socket.addEventListener('open', () => socket.send(JSON.stringify({id: 1, method: 'Runtime.evaluate', params: {
    expression: '({title:document.title,checked:document.getElementById("check").checked,clicks:document.getElementById("status").textContent,field:document.getElementById("field").value,drag:document.getElementById("dragstatus").textContent})',
    returnByValue: true,
  }})));
  socket.addEventListener('message', event => {
    const message = JSON.parse(event.data);
    if (message.id !== 1) return;
    clearTimeout(timer);
    socket.close();
    if (message.error || message.result?.exceptionDetails) reject(new Error('synthetic page evaluation failed'));
    else resolveResult(message.result.result.value);
  });
  socket.addEventListener('error', () => { clearTimeout(timer); reject(new Error('synthetic page socket failed')); });
});
console.log(JSON.stringify(result));
