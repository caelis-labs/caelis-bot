import '../../../style.css';
import { createRoot } from 'react-dom/client';
import { RuntimePreparation } from '../../../RuntimeSettings';
import { createPreparationPreview } from './preparation';
import { RuntimeWorkspace } from '../RuntimeWorkspace';
import { createPreviewClient } from './client';
import '../runtime.css';

// Dedicated development HTML entry; production builds include only index.html.
if (!import.meta.env.DEV) throw new Error('Settings preview is development-only');
document.body.dataset.surface='settings';
const client=createPreviewClient(), call=createPreparationPreview();
createRoot(document.getElementById('root')!).render(<main className="settings-window"><aside><nav aria-label="设置分类">{['General','Appearance','AI & connections','Privacy & permissions','About & help'].map(label=><button key={label} aria-current={label==='AI & connections'?'page':undefined} disabled={label!=='AI & connections'}>{label}</button>)}</nav><small>Caelis Bot<br/>交互预览 · 示例数据</small></aside><div className="settings-content"><div className="settings-page"><RuntimeWorkspace client={client} preparation={(id,onBusy)=><RuntimePreparation initialRuntime={id} onBusy={onBusy} call={call} host={async()=>undefined as never}/> }/></div></div></main>);
