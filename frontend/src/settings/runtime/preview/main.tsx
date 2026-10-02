import '../../../style.css';
import { createRoot } from 'react-dom/client';
import { RuntimePreparation } from '../../../RuntimeSettings';
import { createPreparationPreview } from './preparation';
import { RuntimeWorkspace } from '../RuntimeWorkspace';
import { createPreviewClient } from './client';
import '../runtime.css';
import {WorkerNodeSettings} from '../../../WorkerNodeSettings';
import {NotebookSyncSettings} from '../NotebookSyncSettings';
import {BatchSettings} from '../BatchSettings';
import {createNodeSettingsClient} from '../nodeClient';
import {createNodePreview} from './nodes';

// Dedicated development HTML entry; production builds include only index.html.
if (!import.meta.env.DEV) throw new Error('Settings preview is development-only');
document.body.dataset.surface='settings';
const client=createPreviewClient(), call=createPreparationPreview();
const nodes=new URLSearchParams(location.search).has('nodes')?createNodePreview(client):undefined;
const owner=nodes&&createNodeSettingsClient(nodes.call);
createRoot(document.getElementById('root')!).render(<main className="settings-window"><aside><nav aria-label="设置分类">{['General','Appearance','AI & connections','Privacy & permissions','About & help'].map(label=><button key={label} aria-current={label==='AI & connections'?'page':undefined} disabled={label!=='AI & connections'}>{label}</button>)}</nav><small>Caelis Bot<br/>交互预览 · 示例数据</small></aside><div className="settings-content"><div className="settings-page"><RuntimeWorkspace batch={nodes&&owner?(view,connection,onClose)=><BatchSettings catalog={nodes.catalog} owner={owner} sourceId="local" view={view} connection={connection} onClose={onClose} onSelect={()=>{}}/>:undefined} client={client} preparation={(id,onBusy)=><RuntimePreparation initialRuntime={id} onBusy={onBusy} call={call} host={async()=>undefined as never}/> }/>{nodes&&<><WorkerNodeSettings catalog={nodes.catalog} call={nodes.call}/><NotebookSyncSettings catalog={nodes.catalog} call={nodes.call}/></>}</div></div></main>);
