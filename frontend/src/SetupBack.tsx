import { ArrowLeftIcon } from '@phosphor-icons/react/dist/csr/ArrowLeft';
import { useI18n } from './i18n';

export function SetupBack({onClick,disabled=false}:{onClick:()=>void;disabled?:boolean}) {
 const {t}=useI18n();
 return <button type="button" className="text-action setup-back" disabled={disabled} onClick={onClick}><ArrowLeftIcon size={14} aria-hidden="true"/>{t('settings.setupBack')}</button>;
}
