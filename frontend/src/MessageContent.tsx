import { Children, isValidElement, memo, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import Markdown, {type ExtraProps, type Options} from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { backend, desktop } from './desktop';
import { messageURL } from './message-url';
import { useI18n } from './i18n';
import { useStreamingText } from './use-streaming-text';
import { markdownChunks } from './markdown-chunks';
import { remarkChatA2ui } from './chat-a2ui-markdown';
import { ChatA2uiBlock } from './ChatA2uiBlock';
import { ChatA2uiReply } from './chat-a2ui-submit';

export function CopyText({text,label,report}:{text:string;label?:string;report:(error:string)=>void}) {
 const {t} = useI18n();
 const [copied,setCopied]=useState(false);
 const effectiveLabel = label ?? t('chat.copyMessage');
 useEffect(()=>{if(!copied)return;const timer=window.setTimeout(()=>setCopied(false),1800);return()=>window.clearTimeout(timer);},[copied]);
 const copy=async()=>{
  try { await desktop('CopyText',text);setCopied(true); }
  catch {report(t('chat.copyFailed'));}
 };
 return <button type="button" className="copy-text" aria-label={copied?t('chat.copied'):effectiveLabel} title={copied?t('chat.copied'):effectiveLabel} onClick={()=>void copy()} onBlur={()=>setCopied(false)}>
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
   {copied?<path d="m5 12 4 4L19 6"/>:<><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/></>}
  </svg>
 </button>;
}
function plain(children:ReactNode):string {
 return Children.toArray(children).map(c=>isValidElement<{children?:ReactNode}>(c)?plain(c.props.children):String(c)).join('');
}
function ExternalLink({url,children,report}:{url?:string;children:ReactNode;report:(error:string)=>void}) {
 const {t} = useI18n();
 const safe=messageURL(url??'');
 if(!safe)return <span>{children}</span>;
 return <button type="button" role="link" className="message-link" title={safe} onClick={()=>void backend('OpenMessageLink',safe).catch(()=>report(t('chat.openLinkFailed')))}>{children}</button>;
}
type A2uiState = 'receiving' | 'final';

function A2uiSlot({node}: ExtraProps) {
 const {source, body, closed, receiving, permitted, ordinal} = node?.properties ?? {};
 const reply = useContext(ChatA2uiReply);
 if (typeof source !== 'string' || typeof body !== 'string') return null;
 return <ChatA2uiBlock source={source} body={body} closed={closed === true} receiving={receiving === true} permitted={permitted === true} replyKey={reply && typeof ordinal === 'number' ? JSON.stringify([reply, ordinal]) : undefined}/>;
}

const MarkdownChunk=memo(function MarkdownChunk({source,report,a2ui,blockOffset}:{source:string;report:(error:string)=>void;a2ui?:A2uiState;blockOffset:number}) {
 const {t} = useI18n();
 const plugins: Options['remarkPlugins'] = a2ui ? [remarkGfm, [remarkChatA2ui, {source, receiving: a2ui === 'receiving', blockOffset}]] : [remarkGfm];
 const components = {
  'chat-a2ui-block': A2uiSlot,
  a:({href,children}:React.ComponentProps<'a'>)=><ExternalLink url={href} report={report}>{children}</ExternalLink>,
  // Generated image URLs must not fetch anything merely because a reply arrives.
  img:({src,alt}:React.ComponentProps<'img'>)=><ExternalLink url={typeof src==='string'?src:''} report={report}>{t('chat.imagePrefix')}{alt||t('chat.imageOpen')} ↗</ExternalLink>,
  pre:({children}:React.ComponentProps<'pre'>)=><div className="code-block"><CopyText text={plain(children)} label={t('chat.copyCode')} report={report}/><pre>{children}</pre></div>,
  table:({children}:React.ComponentProps<'table'>)=><div className="message-table"><table>{children}</table></div>,
 };
 return <Markdown skipHtml remarkPlugins={plugins} urlTransform={messageURL} components={components}>{source}</Markdown>;
});
export const MessageContent=memo(function MessageContent({text,report,animate=false,a2ui}:{text:string;report:(error:string)=>void;animate?:boolean;a2ui?:A2uiState}) {
 const shown=useStreamingText(text,animate);
 const chunks=useMemo(()=>markdownChunks(shown, !!a2ui),[shown,a2ui]);
 let blockOffset=0;
 return <div className="markdown-body">{chunks.map((source,index)=>{
  const chunk=<MarkdownChunk key={index} source={source} report={report} a2ui={a2ui} blockOffset={blockOffset}/>;
  // Count preceding candidate lines conservatively across independent chunks.
  if(a2ui)blockOffset+=(source.match(/^<a2ui>[ \t]*\r?$/gm)??[]).length;
  return chunk;
 })}</div>;
});
