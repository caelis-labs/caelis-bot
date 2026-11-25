import {useEffect, useId, useState, type ReactNode} from 'react';
import {A2uiSurface, createComponentImplementation, type ReactCatalogComponent} from '@a2ui/react';
import {MessageProcessor, type SurfaceModel} from '@a2ui/web_core';
import {chatCatalog, chatText, formComponents} from './chat-a2ui-catalog';
import {chatValidation, compileChatSurface} from './chat-a2ui-compile';
import {useI18n} from './i18n';
import './chat-a2ui.css';

// The Basic Catalog's Text schema also accepts bindings and pure functions.
// The chat view displays the resolved value as text without a Markdown renderer.
const text = createComponentImplementation(chatText, ({props}) => {
 if (props.variant === 'title') return <strong>{props.text}</strong>;
 if (props.variant === 'caption') return <small>{props.text}</small>;
 return <span>{props.text}</span>;
});
const catalog = chatCatalog.withEntries<ReactCatalogComponent>(
 [text, ...[...chatCatalog.components.values()].filter(component => 'element' in component)],
 [...chatCatalog.functions.values()],
);

export default function ExpressChatSurface({body, fallback}: {body: string; fallback: ReactNode}) {
 const {t} = useI18n();
 const id = `chat-${useId()}`;
 const [view, setView] = useState<{surface: SurfaceModel<ReactCatalogComponent>; localForm: boolean} | null>();
 useEffect(() => {
  const processor = new MessageProcessor<ReactCatalogComponent>([catalog], undefined, {version: 'v1.0', validationConfig: chatValidation});
  let active = true, failed = false;
  let errors: ReturnType<SurfaceModel['onError']['subscribe']> | undefined;
  try {
   const message = compileChatSurface(body);
   if (message) processor.processMessages({...message, createSurface: {...message.createSurface, surfaceId: id}});
   const surface = processor.model.surfacesMap.get(id);
   const localForm = message?.createSurface.components?.some(component => formComponents.includes(component.component)) ?? false;
   // Resolver errors can be raised while React is building the child
   // tree. Fail this block after that render, and ignore a disposed view.
   errors = surface?.onError.subscribe(() => {
    if (failed) return;
    failed = true;
    queueMicrotask(() => { if (active) setView(null); });
   });
   setView(surface ? {surface, localForm} : null);
  } catch {
   setView(null);
  }
  return () => { active = false; errors?.unsubscribe(); processor.dispose(); };
 }, [body, id]);
 if (view === undefined) return null;
 if (!view) return fallback;
 return <div className="chat-a2ui-surface">
  {view.localForm && <p className="chat-a2ui-local">{t('chat.a2uiLocalDraft')}</p>}
  <A2uiSurface surface={view.surface}/>
 </div>;
}
