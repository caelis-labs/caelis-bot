import type { ButtonHTMLAttributes } from 'react';
import { useI18n } from './i18n';

// Discover page contents centrally; apply a choice in its initiating button.
export function SetupLoading() {
 const {t}=useI18n();
 return <div className="setup-loading" role="status" aria-live="polite" aria-busy="true">
  <span className="setup-spinner" aria-hidden="true"/>
  <span>{t('settings.setupLoading')}</span>
 </div>;
}

export function SetupButton({busy=false,children,disabled,className='',...props}:ButtonHTMLAttributes<HTMLButtonElement>&{busy?:boolean}) {
 return <button {...props} className={`setup-button ${className}`.trim()} disabled={disabled||busy} aria-busy={busy||undefined}>
  <span className="setup-button-label">{children}</span>
  {busy&&<span className="setup-spinner" aria-hidden="true"/>}
 </button>;
}
