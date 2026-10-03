import '../../../style.css';
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
createRoot(document.getElementById('root')!).render(<I18nProvider bridge={bridge}><main className="settings-window"><aside><nav aria-label="设置分类">{['General','Appearance','AI & connections','Privacy & permissions','About & help'].map(label=><button key={label} aria-current={label==='AI & connections'?'page':undefined} disabled={label!=='AI & connections'}>{label}</button>)}</nav><small>Caelis Bot<br/>交互预览 · 示例数据</small></aside><div className="settings-content"><div className="settings-page"><RuntimeWorkspace client={client} machines={machinePreview(scenario)} preparation={(id,onBusy)=><RuntimePreparation initialRuntime={id} onBusy={onBusy} call={call} host={async()=>undefined as never}/> }/></div></div></main></I18nProvider>);
