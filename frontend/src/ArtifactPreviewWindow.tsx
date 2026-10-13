import { useCallback, useEffect, useRef, useState } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { backend, desktop } from './desktop';
import { useI18n } from './i18n';

type Selection = { id: string; name: string };
type Preview = { text: string; truncated: boolean };

export function ArtifactPreviewWindow() {
 const { t } = useI18n();
 const [selection,setSelection] = useState<Selection | null>(null);
 const [preview,setPreview] = useState<Preview | null>(null);
 const [error,setError] = useState('');
 const [actionError,setActionError] = useState('');
 const [moreOpen,setMoreOpen] = useState(false);
 const moreButton=useRef<HTMLButtonElement>(null);
 const menu=useRef<HTMLDivElement>(null);
 const generation=useRef(0);
 const load=useCallback(() => {
  const current=++generation.current;
  setSelection(null);setPreview(null);setError('');setActionError('');setMoreOpen(false);
  void desktop<Selection>('CurrentArtifactPreview').then(async value => {
   if(current!==generation.current)return;
   setSelection(value);
   try {
    const content=await desktop<Preview>('PreviewArtifact',value.id);
    if(current===generation.current)setPreview(content);
   } catch (failure) {
    if(current===generation.current)setError(failure instanceof Error?failure.message:t('chat.artifactUnavailable'));
   }
  }).catch(failure => {
   if(current===generation.current)setError(failure instanceof Error?failure.message:t('chat.artifactUnavailable'));
  });
 },[t]);
 useEffect(()=>{
  window.addEventListener('artifact-preview-changed',load);
  load();
  return()=>{generation.current++;window.removeEventListener('artifact-preview-changed',load);};
 },[load]);
 useEffect(()=>{
  if(!moreOpen)return;
  const outside=(event:PointerEvent)=>{if(!menu.current?.contains(event.target as Node)&&!moreButton.current?.contains(event.target as Node))setMoreOpen(false);};
  const escape=(event:KeyboardEvent)=>{if(event.key==='Escape'){event.preventDefault();setMoreOpen(false);moreButton.current?.focus();}};
  window.addEventListener('pointerdown',outside,true);window.addEventListener('keydown',escape,true);
  return()=>{window.removeEventListener('pointerdown',outside,true);window.removeEventListener('keydown',escape,true);};
 },[moreOpen]);
 const act=(method:string,...args:unknown[])=>{
  if(!selection)return;
  setMoreOpen(false);setActionError('');
  void desktop(method,selection.id,...args).catch(failure=>setActionError(failure instanceof Error?failure.message:t('chat.artifactUnavailable')));
 };
 const reveal=()=>{
  if(!selection)return;
  setMoreOpen(false);setActionError('');
  void backend('RevealArtifact',selection.id).catch(failure=>setActionError(failure instanceof Error?failure.message:t('chat.artifactUnavailable')));
 };
 const isMarkdown=/\.(md|markdown)$/i.test(selection?.name??'');
 return <main className="artifact-preview-window">
  <header className="artifact-preview-header">
   <h1 title={selection?.name}>{selection?.name??t('common.loading')}</h1>
   <div className="artifact-preview-controls">
    <button type="button" disabled={!selection} onClick={()=>act('OpenArtifact')}>{t('chat.openArtifact')}</button>
    <button ref={moreButton} type="button" className="artifact-preview-more" disabled={!selection} aria-label={t('chat.moreArtifactActions')} title={t('chat.moreArtifactActions')} aria-haspopup="menu" aria-expanded={moreOpen} onClick={()=>setMoreOpen(value=>!value)}>⋯</button>
   </div>
   {moreOpen&&selection&&<div ref={menu} className="artifact-preview-menu" role="menu">
    <button role="menuitem" onClick={()=>act('SaveArtifactAs',selection.name)}>{t('chat.saveArtifactAs')}</button>
    <button role="menuitem" onClick={()=>act('CopyArtifactPath')}>{t('chat.copyArtifactPath')}</button>
    <button role="menuitem" onClick={()=>act('CopyArtifactContent')}>{t('chat.copyArtifactContent')}</button>
    <button role="menuitem" onClick={reveal}>{t('chat.revealInFinder')}</button>
   </div>}
  </header>
  {actionError&&<p className="artifact-preview-error" role="alert">{actionError}</p>}
  <section className="artifact-preview-scroll" aria-label={selection?.name??t('chat.previewArtifact')}>
   {error?<p className="artifact-preview-error" role="alert">{error}</p>:preview?
    <article className={isMarkdown?'artifact-preview-markdown markdown-body':'artifact-preview-plain'}>
     {isMarkdown?<Markdown skipHtml remarkPlugins={[remarkGfm]} components={{
      a:({children})=><span>{children}</span>,
      img:({alt})=><span>{alt??''}</span>,
     }}>{preview.text}</Markdown>:<pre>{preview.text}</pre>}
     {preview.truncated&&<p className="artifact-preview-truncated">{t('chat.artifactPreviewTruncated')}</p>}
    </article>:<p className="artifact-preview-loading" role="status">{t('common.loading')}</p>}
  </section>
 </main>;
}
