import {Fragment, useEffect, useState, type ReactNode} from 'react';
import {ExpressParser} from '@a2ui/agent';
import {A2uiSurface, createComponentImplementation, type ReactCatalogComponent} from '@a2ui/react';
import {Catalog, MessageProcessor, type SurfaceModel} from '@a2ui/web_core';
import {ComponentIdSchema, markChildRef} from '@a2ui/web_core/v1_0';
import {z} from 'zod';
import './chat-a2ui.css';

const children = markChildRef(z.array(ComponentIdSchema).min(1).max(12), 'child-list');
const catalog = new Catalog('https://caelis.dev/catalogs/chat-read-only', 'v1.0', [
 createComponentImplementation({name: 'Text', schema: z.object({text: z.string().min(1).max(1000), variant: z.enum(['title', 'body', 'caption']).optional()}).strict()},
  ({props}) => props.variant === 'title' ? <strong>{props.text}</strong> : props.variant === 'caption' ? <small>{props.text}</small> : <span>{props.text}</span>),
 createComponentImplementation({name: 'Card', schema: z.object({child: ComponentIdSchema}).strict()},
  ({props, buildChild}) => <div className="chat-a2ui-card">{buildChild(props.child)}</div>),
 createComponentImplementation({name: 'Column', schema: z.object({children}).strict()},
  ({props, buildChild}) => <div className="chat-a2ui-column">{props.children.map(id => <Fragment key={id}>{buildChild(id)}</Fragment>)}</div>),
 createComponentImplementation({name: 'Row', schema: z.object({children}).strict()},
  ({props, buildChild}) => <div className="chat-a2ui-row">{props.children.map(id => <Fragment key={id}>{buildChild(id)}</Fragment>)}</div>),
 createComponentImplementation({name: 'Divider', schema: z.object({}).strict()}, () => <hr/>),
]);

const surfaceId = 'chat';
const express = new ExpressParser(catalog, surfaceId, 'v1.0');
const inheritedNames = new Set([...Object.getOwnPropertyNames(Object.prototype), 'prototype']);

function compile(body: string) {
 if (body.length > 8192) return;
 // Keep the SDK's compiler on static catalog syntax. Exclude paths, maps,
 // expressions and inherited object keys before processing model input.
 const structure = body.replace(/"(?:\\[^\r\n]|[^"\\\r\n])*"/g, '');
 if (!/^[\w\s=,()[\]]*$/u.test(structure) || (structure.match(/\w+/g) ?? []).some(word => inheritedNames.has(word))) return;
 const [create, ...extra] = express.compile(body);
 if (extra.length || !create || create.version !== '1.0' || !('createSurface' in create)) return;
 const opening = create.createSurface;
 if (opening.catalogId !== catalog.id || opening.surfaceId !== surfaceId || !opening.components?.length || opening.components.length > 40) return;
 if (Object.keys(opening).some(key => key !== 'catalogId' && key !== 'surfaceId' && key !== 'components')) return;
 return create;
}

export default function ExpressChatSurface({body, fallback}: {body: string; fallback: ReactNode}) {
 const [surface, setSurface] = useState<SurfaceModel<ReactCatalogComponent> | null>();
 useEffect(() => {
  const processor = new MessageProcessor<ReactCatalogComponent>([catalog], undefined, {version: 'v1.0'});
  try {
   const messages = compile(body);
   if (messages) processor.processMessages(messages);
   setSurface(processor.model.surfacesMap.get(surfaceId) ?? null);
  } catch {
   setSurface(null);
  }
  return () => processor.dispose();
 }, [body]);
 if (surface === undefined) return null;
 return surface ? <div className="chat-a2ui-surface"><A2uiSurface surface={surface}/></div> : fallback;
}
