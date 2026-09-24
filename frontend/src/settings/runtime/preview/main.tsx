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
createRoot(document.getElementById('root')!).render(<main className="settings-window"><aside><nav aria-label="设置分类">{['常规','外观','运行时与模型','权限','存储','诊断','关于'].map(label=><button key={label} aria-current={label==='运行时与模型'?'page':undefined} disabled={label!=='运行时与模型'}>{label}</button>)}</nav><small>Caelis Bot<br/>交互预览 · 示例数据</small></aside><div className="settings-content"><div className="settings-page"><RuntimeWorkspace client={client} preparation={(id,onBusy)=><RuntimePreparation initialRuntime={id} onBusy={onBusy} call={call} host={async()=>undefined as never}/> }/></div></div></main>);
