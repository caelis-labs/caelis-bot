// Independent DOM readback from only the disposable page in a private Chrome profile.
import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {pathToFileURL} from 'node:url';

const [profile, run] = process.argv.slice(2);
if (!profile || !/^[a-z0-9-]+$/.test(run ?? '')) throw Error('private profile and unique run required');
const port = Number(readFileSync(resolve(profile, 'DevToolsActivePort'), 'utf8').split('\n')[0]);
const expected = pathToFileURL(resolve('experiments/desktop-control/browser-rc3-checkbox.html')).href + `?run=${run}`;
const pages = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json();
const page = pages.find(item => item.type === 'page' && item.url === expected);
if (!page || pages.filter(item => item.type === 'page').length !== 1) throw Error('private profile has an unexpected page');
const socket = new WebSocket(page.webSocketDebuggerUrl);
const result = await new Promise((done, fail) => {
  const timer = setTimeout(() => { socket.close(); fail(Error('DOM readback timeout')); }, 5000);
  socket.addEventListener('open', () => socket.send(JSON.stringify({id: 1, method: 'Runtime.evaluate',
    params: {expression: 'window.botRC3Evidence()', returnByValue: true}})));
  socket.addEventListener('message', event => {
    const response = JSON.parse(event.data);
    if (response.id !== 1) return;
    clearTimeout(timer);
    socket.close();
    if (response.error || response.result?.exceptionDetails) fail(Error('DOM readback failed'));
    else done(response.result.result.value);
  });
  socket.addEventListener('error', () => { clearTimeout(timer); fail(Error('DOM readback socket failed')); });
});
if (result.title !== `Bot RC3 Chrome Checkbox Fixture ${run}`) throw Error('wrong synthetic page');
console.log(JSON.stringify(result));
