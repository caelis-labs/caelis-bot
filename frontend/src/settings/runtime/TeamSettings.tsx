import {useRef,useState} from 'react';
import type {ModelOption} from '../../backend/contract';
import type {RuntimeSettingsClient,TeamRole,TeamState} from './types';
import {ModelPicker} from './ModelPicker';
import {SettingsPage} from './SettingsPage';
import {SettingsDialog} from './SettingsDialog';
import {useI18n} from '../../i18n';
import {SettingsChevron} from '../../SettingsIcons';
const roles=['breeze','orbit','zenith','tool-search','guardian'] as const;
export function TeamSettings({team,models,client,onRefresh,inline=false,onBack=()=>{},hideHeader=false}:{team:TeamState;models:ModelOption[];client:RuntimeSettingsClient;onRefresh:()=>Promise<void>;inline?:boolean;onBack?:()=>void;hideHeader?:boolean}){
 const {t}=useI18n(),anchor=useRef<HTMLElement|null>(null);
 const [expanded,setExpanded]=useState(false),[editing,setEditing]=useState<{role:TeamRole;label:typeof roles[number]|'guardian-screen'}|null>(null);
 const Surface=inline?SettingsPage:SettingsDialog;
 return <section className="runtime-team">
  {!inline&&<button className="runtime-team-entry" onClick={()=>setExpanded(true)}>{t('runtime.teamSettings')}<SettingsChevron/></button>}
  {(inline||expanded)&&<Surface hideHeader={hideHeader} title={t('runtime.teamSettings')} className="runtime-team-dialog" blocked={!!editing} onClose={()=>inline?onBack():setExpanded(false)}>
   {!team.available?<p className="settings-note">{team.reason}</p>:<div className="team-agent-cards">{roles.map(id=>{
    const role=team.roles.find(r=>r.id===id);
    const classifier=id==='guardian'?team.roles.find(r=>r.id==='guardian-screen'):undefined;
    const classifierModel=models.find(m=>m.model===classifier?.selection.model);
    const model=models.find(m=>m.model===role?.selection.model);
    return <div key={id} className="team-agent-tile"><button className="team-agent-card" disabled={!role} onClick={e=>{if(role){anchor.current=e.currentTarget;setEditing({role,label:id})}}}>
     <strong>{id}</strong><p>{t(`settings.agent_${id==='tool-search'?'toolSearch':id}`)}</p><span className="team-agent-model">{model?.name||role?.selection.model||t('runtime.default')}<SettingsChevron/></span>
     {role?.problem&&<small className="inline-error">{role.problem}</small>}
    </button>{classifier&&<button className="team-classifier-entry" onClick={e=>{anchor.current=e.currentTarget;setEditing({role:classifier,label:'guardian-screen'})}}><span>{t('settings.agentClassifier')}</span><span>{classifierModel?.name||classifier.selection.model||t('settings.agentClassifierOptional')}<SettingsChevron/></span></button>}</div>;
   })}</div>}
  </Surface>}
  {editing&&<ModelPicker key={editing.role.id} selectInitialModel emptyMessage={editing.label==='guardian-screen'?t('settings.agentClassifierMissing'):undefined} anchor={anchor.current} title={editing.label==='guardian-screen'?t('settings.agentClassifier'):editing.label} description={t(`settings.agent_${editing.label==='tool-search'?'toolSearch':editing.label==='guardian-screen'?'guardianScreen':editing.label}`)} value={editing.role.selection} models={models.filter(m=>editing.role.modelIds?.includes(m.model))} onClose={()=>setEditing(null)} onReload={onRefresh} onSave={async selection=>{await client.changeTeam({action:'bind',id:editing.role.id,selection},team.revision);setEditing(null);await onRefresh()}} onReset={!editing.role.inherited?async()=>{await client.changeTeam({action:'reset',id:editing.role.id},team.revision);setEditing(null);await onRefresh()}:undefined}/>}
 </section>;
}
