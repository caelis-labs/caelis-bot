import assert from 'node:assert/strict';
import { after, test } from 'node:test';
import { createServer } from 'vite';
import React from 'react';
import { renderToStaticMarkup } from 'react-dom/server';

const globalKeys = [
 'IS_REACT_ACT_ENVIRONMENT',
 'window',
 'self',
 'document',
 'Node',
 'Element',
 'HTMLElement',
 'HTMLDialogElement',
 'HTMLButtonElement',
 'HTMLInputElement',
 'Text',
 'Comment',
 'Event',
 'CustomEvent',
 'MouseEvent',
 'PointerEvent',
 'KeyboardEvent',
 'MutationObserver',
 'ResizeObserver',
 'requestAnimationFrame',
 'cancelAnimationFrame',
];

const savedGlobals = new Map(
 globalKeys.map(key => [key, Object.getOwnPropertyDescriptor(globalThis, key)]),
);

function restoreGlobals() {
 for (const key of globalKeys) {
  const prev = savedGlobals.get(key);
  if (prev) Object.defineProperty(globalThis, key, prev);
  else delete globalThis[key];
 }
}

class FakeEvent {
 constructor(type = '', init = {}) {
  this.type = type;
  this.bubbles = !!init.bubbles;
  this.cancelable = !!init.cancelable;
  this.defaultPrevented = false;
  this.cancelBubble = false;
  this._stopImmediate = false;
  this.target = null;
  this.currentTarget = null;
 }
 initEvent(type, bubbles = false, cancelable = false) {
  this.type = type;
  this.bubbles = !!bubbles;
  this.cancelable = !!cancelable;
 }
 preventDefault() {
  if (this.cancelable) this.defaultPrevented = true;
 }
 stopPropagation() {
  this.cancelBubble = true;
 }
 stopImmediatePropagation() {
  this.cancelBubble = true;
  this._stopImmediate = true;
 }
}

class FakeCustomEvent extends FakeEvent {
 constructor(type, init = {}) {
  super(type, init);
  this.detail = init.detail;
 }
}

class FakeNode {
 constructor(nodeType, nodeName, ownerDocument = null) {
  this.nodeType = nodeType;
  this.nodeName = nodeName;
  this.ownerDocument = ownerDocument;
  this.parentNode = null;
  this.childNodes = [];
  this._listeners = new Map();
 }
 get parentElement() {
  return this.parentNode && this.parentNode.nodeType === 1 ? this.parentNode : null;
 }
 get firstChild() {
  return this.childNodes[0] ?? null;
 }
 get nextSibling() {
  if (!this.parentNode) return null;
  const idx = this.parentNode.childNodes.indexOf(this);
  return idx >= 0 && idx + 1 < this.parentNode.childNodes.length ? this.parentNode.childNodes[idx + 1] : null;
 }
 get isConnected() {
  let cur = this;
  while (cur) {
   if (cur === this.ownerDocument || cur.nodeType === 9) return true;
   cur = cur.parentNode;
  }
  return false;
 }
 contains(other) {
  let cur = other;
  while (cur) {
   if (cur === this) return true;
   cur = cur.parentNode;
  }
  return false;
 }
 appendChild(child) {
  return this.insertBefore(child, null);
 }
 insertBefore(child, ref) {
  if (child.nodeType === 11) {
   const nodes = [...child.childNodes];
   for (const node of nodes) this.insertBefore(node, ref);
   return child;
  }
  if (child.parentNode) child.parentNode.removeChild(child);
  child.parentNode = this;
  if (ref && this.childNodes.includes(ref)) {
   this.childNodes.splice(this.childNodes.indexOf(ref), 0, child);
  } else {
   this.childNodes.push(child);
  }
  return child;
 }
 removeChild(child) {
  const idx = this.childNodes.indexOf(child);
  if (idx >= 0) {
   this.childNodes.splice(idx, 1);
   child.parentNode = null;
  }
  return child;
 }
 addEventListener(type, fn, options) {
  const capture = typeof options === 'boolean' ? options : !!options?.capture;
  const list = this._listeners.get(type) ?? [];
  if (!list.some(entry => entry.fn === fn && entry.capture === capture)) {
   list.push({ fn, capture });
   this._listeners.set(type, list);
  }
 }
 removeEventListener(type, fn, options) {
  const capture = typeof options === 'boolean' ? options : !!options?.capture;
  const list = this._listeners.get(type);
  if (!list) return;
  this._listeners.set(
   type,
   list.filter(entry => entry.fn !== fn || entry.capture !== capture),
  );
 }
 _invokeListeners(event, capturePhase) {
  const list = [...(this._listeners.get(event.type) ?? [])];
  for (const entry of list) {
   if (capturePhase !== undefined && entry.capture !== capturePhase) continue;
   event.currentTarget = this;
   entry.fn.call(this, event);
   if (event._stopImmediate) break;
  }
 }
 dispatchEvent(event) {
  event.target = this;
  const path = [];
  let cur = this;
  while (cur) {
   path.unshift(cur);
   cur = cur.parentNode ?? (cur === this.ownerDocument ? this.ownerDocument.defaultView : null);
  }
  // Capture phase on ancestors
  for (let i = 0; i < path.length - 1; i++) {
   if (event.cancelBubble) break;
   path[i]._invokeListeners(event, true);
  }
  // Target phase
  if (!event.cancelBubble) {
   this._invokeListeners(event, undefined);
  }
  // Bubble phase on ancestors
  if (event.bubbles) {
   for (let i = path.length - 2; i >= 0; i--) {
    if (event.cancelBubble) break;
    path[i]._invokeListeners(event, false);
   }
  }
  event.currentTarget = null;
  return !event.defaultPrevented;
 }
}

class FakeText extends FakeNode {
 constructor(data, ownerDocument) {
  super(3, '#text', ownerDocument);
  this._data = String(data ?? '');
 }
 get nodeValue() {
  return this._data;
 }
 set nodeValue(val) {
  this._data = String(val ?? '');
 }
 get textContent() {
  return this._data;
 }
 set textContent(val) {
  this._data = String(val ?? '');
 }
}

class FakeComment extends FakeNode {
 constructor(data, ownerDocument) {
  super(8, '#comment', ownerDocument);
  this.data = String(data ?? '');
 }
}

class FakeFragment extends FakeNode {
 constructor(ownerDocument) {
  super(11, '#document-fragment', ownerDocument);
 }
}

function matchesSelector(el, selector) {
 const trimmed = selector.trim();
 if (trimmed === '[data-select-open="true"]') {
  return el.getAttribute('data-select-open') === 'true';
 }
 if (trimmed.startsWith('.')) {
  const cls = trimmed.slice(1);
  return (el.className || '').split(/\s+/).includes(cls);
 }
 const dot = trimmed.indexOf('.');
 if (dot > 0) {
  const tag = trimmed.slice(0, dot).toUpperCase();
  const cls = trimmed.slice(dot + 1);
  return el.tagName === tag && (el.className || '').split(/\s+/).includes(cls);
 }
 return el.tagName === trimmed.toUpperCase();
}

function walkQuery(root, selector, all = false) {
 const out = [];
 const visit = node => {
  for (const child of node.childNodes) {
   if (child.nodeType === 1 && matchesSelector(child, selector)) {
    out.push(child);
    if (!all) return true;
   }
   if (visit(child) && !all) return true;
  }
  return false;
 };
 visit(root);
 return all ? out : out[0] ?? null;
}

class FakeElement extends FakeNode {
 constructor(tagName, ownerDocument) {
  super(1, tagName.toUpperCase(), ownerDocument);
  this.tagName = tagName.toUpperCase();
  this.attributes = new Map();
  this.style = {};
  this.dataset = {};
  this.disabled = false;
  this.focused = false;
 }
 get id() {
  return this.getAttribute('id') ?? '';
 }
 set id(v) {
  this.setAttribute('id', v);
 }
 get className() {
  return this.getAttribute('class') ?? '';
 }
 set className(v) {
  this.setAttribute('class', v);
 }
 set textContent(val) {
  this.childNodes = [];
  if (val !== '' && val !== null && val !== undefined) {
   this.appendChild(new FakeText(val, this.ownerDocument));
  }
 }
 get textContent() {
  return this.childNodes.map(c => c.textContent ?? '').join('');
 }
 setAttribute(name, value) {
  this.attributes.set(name, String(value));
 }
 getAttribute(name) {
  return this.attributes.has(name) ? this.attributes.get(name) : null;
 }
 hasAttribute(name) {
  return this.attributes.has(name);
 }
 removeAttribute(name) {
  this.attributes.delete(name);
 }
 focus() {
  this.focused = true;
  if (this.ownerDocument) this.ownerDocument.activeElement = this;
 }
 closest(selector) {
  let cur = this;
  while (cur && cur.nodeType === 1) {
   if (matchesSelector(cur, selector)) return cur;
   cur = cur.parentNode;
  }
  return null;
 }
 getBoundingClientRect() {
  return { left: 100, top: 100, right: 500, bottom: 400, width: 400, height: 300 };
 }
 querySelector(selector) {
  return walkQuery(this, selector, false);
 }
 querySelectorAll(selector) {
  return walkQuery(this, selector, true);
 }
}

class FakeButtonElement extends FakeElement {}
class FakeInputElement extends FakeElement {}
class FakeIFrameElement extends FakeElement {}

class FakeDialogElement extends FakeElement {
 constructor(ownerDocument) {
  super('dialog', ownerDocument);
  this.showModalCalls = 0;
  this.closeCalls = 0;
  this._pendingTimers = new Set();
 }
 get open() {
  return this.hasAttribute('open');
 }
 set open(value) {
  if (value) this.setAttribute('open', '');
  else this.removeAttribute('open');
 }
 showModal() {
  this.showModalCalls++;
  this.open = true;
 }
 close() {
  if (!this.open) return;
  this.closeCalls++;
  this.open = false;
  // Native HTML <dialog>.close() queues an async task to fire the "close" event.
  const timer = setTimeout(() => {
   this._pendingTimers.delete(timer);
   this.dispatchEvent(new FakeEvent('close', { bubbles: false, cancelable: false }));
  }, 0);
  this._pendingTimers.add(timer);
 }
 cancelTimers() {
  for (const timer of this._pendingTimers) clearTimeout(timer);
  this._pendingTimers.clear();
 }
}

class FakeDocument extends FakeNode {
 constructor() {
  super(9, '#document', null);
  this.ownerDocument = this;
  this.defaultView = null;
  this.documentElement = new FakeElement('html', this);
  this.head = new FakeElement('head', this);
  this.body = new FakeElement('body', this);
  this.documentElement.appendChild(this.head);
  this.documentElement.appendChild(this.body);
  this.appendChild(this.documentElement);
  this.activeElement = this.body;
 }
 createElement(tag) {
  const lower = String(tag).toLowerCase();
  if (lower === 'dialog') return new FakeDialogElement(this);
  if (lower === 'button') return new FakeButtonElement(lower, this);
  if (lower === 'input') return new FakeInputElement(lower, this);
  return new FakeElement(lower, this);
 }
 createElementNS(_ns, tag) {
  return this.createElement(tag);
 }
 createTextNode(text) {
  return new FakeText(text, this);
 }
 createComment(data) {
  return new FakeComment(data, this);
 }
 createDocumentFragment() {
  return new FakeFragment(this);
 }
 createEvent() {
  return new FakeEvent();
 }
 querySelector(selector) {
  return walkQuery(this.documentElement, selector, false);
 }
 querySelectorAll(selector) {
  return walkQuery(this.documentElement, selector, true);
 }
}

function installDom() {
 const doc = new FakeDocument();
 const listeners = new Map();
 const win = {
  innerWidth: 1280,
  innerHeight: 800,
  document: doc,
  addEventListener(type, fn, options) {
   const capture = typeof options === 'boolean' ? options : !!options?.capture;
   const list = listeners.get(type) ?? [];
   if (!list.some(entry => entry.fn === fn && entry.capture === capture)) {
    list.push({ fn, capture });
    listeners.set(type, list);
   }
  },
  removeEventListener(type, fn, options) {
   const capture = typeof options === 'boolean' ? options : !!options?.capture;
   const list = listeners.get(type);
   if (!list) return;
   listeners.set(type, list.filter(entry => entry.fn !== fn || entry.capture !== capture));
  },
  _invokeListeners(event, capturePhase) {
   const list = [...(listeners.get(event.type) ?? [])];
   for (const entry of list) {
    if (capturePhase !== undefined && entry.capture !== capturePhase) continue;
    event.currentTarget = win;
    entry.fn.call(win, event);
    if (event._stopImmediate) break;
   }
  },
  dispatchEvent(event) {
   event.target = win;
   this._invokeListeners(event, undefined);
   return !event.defaultPrevented;
  },
 };
 doc.defaultView = win;

 let rafSeq = 0;
 globalThis.IS_REACT_ACT_ENVIRONMENT = true;
 globalThis.window = win;
 globalThis.self = win;
 globalThis.document = doc;
 globalThis.Node = FakeNode;
 globalThis.Element = FakeElement;
 globalThis.HTMLElement = FakeElement;
 globalThis.HTMLDialogElement = FakeDialogElement;
 globalThis.HTMLButtonElement = FakeButtonElement;
 globalThis.HTMLInputElement = FakeInputElement;
 globalThis.HTMLIFrameElement = FakeIFrameElement;
 globalThis.Text = FakeText;
 globalThis.Comment = FakeComment;
 globalThis.Event = FakeEvent;
 globalThis.CustomEvent = FakeCustomEvent;
 globalThis.MouseEvent = FakeEvent;
 globalThis.PointerEvent = FakeEvent;
 globalThis.KeyboardEvent = FakeEvent;
 globalThis.MutationObserver = class { observe() {} disconnect() {} };
 globalThis.ResizeObserver = class { observe() {} disconnect() {} };
 globalThis.requestAnimationFrame = fn => {
  const id = ++rafSeq;
  setTimeout(() => fn(performance.now()), 0);
  return id;
 };
 globalThis.cancelAnimationFrame = id => clearTimeout(id);
 Object.assign(win, {
  Node: FakeNode,
  Element: FakeElement,
  HTMLElement: FakeElement,
  HTMLDialogElement: FakeDialogElement,
  HTMLButtonElement: FakeButtonElement,
  HTMLInputElement: FakeInputElement,
  HTMLIFrameElement: FakeIFrameElement,
  Text: FakeText,
  Comment: FakeComment,
  Event: FakeEvent,
  CustomEvent: FakeCustomEvent,
  MouseEvent: FakeEvent,
  PointerEvent: FakeEvent,
  KeyboardEvent: FakeEvent,
 });
 return doc;
}

const flushMacrotasks = () => new Promise(resolve => setTimeout(resolve, 25));

const server = await createServer({ server: { middlewareMode: true, hmr: false }, appType: 'custom' });
const doc = installDom();
const { createRoot } = await import('react-dom/client');
const { SettingsDialog } = await server.ssrLoadModule('/src/settings/runtime/SettingsDialog.tsx');
const { UpdateSettingsShell } = await server.ssrLoadModule('/src/UpdateSettingsShell.tsx');
const { PermissionSettings } = await server.ssrLoadModule('/src/PermissionSettings.tsx');

after(async () => {
 for (const dlg of doc.querySelectorAll('dialog')) dlg.cancelTimers?.();
 await server.close();
 restoreGlobals();
});

function mountStrictDialog({ busy = false, initialOpen = true } = {}) {
 const host = doc.createElement('div');
 doc.body.appendChild(host);
 const root = createRoot(host);
 let onCloseCalls = 0;
 let setOpenExternal;

 function Harness() {
  const [open, setOpen] = React.useState(initialOpen);
  setOpenExternal = setOpen;
  if (!open) return null;
  return React.createElement(
   SettingsDialog,
   {
    title: 'StrictMode Test Dialog',
    busy,
    portalTarget: host,
    onClose: () => {
     onCloseCalls++;
     setOpen(false);
    },
   },
   React.createElement('p', null, 'Dialog content'),
  );
 }

 React.act(() => {
  root.render(React.createElement(React.StrictMode, null, React.createElement(Harness)));
 });

 const getDialog = () => host.querySelector('dialog');
 const unmount = () => {
  for (const dlg of host.querySelectorAll('dialog')) dlg.cancelTimers?.();
  React.act(() => {
   root.unmount();
  });
  if (host.parentNode) host.parentNode.removeChild(host);
 };

 return {
  host,
  getDialog,
  get onCloseCalls() {
   return onCloseCalls;
  },
  setOpen: v => React.act(() => { setOpenExternal(v); }),
  unmount,
 };
}

test('SettingsDialog ignores stale native close events when reopened under React StrictMode', async t => {
 const mounted = mountStrictDialog();
 t.after(() => mounted.unmount());

 const dialog = mounted.getDialog();
 assert.ok(dialog, 'dialog should be mounted');
 // StrictMode mounts -> showModal (1), unmounts -> close (1), remounts -> showModal (2).
 assert.equal(dialog.showModalCalls, 2);
 assert.equal(dialog.closeCalls, 1);
 assert.equal(dialog.open, true);

 // Wait for the queued native close event from StrictMode cleanup to arrive.
 await React.act(async () => {
  await flushMacrotasks();
 });

 assert.equal(mounted.onCloseCalls, 0, 'stale cleanup close event must not trigger onClose');
 assert.ok(mounted.getDialog(), 'dialog must remain mounted after delayed StrictMode close');
 assert.equal(mounted.getDialog().open, true, 'dialog must remain open');
});

test('SettingsDialog calls onClose exactly once when closed via native close, close button, or Escape under StrictMode', async t => {
 // 1. Native close (e.g. closedby="any" light dismiss)
 {
  const mounted = mountStrictDialog();
  t.after(() => mounted.unmount());
  const dialog = mounted.getDialog();
  assert.ok(dialog);
  await React.act(async () => { await flushMacrotasks(); });
  assert.equal(mounted.onCloseCalls, 0);

  await React.act(async () => {
   dialog.close();
   await flushMacrotasks();
  });
  assert.equal(mounted.onCloseCalls, 1);
  assert.equal(mounted.getDialog(), null);
 }

 // 2. Close button click (which calls onClose, unmounting dialog and running effect cleanup dialog.close())
 {
  const mounted = mountStrictDialog();
  t.after(() => mounted.unmount());
  await React.act(async () => { await flushMacrotasks(); });
  const closeBtn = mounted.host.querySelector('button.runtime-close');
  assert.ok(closeBtn, 'close button should exist');

  await React.act(async () => {
   closeBtn.dispatchEvent(new FakeEvent('click', { bubbles: true, cancelable: true }));
   await flushMacrotasks();
  });
  assert.equal(mounted.onCloseCalls, 1);
  assert.equal(mounted.getDialog(), null);
 }

 // 3. Escape key / native cancel event
 {
  const mounted = mountStrictDialog();
  t.after(() => mounted.unmount());
  await React.act(async () => { await flushMacrotasks(); });
  const dialog = mounted.getDialog();
  assert.ok(dialog);

  await React.act(async () => {
   const cancelEvent = new FakeEvent('cancel', { bubbles: false, cancelable: true });
   dialog.dispatchEvent(cancelEvent);
   assert.equal(cancelEvent.defaultPrevented, true);
   await flushMacrotasks();
  });
  assert.equal(mounted.onCloseCalls, 1);
  assert.equal(mounted.getDialog(), null);
 }
});

test('SettingsDialog reopens instead of closing when busy', async t => {
 const mounted = mountStrictDialog({ busy: true });
 t.after(() => mounted.unmount());
 await React.act(async () => { await flushMacrotasks(); });

 const dialog = mounted.getDialog();
 assert.ok(dialog);
 const prevShowModalCalls = dialog.showModalCalls;

 await React.act(async () => {
  dialog.close();
  await flushMacrotasks();
 });

 assert.equal(mounted.onCloseCalls, 0);
 assert.equal(dialog.open, true);
 assert.equal(dialog.showModalCalls, prevShowModalCalls + 1);
});

test('UpdateSettingsShell omits duplicate identity header and renders automatic updates switch', () => {
 const base = renderToStaticMarkup(
  React.createElement(UpdateSettingsShell, {
   status: 'Up to date',
   actions: React.createElement('button', null, 'Check for updates'),
  }),
 );
 assert.doesNotMatch(base, /about-identity/);
 assert.doesNotMatch(base, /role="switch"/);

 const withToggle = renderToStaticMarkup(
  React.createElement(UpdateSettingsShell, {
   badge: 'QA build',
   status: 'Up to date',
   actions: React.createElement('button', null, 'Check for updates'),
   automatic: {
    id: 'automatic-updates',
    label: 'Check for updates automatically',
    description: 'Checks release versions daily.',
    checked: true,
    disabled: false,
    onChange: () => {},
   },
  }),
 );
 assert.doesNotMatch(withToggle, /about-identity/);
 assert.match(withToggle, /QA build/);
 assert.match(withToggle, /id="automatic-updates"/);
 assert.match(withToggle, /role="switch"/);
 assert.match(withToggle, /checked=""/);

 const disabledToggle = renderToStaticMarkup(
  React.createElement(UpdateSettingsShell, {
   status: 'Check for updates',
   actions: React.createElement('button', null, 'Check for updates'),
   automatic: {
    id: 'automatic-updates',
    label: 'Check for updates automatically',
    description: 'In-app updates are not enabled for this build.',
    checked: false,
    disabled: true,
    onChange: () => {},
   },
  }),
 );
 assert.match(disabledToggle, /id="automatic-updates"/);
 assert.match(disabledToggle, /disabled=""/);
});

test('PermissionSettings renders footer repair trigger, icon manage action without redundant hint, and clean repair modal', async t => {
 const host = doc.createElement('div');
 doc.body.appendChild(host);
 const root = createRoot(host);
 t.after(() => {
  for (const dlg of doc.querySelectorAll('dialog')) dlg.cancelTimers?.();
  React.act(() => { root.unmount(); });
  if (host.parentNode) host.parentNode.removeChild(host);
 });

 const calls = [];
 const mockCall = async (method, ...args) => {
  calls.push([method, ...args]);
  if (method === 'SystemPermissions') {
   return {
    supported: true,
    appPath: '/Applications/Caelis Bot.app',
    development: false,
    permissions: [
     { id: 'accessibility', status: 'authorized' },
     { id: 'screenCapture', status: 'permissionRequired' },
     { id: 'automation', status: 'notRunning', target: 'Terminal' },
     { id: 'notifications', status: 'authorized' },
    ],
   };
  }
  return undefined;
 };

 await React.act(async () => {
  root.render(React.createElement(React.StrictMode, null, React.createElement(PermissionSettings, { embedded: true, call: mockCall })));
 });

 const authorizedBtn = host.querySelector('#permission-accessibility') ?? host.querySelectorAll('button.permission-icon-action')[0];
 assert.ok(authorizedBtn, 'authorized permission row should use icon action button');
 assert.equal(authorizedBtn.textContent.trim(), '', 'icon action button should not render Manage text');

 // Clicking manage opens system settings silently without rendering redundant turn-off feedback text.
 await React.act(async () => {
  authorizedBtn.dispatchEvent(new FakeEvent('click', { bubbles: true, cancelable: true }));
  await flushMacrotasks();
 });
 assert.ok(calls.some(([method, id]) => method === 'OpenSystemPermissionSettings' && id === 'accessibility'));
 assert.equal(host.querySelector('.permission-feedback'), null, 'authorized row should not show turn-off hint');

 const footer = host.querySelector('.permission-footer');
 const trigger = footer?.querySelector('button.permission-repair-trigger');
 assert.ok(trigger, 'repair trigger button should be rendered in permission footer');
 assert.equal(host.querySelector('.permission-privacy'), null, 'main permission page should remain uncluttered');
 assert.equal(doc.querySelector('dialog'), null, 'repair dialog should be closed initially');

 await React.act(async () => {
  trigger.dispatchEvent(new FakeEvent('click', { bubbles: true, cancelable: true }));
  await flushMacrotasks();
 });

 const dialog = doc.querySelector('dialog');
 assert.ok(dialog, 'repair dialog should open');
 assert.equal(dialog.open, true, 'repair dialog must stay open under StrictMode');
 const text = dialog.textContent;
 assert.match(text, /\/Applications\/Caelis Bot\.app/);
 assert.doesNotMatch(text, /Send test notification/);
 assert.equal(dialog.querySelectorAll('p.settings-note').length, 0);
 assert.equal(dialog.querySelector('.settings-select-centered'), null, 'repair category select should align left');
 assert.ok(dialog.querySelector('button.permission-reveal-link'), 'reveal link button should exist');
 assert.ok(dialog.querySelector('.permission-repair-footer')?.querySelector('button.permission-reset-submit'), 'reset action button should exist');
});
