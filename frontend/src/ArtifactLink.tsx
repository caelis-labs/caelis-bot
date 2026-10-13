import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { backend, desktop } from './desktop';
import type { Artifact } from './backend/contract';
import { SettingsDialog } from './settings/runtime/SettingsDialog';
import { useI18n } from './i18n';

type Preview = { text: string; truncated: boolean };
const textFile = (name: string) => /\.(txt|md|markdown|json|jsonl|sql|log|csv|tsv|ya?ml|xml|toml|ini|sh|py|go|swift|js|ts|tsx|css|html)$/i.test(name);
const fileLabel = (value: string) => {
 const name = value.replaceAll('\\','/').split('/').at(-1) || 'artifact';
 return name.length > 255 || /[\u0000-\u001f\u007f]/.test(name) ? 'artifact' : name;
};

export function ArtifactLink({ file, report }: { file: Artifact; report: (message: string) => void }) {
 const { t } = useI18n();
 const trigger = useRef<HTMLButtonElement>(null);
 const menu = useRef<HTMLDivElement>(null);
 const [previewOpen,setPreviewOpen] = useState(false);
 const [preview,setPreview] = useState<Preview | null>(null);
 const [previewError,setPreviewError] = useState('');
 const [position,setPosition] = useState<{x:number;y:number}|null>(null);
 const name = fileLabel(file.name);
 const isText = textFile(name);
 const failure = (error:unknown) => report(error instanceof Error ? error.message : t('chat.artifactUnavailable'));
 const action = (method:string,...args:unknown[]) => { setPosition(null); void desktop(method,file.id,...args).catch(failure); };
 const showPreview = () => {
  setPosition(null); setPreviewOpen(true); setPreview(null); setPreviewError('');
 };
 useEffect(() => {
  if (!previewOpen || !isText) return;
  let live = true;
  void desktop<Preview>('PreviewArtifact',file.id).then(value => { if(live)setPreview(value); }).catch(error => {
   if(live)setPreviewError(error instanceof Error ? error.message : t('chat.artifactUnavailable'));
  });
  return () => { live = false; };
 },[previewOpen,file.id,isText,t]);
 useEffect(() => {
  if(!position)return;
  const outside=(event:PointerEvent)=>{if(!menu.current?.contains(event.target as Node)&&event.target!==trigger.current)setPosition(null);};
  const escape=(event:KeyboardEvent)=>{if(event.key==='Escape'){event.preventDefault();setPosition(null);trigger.current?.focus();}};
  window.addEventListener('pointerdown',outside,true);window.addEventListener('keydown',escape,true);
  return()=>{window.removeEventListener('pointerdown',outside,true);window.removeEventListener('keydown',escape,true);};
 },[position]);
 return <>
  <button ref={trigger} type="button" className="artifact-link" aria-haspopup="menu" onClick={()=>isText?showPreview():action('OpenArtifact')} onContextMenu={event=>{
   event.preventDefault();event.stopPropagation();setPosition({x:Math.max(8,Math.min(event.clientX,innerWidth-210)),y:Math.max(8,Math.min(event.clientY,innerHeight-235))});
  }} onKeyDown={event=>{if(event.key==='ContextMenu'||event.shiftKey&&event.key==='F10'){event.preventDefault();const box=trigger.current!.getBoundingClientRect();setPosition({x:box.left,y:box.bottom+4});}}}>
   <span aria-hidden="true">↗</span>{name}
  </button>
  {position&&createPortal(<div ref={menu} className="artifact-context-menu" role="menu" style={{left:position.x,top:position.y}}>
   {isText&&<button role="menuitem" onClick={showPreview}>{t('chat.previewArtifact')}</button>}
   <button role="menuitem" onClick={()=>action('OpenArtifact')}>{t('chat.openArtifact')}</button>
   <button role="menuitem" onClick={()=>action('SaveArtifactAs',name)}>{t('chat.saveArtifactAs')}</button>
   <button role="menuitem" onClick={()=>action('CopyArtifactPath')}>{t('chat.copyArtifactPath')}</button>
   {isText&&<button role="menuitem" onClick={()=>action('CopyArtifactContent')}>{t('chat.copyArtifactContent')}</button>}
   <button role="menuitem" onClick={()=>{setPosition(null);void backend('RevealArtifact',file.id).catch(failure);}}>{t('chat.revealInFinder')}</button>
  </div>,document.body)}
  {previewOpen&&<SettingsDialog portalTarget={document.body} returnFocus={trigger.current} title={name} className="artifact-preview-dialog" onClose={()=>setPreviewOpen(false)}>
   {previewError?<p role="alert">{previewError}</p>:preview?<><textarea readOnly aria-label={name} value={preview.text}/>{preview.truncated&&<p className="quiet">{t('chat.artifactPreviewTruncated')}</p>}</>:<p role="status">{t('common.loading')}</p>}
   <div className="artifact-preview-actions"><button type="button" onClick={()=>action('OpenArtifact')}>{t('chat.openArtifact')}</button>{preview&&!preview.truncated&&<button type="button" onClick={()=>action('CopyArtifactContent')}>{t('chat.copyArtifactContent')}</button>}</div>
  </SettingsDialog>}
 </>;
}
