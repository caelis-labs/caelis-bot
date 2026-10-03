import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useI18n } from '../../i18n';
import { XIcon } from '../../SettingsIcons';

export function SettingsDialog({ title, description, busy = false, blocked = false, onClose, children, className = '', anchor, alignTo, returnFocus }: {
 title: string; description?: string; busy?: boolean; blocked?: boolean; onClose: () => void; children: ReactNode; className?: string; anchor?: HTMLElement | null; alignTo?: HTMLElement | null; returnFocus?: HTMLElement | null;
}) {
 const { t } = useI18n();
 const titleID = useId(), ref = useRef<HTMLElement>(null), layer = useRef<HTMLDivElement>(null);
 const [position, setPosition] = useState<{ left: number; top: number }>();
 useEffect(() => {
  if (blocked) return;
  const prior = returnFocus || anchor || document.activeElement as HTMLElement | null;
  // Dialogs are sibling portals. Each layer restores the exact previous inert
  // state, so closing a nested picker cannot unlock its parent's background.
  const siblings = [...layer.current!.parentElement!.children].filter(node => node !== layer.current) as HTMLElement[];
  const previous = siblings.map(node => node.inert);
  siblings.forEach(node => { node.inert = true; });
  ref.current?.focus();
  const guard = (event: Event) => event.preventDefault();
  window.addEventListener('settings-navigate', guard);
  return () => {
   siblings.forEach((node, index) => { node.inert = previous[index]; });
   window.removeEventListener('settings-navigate', guard);
   requestAnimationFrame(() => { if (prior?.isConnected && !prior.closest('[inert]')) prior.focus(); });
  };
 }, [blocked, anchor, returnFocus]);
 useLayoutEffect(() => {
  if (!anchor || !ref.current) return;
  const place = () => {
   if (!anchor.isConnected || !ref.current) return;
   const box = ref.current.getBoundingClientRect(), trigger = anchor.getBoundingClientRect(), alignment = (alignTo || anchor).getBoundingClientRect();
   const left = Math.max(16, Math.min(innerWidth - box.width - 16, alignment.right - box.width));
   const below = trigger.bottom + 8, above = trigger.top - box.height - 8;
   const top = below + box.height <= innerHeight - 16 ? below : above >= 16 ? above : Math.max(16, innerHeight - box.height - 16);
   setPosition(old => old?.left === left && old?.top === top ? old : { left, top });
  };
  place();
  const observer = new ResizeObserver(place);
  observer.observe(ref.current); observer.observe(document.documentElement);
  if (alignTo) observer.observe(alignTo);
  window.addEventListener('resize', place); window.addEventListener('scroll', place, true);
  return () => { observer.disconnect(); window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true); };
 }, [anchor, alignTo]);
 return createPortal(<div ref={layer} className={`setup-scrim runtime-dialog-scrim${anchor ? ' runtime-popover-scrim' : ''}`} onPointerDown={event => { if (event.target === event.currentTarget && !busy) onClose(); }}>
  <section ref={ref} style={anchor ? position : undefined} tabIndex={-1} className={`setup-dialog runtime-dialog ${className}`} role="dialog" aria-modal="true" aria-labelledby={titleID} onKeyDown={event => {
   if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); if (!busy) onClose(); }
   if (event.key !== 'Tab') return;
   event.stopPropagation();
   const nodes = [...ref.current!.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),textarea:not(:disabled),summary,a[href]')].filter(node => node.getClientRects().length && !node.closest('[inert]'));
   const first = nodes[0], last = nodes.at(-1);
   if (!first) { event.preventDefault(); return; }
   if (event.shiftKey && (document.activeElement === first || document.activeElement === ref.current)) { event.preventDefault(); last?.focus(); }
   else if (!event.shiftKey && (document.activeElement === last || document.activeElement === ref.current)) { event.preventDefault(); first.focus(); }
  }}>
   <div className="runtime-dialog-heading"><h2 id={titleID}>{title}</h2><button className="runtime-close" aria-label={t('runtime.closeDialog')} disabled={busy} onClick={onClose}><XIcon aria-hidden="true" size={16}/></button></div>
   {description && <p className="runtime-dialog-description">{description}</p>}
   {children}
  </section>
 </div>, document.querySelector('.settings-window') || document.body);
}
