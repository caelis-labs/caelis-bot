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
 const generation=useRef(0);
 const load=useCallback(() => {
  const current=++generation.current;
  setSelection(null);setPreview(null);setError('');setActionError('');
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
  const action=(event:Event)=>{
   const detail=(event as CustomEvent<{action:string;id:string;name:string}>).detail;
   if(!detail?.id)return;
   let result:Promise<unknown>;
   switch(detail.action){
    case 'open': result=desktop('OpenArtifact',detail.id);break;
    case 'save': result=desktop('SaveArtifactAs',detail.id,detail.name);break;
    case 'path': result=desktop('CopyArtifactPath',detail.id);break;
    case 'content': result=desktop('CopyArtifactContent',detail.id);break;
    case 'reveal': result=backend('RevealArtifact',detail.id);break;
    default:return;
   }
   setActionError('');
   void result.catch(failure=>setActionError(failure instanceof Error?failure.message:t('chat.artifactUnavailable')));
  };
  window.addEventListener('artifact-preview-toolbar-action',action);
  return()=>window.removeEventListener('artifact-preview-toolbar-action',action);
 },[t]);
 const isMarkdown=/\.(md|markdown)$/i.test(selection?.name??'');
 return <main className="artifact-preview-window">
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
