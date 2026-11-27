import type {DataModel} from '@a2ui/web_core';
import {fieldText} from './chat-a2ui-functions.ts';

const placeholder = /\{([\p{L}_][\p{L}\p{N}_]{0,39})\}/gu;
const fieldName = /^[\p{L}_][\p{L}\p{N}_]{0,39}$/u;
const statePath = /^\/(?:[\w-]+\/){0,7}[\w-]+$/;
const maxFields = 12;
const forbidden = new Set([...Object.getOwnPropertyNames(Object.prototype), 'prototype']);
const writablePath = (path: string) => statePath.test(path) && !path.split('/').some(key => forbidden.has(key));

export function chatEvent(action: unknown) {
 const event = (action as {event?: unknown} | undefined)?.event;
 if (!event || typeof event !== 'object') return;
 const {name, context = {}, userMessage} = event as {
  name?: unknown; context?: Record<string, unknown>; userMessage?: unknown;
 };
 if (!context || typeof context !== 'object' || Array.isArray(context)) return;
 const fields = Object.keys(context);
 if (fields.length > maxFields) return;
 if (name === 'set' && userMessage === undefined && fields.length > 0 &&
  fields.every(writablePath)) return 'set';
 if (name !== 'submit' || typeof userMessage !== 'string' || !userMessage.trim() || userMessage.length > 4096) return;
 const slots = [...userMessage.matchAll(placeholder)].map(match => match[1]);
 if (/[{}]/.test(userMessage.replace(placeholder, '')) ||
  !fields.every(key => fieldName.test(key) && slots.includes(key)) || !slots.every(key => Object.hasOwn(context, key))) return;
 return 'submit';
}

// All values were evaluated by A2UI against the same old state. Publish the
// whole update once so derived values cannot observe a partial transition.
export function applyChatState(model: DataModel, changes: Record<string, unknown>) {
 const next = structuredClone(model.get<Record<string, unknown>>('/'));
 for (const [path, value] of Object.entries(changes)) {
  if (!writablePath(path)) return false;
  const segments = path.slice(1).split('/');
  const key = segments.pop()!;
  let parent: unknown = next;
  for (const segment of segments) {
   if (!parent || typeof parent !== 'object' || !Object.hasOwn(parent, segment)) return false;
   parent = (parent as Record<string, unknown>)[segment];
  }
  if (!parent || typeof parent !== 'object' || !Object.hasOwn(parent, key) ||
   Array.isArray(parent) && !/^\d+$/.test(key)) return false;
  const record = parent as Record<string, unknown>;
  const previous = record[key];
  if (previous && typeof previous === 'object' && !Array.isArray(previous)) return false;
  if (value !== null && fieldText(value) === undefined) return false;
  record[key] = value;
 }
 model.set('/', next, true);
 return true;
}

export function fillChatTemplate(template: string, values: Record<string, unknown>) {
 const display = new Map<string, string>();
 for (const [, name] of template.matchAll(placeholder)) {
  if (display.has(name)) continue;
  const value = fieldText(Object.hasOwn(values, name) ? values[name] : undefined);
  if (!value) return;
  display.set(name, value);
 }
 return template.replace(placeholder, (_, name: string) => display.get(name) ?? '').trim() || undefined;
}
