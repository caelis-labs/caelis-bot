import { useEffect, useId, useRef, type ReactNode } from 'react';

export function SettingsDialog({ title, description, busy = false, onClose, children }: { title: string; description?: string; busy?: boolean; onClose: () => void; children: ReactNode }) {
 const titleID = useId(), ref = useRef<HTMLElement>(null);
 useEffect(() => {
  const prior = document.activeElement as HTMLElement | null;
  ref.current?.focus();
  const guard = (event: Event) => event.preventDefault();
  window.addEventListener('settings-navigate', guard);
  return () => { window.removeEventListener('settings-navigate', guard); if (prior?.isConnected) prior.focus(); };
 }, []);
 return <div className="setup-scrim runtime-dialog-scrim" onPointerDown={event => { if (event.target === event.currentTarget && !busy) onClose(); }}>
  <section ref={ref} tabIndex={-1} className="setup-dialog runtime-dialog" role="dialog" aria-modal="true" aria-labelledby={titleID} onKeyDown={event => {
   if (event.key === 'Escape') { event.stopPropagation(); if (!busy) onClose(); }
   if (event.key !== 'Tab') return;
   event.stopPropagation();
   const nodes = [...ref.current!.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),summary,a[href]')].filter(node => node.getClientRects().length);
   const first = nodes[0], last = nodes.at(-1);
   if (!first) { event.preventDefault(); return; }
   if (event.shiftKey && (document.activeElement === first || document.activeElement === ref.current)) { event.preventDefault(); last?.focus(); }
   else if (!event.shiftKey && (document.activeElement === last || document.activeElement === ref.current)) { event.preventDefault(); first.focus(); }
  }}>
   <div className="runtime-dialog-heading"><h2 id={titleID}>{title}</h2><button className="runtime-close" aria-label="关闭面板" disabled={busy} onClick={onClose}><img src="/icons/xmark.png" alt=""/></button></div>
   {description && <p className="runtime-dialog-description">{description}</p>}
   {children}
  </section>
 </div>;
}
