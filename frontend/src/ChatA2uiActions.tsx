import {createContext, useContext, useLayoutEffect, useState, type ReactNode} from 'react';
import type {ReactCatalogComponent} from '@a2ui/react';
import type {ActionPayload, SurfaceModel} from '@a2ui/web_core';
import {applyChatState, fillChatTemplate} from './chat-a2ui-actions';
import {ChatA2uiSubmission} from './chat-a2ui-submit';
import {useChatA2uiOrigin} from './ChatA2uiForm';
import {useI18n} from './i18n';

export const ChatA2uiSubmitEnabled = createContext(false);

export function ChatA2uiActions({view, replyKey, receiving, visible, hasSubmit, children}: {
 view: SurfaceModel<ReactCatalogComponent>; replyKey?: string; receiving: boolean; visible: boolean;
 hasSubmit: boolean; children: ReactNode;
}) {
 const {t} = useI18n();
 const prefix = useChatA2uiOrigin(hasSubmit ? replyKey : undefined);
 const submission = useContext(ChatA2uiSubmission);
 const [problem, setProblem] = useState<'invalid' | 'tooLong'>();
 const cards = prefix ? submission?.cards(prefix) ?? [] : [];
 const pending = cards.find(card => card.status === 'unknown' || card.status === 'sending');
 const enabled = !!(prefix && visible && !receiving && submission?.available && !pending);

 useLayoutEffect(() => {
  const subscription = view.onAction.subscribe(async ({name, context, userMessage, sourceComponentId}: ActionPayload) => {
   if (!visible) return;
   if (name === 'set') {
    if (applyChatState(view.dataModel, context)) setProblem(undefined);
    return;
   }
   if (name !== 'submit' || !userMessage || !enabled || !prefix || !submission) return;
   const payload = fillChatTemplate(userMessage, context);
   if (!payload) {
    setProblem('invalid');
    return;
   }
   if (new TextEncoder().encode(payload.text).length > 64 * 1024) {
    setProblem('tooLong');
    return;
   }
   setProblem(undefined);
   await submission.sendCard(prefix, sourceComponentId, payload.text, payload.fields);
  });
  return () => subscription.unsubscribe();
 }, [view, visible, enabled, prefix, submission]);
 const sent = cards.filter(card => card.status === 'accepted' || card.status === 'completed');
 const latest = pending ?? cards.at(-1);
 const status = problem === 'invalid' ? t('chat.a2uiInvalid') :
  problem === 'tooLong' ? t('chat.a2uiTooLong') :
  latest?.status === 'sending' ? t('chat.statusSending') :
  latest?.status === 'unknown' ? t('chat.a2uiSendUnknown') :
  latest?.status === 'rejected' ? t('chat.a2uiSendRejected') : undefined;
 return <ChatA2uiSubmitEnabled.Provider value={enabled}>
  {children}
  {status && <div className={problem || latest?.status === 'rejected' ? 'a2ui-error-message' : 'chat-a2ui-hint'} role="status">{status}</div>}
  {!!sent.length && <details className="chat-a2ui-snapshots">
   <summary>{t('chat.a2uiSnapshots', {count: sent.length})}</summary>
   <ol>{sent.map(card => <li key={card.requestId}>
    {card.chatCard?.fields.length ? <dl>{card.chatCard.fields.map(field => <FragmentField key={field.name} name={field.name} value={field.value}/>)}</dl> : card.text}
   </li>)}</ol>
  </details>}
 </ChatA2uiSubmitEnabled.Provider>;
}

function FragmentField({name, value}: {name: string; value: string}) {
 return <><dt>{name}</dt><dd>{value}</dd></>;
}
