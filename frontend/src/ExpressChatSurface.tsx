import {useEffect, useId, useMemo, useState, type ReactNode} from 'react';
import {A2uiSurface, type ReactCatalogComponent} from '@a2ui/react';
import {MessageProcessor, type SurfaceModel} from '@a2ui/web_core';
import {chatCatalog, formComponents} from './chat-a2ui-catalog';
import {chatValidation, compileChatSurface} from './chat-a2ui-compile';
import {ChatA2uiFormOrigin} from './ChatA2uiForm';
import {ChatA2uiActions} from './ChatA2uiActions';
import {chatEvent} from './chat-a2ui-actions';
import {chatControls} from './ChatA2uiControls';
import {useI18n} from './i18n';
import './chat-a2ui.css';

const catalog = chatCatalog.withEntries<ReactCatalogComponent>(
 chatControls,
 [...chatCatalog.functions.values()],
);

export default function ExpressChatSurface({body, fallback, replyKey, receiving, visible}: {
 body: string; fallback: ReactNode; replyKey?: string; receiving: boolean; visible: boolean;
}) {
 const message = useMemo(() => {
  try { return compileChatSurface(body); }
  catch { return undefined; }
 }, [body]);
 if (!message) return visible ? fallback : null;
 const components = message.createSurface.components ?? [];
 const submitForm = components.some(component => component.component === 'Form');
 const hasActions = components.some(component => component.component === 'Button');
 const hasSubmit = components.some(component => component.component === 'Button' && chatEvent(component.action) === 'submit');
 if (!visible && !submitForm && !hasActions) return null;
 return <ResolvedSurface message={message} fallback={fallback} replyKey={replyKey} receiving={receiving} visible={visible}
  hasSubmit={hasSubmit} localForm={!hasActions && !submitForm && components.some(component => formComponents.includes(component.component))}/>;
}

function ResolvedSurface({message, fallback, replyKey, receiving, visible, localForm, hasSubmit}: {
 message: NonNullable<ReturnType<typeof compileChatSurface>>; fallback: ReactNode; replyKey?: string;
 receiving: boolean; visible: boolean; localForm: boolean; hasSubmit: boolean;
}) {
 const {t} = useI18n();
 const id = `chat-${useId()}`;
 const [view, setView] = useState<SurfaceModel<ReactCatalogComponent> | null>();
 useEffect(() => {
  const processor = new MessageProcessor<ReactCatalogComponent>([catalog], undefined, {version: 'v1.0', validationConfig: chatValidation});
  let active = true, failed = false;
  let errors: ReturnType<SurfaceModel['onError']['subscribe']> | undefined;
  try {
   processor.processMessages({...message, createSurface: {...message.createSurface, surfaceId: id}});
   const surface = processor.model.surfacesMap.get(id);
   // Resolver errors can be raised while React is building the child
   // tree. Fail this block after that render, and ignore a disposed view.
   errors = surface?.onError.subscribe(() => {
    if (failed) return;
    failed = true;
    queueMicrotask(() => { if (active) setView(null); });
   });
   setView(surface ?? null);
  } catch {
   setView(null);
  }
  return () => { active = false; errors?.unsubscribe(); processor.dispose(); };
 }, [message, id]);
 if (view === undefined) return null;
 if (!view) return visible ? fallback : null;
 return <div className="chat-a2ui-surface" hidden={!visible}>
  {localForm && <p className="chat-a2ui-local">{t('chat.a2uiLocalDraft')}</p>}
  <ChatA2uiFormOrigin.Provider value={{replyKey, receiving}}>
   <ChatA2uiActions view={view} replyKey={replyKey} receiving={receiving} visible={visible} hasSubmit={hasSubmit}>
    <A2uiSurface surface={view}/>
   </ChatA2uiActions>
  </ChatA2uiFormOrigin.Provider>
 </div>;
}
