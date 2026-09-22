import type { ChatActivity } from './chat-presentation';

export function WorkingMessage({activity,active}:{activity:ChatActivity;active:boolean}) {
 const moving=active&&activity!=='stopping';
 const label=activity==='stopping'?'正在停止':activity==='reviewing'?'正在自动审查':'Caelis Bot 正在回复';
 return <article className={`message-row assistant working-message ${moving?'is-active':''}`}>
  <img className="bot-avatar working-avatar" src="/icons/caelis-avatar.png" alt="Caelis Bot" width="32" height="32" draggable={false}/>
  <div className="message assistant working-indicator" role="status" aria-label={label}>
   {activity==='thinking'?<span className="loading-dots" aria-hidden="true"><i/><i/><i/></span>:<>
    {activity==='reviewing'&&<span className="loading-dots" aria-hidden="true"><i/><i/><i/></span>}
    <span className="working-label" aria-hidden="true">{label}</span>
   </>}
  </div>
 </article>;
}
