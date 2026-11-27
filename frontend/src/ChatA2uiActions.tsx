import {createContext, useContext, useLayoutEffect, useState, type ReactNode} from 'react';
import type {ReactCatalogComponent} from '@a2ui/react';
import type {ActionPayload, SurfaceModel} from '@a2ui/web_core';
import {applyChatState, fillChatTemplate} from './chat-a2ui-actions';
import {ChatA2uiSubmission} from './chat-a2ui-submit';
import {useI18n} from './i18n';

export const ChatA2uiSubmitEnabled = createContext(false);

export function ChatA2uiActions({view, replyKey, receiving, visible, children}: {
 view: SurfaceModel<ReactCatalogComponent>; replyKey?: string; receiving: boolean; visible: boolean; children: ReactNode;
}) {
 const {t} = useI18n();
 const submission = useContext(ChatA2uiSubmission);
 const [problem, setProblem] = useState<'invalid' | 'tooLong'>();
 const outcome = replyKey ? submission?.outcome(replyKey, true) : undefined;
 const enabled = !!(replyKey && visible && !receiving && submission?.available && outcome !== 'sending' && outcome !== 'unknown');

 useLayoutEffect(() => {
  const subscription = view.onAction.subscribe(async ({name, context, userMessage}: ActionPayload) => {
   if (!visible) return;
   if (name === 'set') {
    if (applyChatState(view.dataModel, context)) setProblem(undefined);
    return;
   }
   if (name !== 'submit' || !userMessage || !enabled || !replyKey || !submission) return;
   const text = fillChatTemplate(userMessage, context);
   if (!text) {
    setProblem('invalid');
    return;
   }
   if (new TextEncoder().encode(text).length > 64 * 1024) {
    setProblem('tooLong');
    return;
   }
   setProblem(undefined);
   await submission.send(replyKey, text, true);
  });
  return () => subscription.unsubscribe();
 }, [view, visible, enabled, replyKey, submission]);
 const status = problem === 'invalid' ? t('chat.a2uiInvalid') :
  problem === 'tooLong' ? t('chat.a2uiTooLong') :
  outcome === 'sending' ? t('chat.statusSending') :
  outcome === 'unknown' ? t('chat.a2uiSendUnknown') :
  outcome === 'rejected' ? t('chat.a2uiSendRejected') : undefined;
 return <ChatA2uiSubmitEnabled.Provider value={enabled}>
  {children}
  {status && <div className={problem || outcome === 'rejected' ? 'a2ui-error-message' : 'chat-a2ui-hint'} role="status">{status}</div>}
 </ChatA2uiSubmitEnabled.Provider>;
}
