import { Children, isValidElement, memo, useEffect, useState, type ReactNode } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { backend, desktop } from './desktop';
import { messageURL } from './message-url';

export function CopyText({text,label='复制消息',report}:{text:string;label?:string;report:(error:string)=>void}) {
 const [copied,setCopied]=useState(false);
 useEffect(()=>{if(!copied)return;const timer=window.setTimeout(()=>setCopied(false),1800);return()=>window.clearTimeout(timer);},[copied]);
 const copy=async()=>{
  try { await desktop('CopyText',text);setCopied(true); }
  catch {report('暂时无法复制，请选择文本后复制');}
 };
 return <button type="button" className="copy-text" aria-label={copied?'已复制':label} title={copied?'已复制':label} onClick={()=>void copy()} onBlur={()=>setCopied(false)}>
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
   {copied?<path d="m5 12 4 4L19 6"/>:<><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1"/></>}
  </svg>
 </button>;
}
function plain(children:ReactNode):string {
 return Children.toArray(children).map(c=>isValidElement<{children?:ReactNode}>(c)?plain(c.props.children):String(c)).join('');
}
function ExternalLink({url,children,report}:{url?:string;children:ReactNode;report:(error:string)=>void}) {
 const safe=messageURL(url??'');
 if(!safe)return <span>{children}</span>;
 return <button type="button" role="link" className="message-link" title={safe} onClick={()=>void backend('OpenMessageLink',safe).catch(()=>report('暂时无法打开链接'))}>{children}</button>;
}
export const MessageContent=memo(function MessageContent({text,report}:{text:string;report:(error:string)=>void}) {
 return <div className="markdown-body"><Markdown skipHtml remarkPlugins={[remarkGfm]} urlTransform={messageURL} components={{
  a:({href,children})=><ExternalLink url={href} report={report}>{children}</ExternalLink>,
  // Generated image URLs must not fetch anything merely because a reply arrives.
  img:({src,alt})=><ExternalLink url={typeof src==='string'?src:''} report={report}>图片：{alt||'打开查看'} ↗</ExternalLink>,
  pre:({children})=><div className="code-block"><CopyText text={plain(children)} label="复制代码" report={report}/><pre>{children}</pre></div>,
  table:({children})=><div className="message-table"><table>{children}</table></div>,
 }}>{text}</Markdown></div>;
});
