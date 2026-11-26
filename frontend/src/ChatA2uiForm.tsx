import {createContext, useCallback, useContext, useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode} from 'react';
import {ChatA2uiSubmission} from './chat-a2ui-submit';
import {useI18n} from './i18n';

type Answer = {label: string; value: string; invalid: boolean};
const VisibleAnswers = createContext<{
 register: (element: HTMLElement, answer: Answer) => () => void;
 checked: boolean;
 frozen: boolean;
} | null>(null);
export const ChatA2uiFormOrigin = createContext<{replyKey?: string; receiving: boolean}>({receiving: false});

export function useAnswer(label: string, value: string, invalid: boolean) {
 const form = useContext(VisibleAnswers);
 const root = useRef<HTMLDivElement>(null);
 const register = form?.register;
 useLayoutEffect(() => {
  if (root.current && register) return register(root.current, {label, value, invalid});
 }, [register, label, value, invalid]);
 return {root, inForm: !!form, checked: form?.checked, frozen: form?.frozen ?? false};
}

export function ChatA2uiForm({title, children}: {title: string; children: ReactNode}) {
 const {t} = useI18n();
 const id = useId();
 const {replyKey, receiving} = useContext(ChatA2uiFormOrigin);
 const submission = useContext(ChatA2uiSubmission);
 const [origin, setOrigin] = useState<{replyKey: string; key: string}>();
 const [checked, setChecked] = useState(false);
 const [tooLong, setTooLong] = useState(false);
 const [progress, setProgress] = useState({answered: 0, total: 0});
 const root = useRef<HTMLElement>(null);
 const fields = useRef(new Map<HTMLElement, Answer>());

 useEffect(() => {
  if (!replyKey || !crypto.subtle) return;
  let active = true;
  void crypto.subtle.digest('SHA-256', new TextEncoder().encode(replyKey)).then(hash => {
   const key = 'a2ui-' + Array.from(new Uint8Array(hash).slice(0, 16), byte => byte.toString(16).padStart(2, '0')).join('') + '-';
   if (active) setOrigin({replyKey, key});
  }).catch(() => {});
  return () => { active = false; };
 }, [replyKey]);

 const register = useCallback((element: HTMLElement, answer: Answer) => {
  const update = () => {
   const total = fields.current.size;
   const answered = [...fields.current.values()].filter(field => !field.invalid && field.value.trim() !== '').length;
   setProgress(current => current.total === total && current.answered === answered ? current : {total, answered});
  };
  fields.current.set(element, answer);
  update();
  return () => { fields.current.delete(element); update(); };
 }, []);

 const key = origin?.replyKey === replyKey ? origin?.key : undefined;
 const outcome = key ? submission?.outcome(key) : undefined;
 const frozen = outcome === 'sending' || outcome === 'unknown' || outcome === 'accepted';
 const enabled = !!(key && submission?.available && !receiving && !frozen && progress.total);
 const submit = () => {
  if (!enabled || !key || !submission) return;
  // Only mounted controls are included. DOM order matches the questions the
  // person sees, including groups and repeated data-model templates.
  const visible = [...root.current!.querySelectorAll<HTMLElement>('[data-a2ui-answer]')]
   .flatMap(element => {
    const answer = fields.current.get(element);
    return answer ? [{element, answer}] : [];
   });
  if (!visible.length) return;
  const invalid = visible.find(({answer}) => answer.invalid);
  setChecked(true);
  if (invalid) {
   invalid.element.querySelector<HTMLElement>('input:not([type="search"]),textarea')?.focus();
   return;
  }
  const text = [title, ...visible.map(({answer}) => `${answer.label}\n${answer.value.trim() ? answer.value : t('chat.a2uiUnanswered')}`)].join('\n\n');
  if (new TextEncoder().encode(text).length > 64 * 1024) {
   setTooLong(true);
   return;
  }
  setTooLong(false);
  void submission.send(key, text);
 };
 const status = outcome === 'sending' ? t('chat.statusSending') : outcome === 'accepted' ? t('chat.a2uiSubmitted') :
  outcome === 'unknown' ? t('chat.a2uiSendUnknown') : outcome === 'rejected' ? t('chat.a2uiSendRejected') :
  tooLong ? t('chat.a2uiTooLong') : t('chat.a2uiSubmitHint');
 const error = outcome === 'rejected' || tooLong;

 return <section className="chat-a2ui-form" ref={root} aria-labelledby={id} aria-busy={outcome === 'sending'}>
  <div className="chat-a2ui-heading">
   <h3 id={id}>{title}</h3>
   {outcome !== 'accepted' && progress.total > 0 &&
    <span>{t('chat.a2uiProgress', {answered: progress.answered, total: progress.total})}</span>}
  </div>
  {outcome !== 'accepted' && <VisibleAnswers.Provider value={{register, checked, frozen}}>
   <div className="chat-a2ui-questions">{children}</div>
  </VisibleAnswers.Provider>}
  <div className="chat-a2ui-footer">
   {outcome !== 'accepted' && <button className="chat-a2ui-submit" type="button" disabled={!enabled} aria-describedby={`${id}-status`} onClick={submit}>
    {t(outcome === 'rejected' ? 'chat.a2uiRetry' : 'chat.a2uiSubmit')}
   </button>}
   <div className={error ? 'a2ui-error-message' : 'chat-a2ui-hint'} id={`${id}-status`} role="status">{status}</div>
  </div>
 </section>;
}
