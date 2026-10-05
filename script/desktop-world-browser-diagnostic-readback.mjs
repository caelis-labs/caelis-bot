// Read the new disposable browser diagnostic without using Desktop World.
import {readFileSync, writeFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';

const [profile, screenshot, variant] = process.argv.slice(2);
if (!profile) throw new Error('pass an isolated Chrome profile');
const port = Number(readFileSync(resolve(profile, 'DevToolsActivePort'), 'utf8').split('\n')[0]);
const direct = variant === 'direct';
const pointer = variant === 'pointer';
const expected = pathToFileURL(resolve(`experiments/desktop-control/browser-rc2-${pointer ? 'pointer' : direct ? 'direct' : 'diagnostic'}.html`)).href;
const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const page = pages.find(item => item.type === 'page' && item.url === expected);
if (!page || pages.filter(item => item.type === 'page').length !== 1) throw new Error('unexpected diagnostic page');
const socket = new WebSocket(page.webSocketDebuggerUrl);
const result = await new Promise((done, fail) => {
  let stage = 'read';
  let state;
  const timer = setTimeout(() => { socket.close(); fail(new Error('diagnostic readback timeout')); }, 5000);
  socket.addEventListener('open', () => socket.send(JSON.stringify({id: 1, method: 'Runtime.evaluate', params: {
    expression: pointer ? '({title:document.title,status:document.getElementById("pointer-status").textContent})' : `({title:document.title,checked:document.getElementById("${direct ? 'direct' : 'diagnostic'}-check").checked,status:document.getElementById("${direct ? 'direct' : 'diagnostic'}-status").textContent})`,
    returnByValue: true,
  }})));
  socket.addEventListener('message', event => {
    const message = JSON.parse(event.data);
    if (message.id === 1 && stage === 'read') {
      if (message.error || message.result?.exceptionDetails) { clearTimeout(timer); socket.close(); fail(new Error('DOM readback failed')); return; }
      state = message.result.result.value;
      if (!screenshot) { clearTimeout(timer); socket.close(); done(state); return; }
      stage = 'image';
      socket.send(JSON.stringify({id: 2, method: 'Page.captureScreenshot', params: {format: 'png'}}));
    } else if (message.id === 2 && stage === 'image') {
      clearTimeout(timer);
      socket.close();
      if (message.error || !message.result?.data) { fail(new Error('diagnostic screenshot failed')); return; }
      writeFileSync(resolve(screenshot), Buffer.from(message.result.data, 'base64'), {mode: 0o600});
      done(state);
    }
  });
  socket.addEventListener('error', () => { clearTimeout(timer); fail(new Error('diagnostic socket failed')); });
});
console.log(JSON.stringify(result));
