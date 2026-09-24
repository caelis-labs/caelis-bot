import '../../../style.css';
import { createRoot } from 'react-dom/client';
import { RuntimeWorkspace } from '../RuntimeWorkspace';
import { createPreviewClient } from './client';
import '../runtime.css';

// Dedicated development HTML entry; production builds include only index.html.
if (!import.meta.env.DEV) throw new Error('Settings preview is development-only');
document.body.dataset.surface='settings';
const client=createPreviewClient();
createRoot(document.getElementById('root')!).render(<main className="settings-window"><aside><nav aria-label="设置分类">{['常规','外观','运行时与模型','权限','存储','诊断','关于'].map(label=><button key={label} aria-current={label==='运行时与模型'?'page':undefined} disabled={label!=='运行时与模型'}>{label}</button>)}</nav><small>Caelis Bot<br/>交互预览 · 示例数据</small></aside><div className="settings-content"><div className="settings-page"><RuntimeWorkspace client={client} preparation={id=><p className="settings-note">{id} 安装和切换使用现有 Bot 原生设置流程。此预览不会启动或重启程序。</p>}/></div></div></main>);
