import {NodeRuntimeSettings} from './NodeRuntimeSettings';
import {RuntimePreparation} from './RuntimePreparation';
export {RuntimePreparation} from './RuntimePreparation';

export function RuntimeSettings({ onboarding = false, onDone,active=true,refreshKey=0 }: { onboarding?: boolean; onDone?: () => void; active?:boolean; refreshKey?:number }) {
 return onboarding ? <RuntimePreparation onboarding onDone={onDone}/> : <NodeRuntimeSettings active={active} refreshKey={refreshKey}/>;
}
