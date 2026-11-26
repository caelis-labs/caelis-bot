import { useEffect, useId, useLayoutEffect, useRef, useState, type PointerEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useI18n } from '../../i18n';
import { XIcon } from '../../SettingsIcons';

function isPointerOutsideDialog(event: PointerEvent<HTMLDialogElement>, dialog: HTMLDialogElement): boolean {
 const rect = dialog.getBoundingClientRect();
 return event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom;
}

export function SettingsDialog({ title, description, eyebrow, closeLabel, busy = false, blocked = false, onClose, children, className = '', anchor, alignTo, returnFocus, portalTarget, surfaceClassName = '' }: {
 title: ReactNode; description?: string; eyebrow?: ReactNode; closeLabel?: string; busy?: boolean; blocked?: boolean; onClose: () => void; children: ReactNode; className?: string; anchor?: HTMLElement | null; alignTo?: HTMLElement | null; returnFocus?: HTMLElement | null; portalTarget?: HTMLElement; surfaceClassName?: string;
}) {
 const { t } = useI18n();
 const titleID = useId();
 const dialogRef = useRef<HTMLDialogElement>(null);
 const restoreFrame = useRef<number | undefined>(undefined);
 const pressedOutside = useRef(false);
 const unmountingRef = useRef(false);
 const onCloseRef = useRef(onClose);
 const busyRef = useRef(busy);
 const blockedRef = useRef(blocked);
 const returnFocusRef = useRef(returnFocus);
 const anchorRef = useRef(anchor);
 onCloseRef.current = onClose;
 busyRef.current = busy;
 blockedRef.current = blocked;
 returnFocusRef.current = returnFocus;
 anchorRef.current = anchor;
 const [position, setPosition] = useState<{ left: number; top: number }>();

 useLayoutEffect(() => {
  if (restoreFrame.current !== undefined) cancelAnimationFrame(restoreFrame.current);
  unmountingRef.current = false;
  const dialog = dialogRef.current;
  const explicitPrior = returnFocusRef.current || anchorRef.current;
  if (dialog && !dialog.open) {
   if (typeof dialog.showModal === 'function') dialog.showModal();
   else dialog.setAttribute('open', '');
  }
  const guard = (event: Event) => event.preventDefault();
  window.addEventListener('settings-navigate', guard);
  return () => {
   unmountingRef.current = true;
   window.removeEventListener('settings-navigate', guard);
   if (dialog) {
    if (typeof dialog.close === 'function') {
     if (dialog.open) dialog.close();
    } else {
     dialog.removeAttribute('open');
    }
   }
   if (explicitPrior) {
    restoreFrame.current = requestAnimationFrame(() => {
     if (explicitPrior.isConnected && !explicitPrior.closest('[inert]')) explicitPrior.focus();
    });
   }
  };
 }, []);

 useLayoutEffect(() => {
  if (!anchor || !dialogRef.current) return;
  const dialog = dialogRef.current;
  const place = () => {
   if (!anchor.isConnected || !dialogRef.current) return;
   const boxRect = dialogRef.current.getBoundingClientRect();
   const width = dialogRef.current.offsetWidth || boxRect.width;
   const height = dialogRef.current.offsetHeight || boxRect.height;
   const trigger = anchor.getBoundingClientRect();
   const alignment = (alignTo || anchor).getBoundingClientRect();
   const left = Math.max(16, Math.min(innerWidth - width - 16, alignment.right - width));
   const below = trigger.bottom + 8;
   const above = trigger.top - height - 8;
   const top = below + height <= innerHeight - 16 ? below : above >= 16 ? above : Math.max(16, innerHeight - height - 16);
   setPosition(old => old?.left === left && old?.top === top ? old : { left, top });
  };
  place();
  const observer = new ResizeObserver(place);
  observer.observe(dialog);
  observer.observe(document.documentElement);
  if (alignTo) observer.observe(alignTo);
  window.addEventListener('resize', place);
  window.addEventListener('scroll', place, true);
  return () => {
   observer.disconnect();
   window.removeEventListener('resize', place);
   window.removeEventListener('scroll', place, true);
  };
 }, [anchor, alignTo]);

 useEffect(() => () => {
  if (restoreFrame.current !== undefined) cancelAnimationFrame(restoreFrame.current);
 }, []);

 const supportsClosedBy = typeof HTMLDialogElement !== 'undefined' && 'closedBy' in HTMLDialogElement.prototype;
 const classes = [
  'setup-dialog',
  'runtime-dialog',
  anchor ? 'runtime-popover-dialog' : '',
  surfaceClassName ? `${surfaceClassName}-dialog` : '',
  className,
 ].filter(Boolean).join(' ');

 return createPortal(
  <dialog
   ref={dialogRef}
   style={anchor ? position : undefined}
   className={classes}
   aria-labelledby={titleID}
   {...({ closedby: busy || blocked ? 'none' : 'any' } as Record<string, string>)}
   onCancel={event => {
    event.preventDefault();
    if (busyRef.current || blockedRef.current || document.querySelector('[data-select-open="true"]')) return;
    onCloseRef.current();
   }}
   onClose={() => {
    if (unmountingRef.current) return;
    const dialog = dialogRef.current;
    if (busyRef.current || blockedRef.current || document.querySelector('[data-select-open="true"]')) {
     if (dialog && !dialog.open && typeof dialog.showModal === 'function') dialog.showModal();
     return;
    }
    onCloseRef.current();
   }}
   onPointerDown={event => {
    if (supportsClosedBy) return;
    pressedOutside.current = isPointerOutsideDialog(event, event.currentTarget);
   }}
   onPointerUp={event => {
    if (supportsClosedBy) return;
    const shouldClose = pressedOutside.current && isPointerOutsideDialog(event, event.currentTarget);
    pressedOutside.current = false;
    if (!shouldClose || busyRef.current || blockedRef.current || document.querySelector('[data-select-open="true"]')) return;
    onCloseRef.current();
   }}
  >
   <div className="runtime-dialog-heading">
    <div className="runtime-dialog-title-group">
     {eyebrow && <small className="runtime-dialog-eyebrow">{eyebrow}</small>}
     <h2 id={titleID}>{title}</h2>
    </div>
    <button type="button" className="runtime-close" aria-label={closeLabel || t('runtime.closeDialog')} disabled={busy} onClick={onClose}>
     <XIcon aria-hidden="true" size={16}/>
    </button>
   </div>
   {description && <p className="runtime-dialog-description">{description}</p>}
   {children}
  </dialog>,
  portalTarget || document.querySelector('.settings-window') || document.body,
 );
}
