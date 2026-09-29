import type { ChatActivity } from './chat-presentation';
import { BotAvatar } from './BotAvatar';
import { useI18n } from './i18n';
import type { Activity } from './backend/contract';
import { activityLabel } from './activity-presentation';
import type { PortraitClip } from './avatar-presentation';

export function WorkingMessage({activity,active,tool,clip}:{activity:ChatActivity;active:boolean;tool?:Activity|null;clip:PortraitClip}) {
 const {t} = useI18n();
 const dreaming=activity==='dreaming';
 const moving=active&&activity!=='stopping'&&!dreaming;
 const label=dreaming?t('chat.dreaming'):activity==='stopping'?t('chat.stopping'):activity==='reviewing'?t('chat.reviewing'):activity==='tool'&&tool?activityLabel(tool,t):t('chat.bubbleThinking');
 return <article className={`message-row assistant working-message ${moving?'is-active':''}`}>
  <BotAvatar animate={active&&activity!=='stopping'} clip={clip}/>
  <div className={`message assistant working-indicator ${dreaming?'dream-indicator':''}`} role="status" aria-label={label} title={dreaming?t('chat.dreamHint'):undefined}>
   <>
    {activity!=='stopping'&&!dreaming&&<span className="loading-dots" aria-hidden="true"><i/><i/><i/></span>}
    <span className="working-label" aria-hidden="true">{label}</span>
   </>
  </div>
 </article>;
}
