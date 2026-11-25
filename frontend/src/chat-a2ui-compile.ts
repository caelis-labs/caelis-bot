import {ExpressParser} from '@a2ui/agent';
import {getComponentReferences, STRICT_VALIDATION} from '@a2ui/web_core';
import type {CreateSurfaceMessage} from '@a2ui/web_core/v1_0';
import {chatCatalog} from './chat-a2ui-catalog.ts';

const chatSurfaceId = 'chat';
const parser = new ExpressParser(chatCatalog, chatSurfaceId, 'v1.0');
const maxItems = 12;
const maxNodes = 160;
export const chatValidation = { ...STRICT_VALIDATION, targetVersion: 'v1.0' } as const;
const forbidden = new Set([...Object.getOwnPropertyNames(Object.prototype), 'prototype', 'Event', 'surface', 'deleteSurface']);

function acceptsSource(body: string) {
 if (body.length > 8192) return false;
 // Screen paths and keys before the upstream compiler materializes data. The
 // official Express compiler still owns syntax and schema interpretation.
 let valid = true;
 const shape = body.replace(/"(?:\\[^\r\n]|[^"\\\r\n])*"([ \t\r\n]*:)?/g, (token, key: string | undefined) => {
  try {
   const literal = JSON.parse(key ? token.slice(0, -key.length) : token);
   if (key && forbidden.has(literal)) valid = false;
  }
  catch { valid = false; }
  return ' ';
 });
 if (!valid || !/^[\w\s=,()[\]{}:/$.?@-]*$/u.test(shape)) return false;
 if ((shape.match(/[a-z_]\w*/gi) ?? []).some(word => forbidden.has(word))) return false;
 for (const path of shape.match(/\$\/?[\w./~-]+/g) ?? []) {
  if (path.split('/').some(segment => /^\d+$/.test(segment) && Number(segment) >= maxItems)) return false;
 }
 let depth = 0;
 for (const char of shape) {
  if ('([{'.includes(char) && ++depth > 12) return false;
  if (')]}'.includes(char)) depth--;
 }
 return true;
}

function boundedData(value: unknown, depth = 0): boolean {
 if (depth > 8) return false;
 if (Array.isArray(value)) return value.length <= maxItems && value.every(item => boundedData(item, depth + 1));
 if (value && typeof value === 'object') {
  const entries = Object.entries(value);
  return entries.length <= 40 && entries.every(([key, item]) => !forbidden.has(key) && boundedData(item, depth + 1));
 }
 return true;
}

// Use the Catalog's reference fields. Each template can expand to the
// maximum array length, including after a local edit. Reject bad graphs
// before the SDK constructs any reactive nodes.
function withinRenderBudget(components: NonNullable<CreateSurfaceMessage['createSurface']['components']>) {
 const byID = new Map(components.map(component => [component.id, component]));
 if (byID.size !== components.length) return false;
 const active = new Set<string>();
 const costs = new Map<string, number>();
 function cost(id: string): number {
  const known = costs.get(id);
  if (known !== undefined) return known;
  const component = byID.get(id);
  if (!component || active.has(id)) return Infinity;
  active.add(id);
  let nodes = 1;
  for (const [child, path] of getComponentReferences(component, chatCatalog.componentRefMap)) {
   nodes += cost(child) * (path.endsWith('.componentId') ? maxItems : 1);
   if (nodes > maxNodes) break;
  }
  active.delete(id);
  costs.set(id, nodes);
  return nodes;
 }
 return cost('root') <= maxNodes;
}

export function compileChatSurface(body: string) {
 if (!acceptsSource(body)) return;
 const [create, ...extra] = parser.compile(body);
 if (extra.length || !create || create.version !== '1.0' || !('createSurface' in create)) return;
 const opening = create.createSurface;
 if (opening.catalogId !== chatCatalog.id || opening.surfaceId !== chatSurfaceId || !opening.components?.length || opening.components.length > 40) return;
 if (Object.keys(opening).some(key => !['catalogId', 'surfaceId', 'components', 'dataModel'].includes(key))) return;
 if (!boundedData(opening.dataModel) || opening.components.some(component => !boundedData(component))) return;
 if (!withinRenderBudget(opening.components)) return;
 return create;
}
