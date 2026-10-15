import { History } from './Panel';
import { desktop } from './desktop';
import { useI18n } from './i18n';
import { GearSixIcon } from '@phosphor-icons/react/dist/csr/GearSix';
export function ContextWindow() {
 const {t}=useI18n();
 return <div className="context-window" data-view="chat"><div className="context-view chat-view"><header className="context-chat-toolbar"><button aria-label={t('native.settings')} onClick={()=>void desktop('OpenSettings')}><GearSixIcon aria-hidden="true" size={18}/></button></header><History/></div></div>;
}
