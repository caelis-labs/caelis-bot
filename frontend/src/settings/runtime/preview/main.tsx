import '../../../style.css';
import { useState } from 'react';
import { useI18n } from '../../../i18n';
import { createRoot } from 'react-dom/client';
import { RuntimePreparation } from '../../../RuntimeSettings';
import { createPreparationPreview } from './preparation';
import { RuntimeWorkspace } from '../RuntimeWorkspace';
import { createPreviewClient } from './client';
import '../runtime.css';
import '../../settings.css';
import { machinePreview } from './machines';
import { I18nProvider, type LanguageBridge } from '../../../i18n';

// Dedicated development HTML entry; production builds include only index.html.
if (!import.meta.env.DEV) throw new Error('Settings preview is development-only');
document.body.dataset.surface='settings';
const client=createPreviewClient(), call=createPreparationPreview();
const params=new URLSearchParams(location.search), scenario=params.get('machine')??'login', locale=params.get('lang')==='zh-CN'?'zh-CN':'en';
const bridge:LanguageBridge={read:async()=>({preference:locale,locale,revision:1}),save:async preference=>({preference,locale,revision:2}),subscribe:()=>()=>{}};
function Preview() {
 const {t}=useI18n();
 const [page,setPage]=useState<'models'|'connections'|'machines'>('connections');
 const navigate=(next:typeof page)=>{if(window.dispatchEvent(new Event('settings-navigate',{cancelable:true})))setPage(next);};
 return <main className="settings-window"><aside><nav aria-label={t('settings.navLabel')}>{['models','connections','machines'].map(id=><button key={id} aria-current={page===id?'page':undefined} onClick={()=>navigate(id as typeof page)}>{t(`settings.${id}` as Parameters<typeof t>[0])}</button>)}</nav><small>Caelis Bot<br/>Preview · example data</small></aside><div className="settings-content"><div className="settings-page" hidden={page==='machines'}><RuntimeWorkspace page={page==='models'?'models':'connections'} active={page!=='machines'} onConnections={()=>setPage('connections')} client={client} preparation={(id,onBusy)=><RuntimePreparation initialRuntime={id} onBusy={onBusy} call={call} host={async()=>undefined as never}/>}/></div><div className="settings-page" hidden={page!=='machines'}>{page==='machines'&&machinePreview(scenario)}</div></div></main>;
}
createRoot(document.getElementById('root')!).render(<I18nProvider bridge={bridge}><Preview/></I18nProvider>);
