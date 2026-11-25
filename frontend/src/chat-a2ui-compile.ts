import {ExpressParser} from '@a2ui/agent';
import {chatCatalog} from './chat-a2ui-catalog.ts';

const chatSurfaceId = 'chat';
const parser = new ExpressParser(chatCatalog, chatSurfaceId, 'v1.0');
const forbidden = new Set([...Object.getOwnPropertyNames(Object.prototype), 'prototype', 'Event', 'surface', 'deleteSurface']);

function acceptsSource(body: string) {
 if (body.length > 8192) return false;
 // Screen paths and keys before the upstream compiler materializes data. The
 // official Express compiler still owns syntax and schema interpretation.
 let valid = true;
 const shape = body.replace(/"(?:\\[^\r\n]|[^"\\\r\n])*"/g, literal => {
  try { if (forbidden.has(JSON.parse(literal))) valid = false; }
  catch { valid = false; }
  return ' ';
 });
 if (!valid || !/^[\w\s=,()[\]{}:/$.?@-]*$/u.test(shape)) return false;
 if ((shape.match(/[a-z_]\w*/gi) ?? []).some(word => forbidden.has(word))) return false;
 if (/\/\d{2,}/.test(shape)) return false;
 let depth = 0;
 for (const char of shape) {
  if ('([{'.includes(char) && ++depth > 12) return false;
  if (')]}'.includes(char)) depth--;
 }
 return true;
}

function boundedData(value: unknown, depth = 0): boolean {
 if (depth > 8) return false;
 if (Array.isArray(value)) return value.length <= 12 && value.every(item => boundedData(item, depth + 1));
 if (value && typeof value === 'object') {
  const entries = Object.entries(value);
  return entries.length <= 40 && entries.every(([key, item]) => !forbidden.has(key) && boundedData(item, depth + 1));
 }
 return true;
}

export function compileChatSurface(body: string) {
 if (!acceptsSource(body)) return;
 const [create, ...extra] = parser.compile(body);
 if (extra.length || !create || create.version !== '1.0' || !('createSurface' in create)) return;
 const opening = create.createSurface;
 if (opening.catalogId !== chatCatalog.id || opening.surfaceId !== chatSurfaceId || !opening.components?.length || opening.components.length > 40) return;
 if (Object.keys(opening).some(key => !['catalogId', 'surfaceId', 'components', 'dataModel'].includes(key))) return;
 if (!boundedData(opening.dataModel) || opening.components.some(component => !boundedData(component))) return;
 return create;
}
