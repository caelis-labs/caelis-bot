import {randomUUID} from 'node:crypto';
import {
  CuaDriver, ListAppsInput, ListWindowsInput, GetWindowStateInput,
  ActionTarget, ClickPosition, ClickInput, ClickButton, InputDeliveryMode,
} from '@trycua/cua-driver';

// Product-owned experimental vocabulary. Native tokens and window identities
// stay inside this object; model-facing targets belong to one observation only.
export class SemanticDesktop {
  #driver;
  #target;
  #current;
  #busy = false;
  constructor(driver = CuaDriver.create()) { this.#driver = driver; }
  async bindFixture() {
    const {apps} = await this.#driver.listApps(ListAppsInput.new({}));
    const matches = apps.filter(a => a.running && a.bundleId === 'dev.caelis.desktop-control-fixture');
    if (matches.length !== 1) throw new Error('Expected exactly one disposable fixture app');
    const pid = matches[0].pid;
    const {windows} = await this.#driver.listWindows(ListWindowsInput.new({pid, onScreenOnly: true}));
    const selected = windows.filter(w => w.title === 'Caelis Desktop Control Fixture');
    if (selected.length !== 1) throw new Error('Expected exactly one fixture window');
    this.#target = {pid, windowId: selected[0].windowId};
    this.#current = undefined;
  }
  async observe() {
    if (this.#busy) throw new Error('desktop_busy');
    this.#busy = true;
    try { return await this.#observe(); } finally { this.#busy = false; }
  }
  async #observe() {
    if (!this.#target) throw new Error('target_unavailable');
    // Failure invalidates old references too. No screenshot and no OCR fallback.
    this.#current = undefined;
    const state = await this.#driver.getWindowState(GetWindowStateInput.new({
      ...this.#target, includeAccessibilityTree: true, includeScreenshot: false,
      maxElements: 100, maxDepth: 12, timeoutMs: 1500,
    }), {signal: AbortSignal.timeout(5000)});
    if (state.images.length) throw new Error('unexpected_screenshot');
    if (state.degraded || state.truncated || !state.snapshotId) throw new Error('incomplete_observation');
    const observation = `obs-${randomUUID()}`;
    const elements = new Map();
    const projected=state.elements??[];
    const root=projected.find(e=>e.role==='AXWindow'&&e.label==='Caelis Desktop Control Fixture');
    if(!root?.frame)throw new Error('window_geometry_unavailable');
    const allowedParents=new Set([String(root.elementIndex)]);
    const windowElements=[];
    for(const e of projected) {
      if(e===root||allowedParents.has(String(e.parentIndex))) {
        windowElements.push(e);allowedParents.add(String(e.elementIndex));
      }
    }
    const treeLines=(state.treeMarkdown??'').split('\n');
    const start=treeLines.findIndex(line=>line.startsWith('- ')&&line.includes('AXWindow'));
    const end=treeLines.findIndex((line,i)=>i>start&&line.startsWith('- '));
    const text=treeLines.slice(start,end<0?undefined:end).join('\n').slice(0,16000);
    const targets = windowElements.map((e,i) => {
      const target = `target-${i+1}`;
      // This first live probe is intentionally scoped to the fixture filter.
      if (e.role==='AXCheckBox'&&e.label==='Only incomplete'&&e.elementToken && e.enabled !== false && e.actions?.includes('AXPress')) elements.set(target,e.elementToken);
      return {target, role:e.role, name:e.label, value:e.value, enabled:e.enabled,
        bounds:e.frame, actions:elements.has(target)?['click']:[]};
    });
    const result = {observation, source:'accessibility', screenshot:false,
      windowBounds:root.frame, elementsComplete:state.elementsComplete,text,
      geometry:'Cua native window/element coordinates; do not treat as Bot desktop points before adapter calibration', targets};
    this.#current = {observation,elements};
    return result;
  }
  async perform(input) {
    if(this.#busy) throw new Error('desktop_busy');
    if (!input || input.observation !== this.#current?.observation) throw new Error('stale_observation');
    if (!Array.isArray(input.steps) || input.steps.length<1 || input.steps.length>8) throw new Error('invalid_steps');
    // Validate the complete batch before delivery. The first external mutation
    // then checkpoints; remaining steps must be replanned against fresh state.
    const tokens=input.steps.map(step=>{
      const token=this.#current.elements.get(step.target);
      if(step.op!=='click'||!token)throw new Error('unsupported_target_action');
      return token;
    });
    this.#busy=true;
    this.#current=undefined;
    try {
      await this.#driver.click(ClickInput.new({target:new ActionTarget.Window(this.#target),
        position:new ClickPosition.Element({elementToken:tokens[0]}),
        deliveryMode:InputDeliveryMode.Background,button:ClickButton.Left,count:1}),
        {signal:AbortSignal.timeout(5000)});
      const observation=await this.#observe();
      return {steps:[{index:0,status:'dispatched'}],remaining:input.steps.slice(1),observation};
    } catch(error) {
      // A delivery followed by a failed observation is not safe to resend.
      this.#current=undefined;
      error.mayHaveActed=true;
      throw error;
    } finally {this.#busy=false;}
  }
  async close() {await this.#driver.shutdown();this.#driver.uniffiDestroy?.();}
}
