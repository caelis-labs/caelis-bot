import {History} from './Panel';
import {ChatSettingsButton} from './ChatSettingsButton';
export function ContextWindow(){
 return <div className="context-window" data-view="chat"><div className="context-view chat-view"><History settingsEntry={<ChatSettingsButton/>}/></div></div>;
}
