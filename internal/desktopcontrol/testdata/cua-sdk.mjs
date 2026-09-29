// Only the native SDK is replaced. Tests run the shipped host and Desktop facade.
import {readFileSync, writeFileSync} from 'node:fs';
const config = JSON.parse(readFileSync(new URL('../../../fixture.json', import.meta.url)));
const root = new URL('../../../', import.meta.url);
const factory = {new: value => value};
export const ListWindowsInput = factory, GetWindowStateInput = factory, ClickInput = factory;
class Record {constructor(value) {Object.assign(this, value);}}
export const ActionTarget = {Window: Record}, ClickPosition = {Element: Record, CapturedCoordinates: Record};
export const InputDeliveryMode = {Foreground: 1}, ClickButton = {Left: 0}, ActionEffect = {Refused: 4};
export class CuaDriver {
  static create() {
    let lists = 0;
    const window = {pid: 17, windowId: 42n, appName: config.windowText ?? 'Fixture', title: config.windowText ?? 'Fixture',
      bounds: {x: 10, y: 20, width: 600, height: 400}};
    const windows = Array.from({length: config.windows ?? 1}, (_, i) => ({...window, windowId: 42n + BigInt(i),
      title: config.windows ? `Window ${i + 1} ${window.title}` : window.title}));
    const input = async () => {writeFileSync(new URL('input-dispatched', root), 'yes'); return {isError: false};};
    const state = ({pid, windowId}) => ({pid, windowId, snapshotId: 'snapshot', elementsComplete: true,
      screenshotFrameValid: true, captureId: 'capture', screenshotWidth: config.imageWidth ?? 1000, screenshotHeight: config.imageHeight ?? 600,
      treeMarkdown: `- [1] AXWindow ${windows.find(w => w.windowId === windowId).title}\n  - selected ${windowId}\n  - ` + (config.text ?? 'Fixture text'),
      elements: [
        {elementIndex: 1n, role: 'AXWindow', label: windows.find(w => w.windowId === windowId).title},
        {elementIndex: 2n, parentIndex: 1n, role: 'AXCheckBox', label: 'Filter', value: '0', elementToken: 'check', actions: ['AXPress']},
        {elementIndex: 3n, parentIndex: 1n, role: 'AXTextField', label: 'Text', elementToken: 'text'},
      ], images: config.images ?? []});
    const snake = value => Object.fromEntries(Object.entries(value).map(([k,v])=>[k.replace(/[A-Z]/g,c=>'_'+c.toLowerCase()),v]));
    return {
      listWindows: async () => {
        if (++lists === 4 && config.blockPreflight) {
          writeFileSync(new URL('preflight-started', root), 'yes');
          // EOF is precisely the old grace-period shutdown signal. It does not
          // cancel an in-flight SDK promise, which can then dispatch an input.
          await new Promise(resolve => process.stdin.once('end', resolve));
        }
        return {windows};
      },
      getWindowState: async args => state(args),
      click: input,
      callTool: async (name, json) => {
        if(name !== 'get_window_state') return input();
        const args=JSON.parse(json), value=state({pid:args.pid,windowId:BigInt(args.window_id)});
        return {isError:false,images:value.images,structuredJson:JSON.stringify({...snake(value),elements:value.elements.map(snake)},(_,v)=>typeof v==='bigint'?String(v):v)};
      },
      shutdown: async () => {writeFileSync(new URL('shutdown-completed', root), 'yes');},
    };
  }
}
