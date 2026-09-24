import type { ChatActivity } from './chat-presentation';
import { BotAvatar } from './BotAvatar';
import { useI18n } from './i18n';

export function WorkingMessage({activity,active}:{activity:ChatActivity;active:boolean}) {
 const {t} = useI18n();
 const moving=active&&activity!=='stopping';
 const label=activity==='stopping'?t('chat.stopping'):activity==='reviewing'?t('chat.reviewing'):t('chat.generating');
 return <article className={`message-row assistant working-message ${moving?'is-active':''}`}>
  <BotAvatar animate={moving}/>
  <div className="message assistant working-indicator" role="status" aria-label={label}>
   {activity==='thinking'?<span className="loading-dots" aria-hidden="true"><i/><i/><i/></span>:<>
    {activity==='reviewing'&&<span className="loading-dots" aria-hidden="true"><i/><i/><i/></span>}
    <span className="working-label" aria-hidden="true">{label}</span>
   </>}
  </div>
 </article>;
}
