import { createRoot } from 'react-dom/client';
import { BotSetup } from '../../frontend/src/BotSetup';
import { RuntimePreparation } from '../../frontend/src/RuntimeSettings';
import { backend, desktop } from '../../frontend/src/desktop';

const params = new URLSearchParams(location.search);
const settings = params.get('scene') === 'settings';
const runtime = params.get('runtime') || 'caelis';
createRoot(document.getElementById('root')!).render(settings
 ? <RuntimePreparation manageOnly initialRuntime={runtime} call={backend} host={desktop} onDone={() => { document.body.dataset.settingsDone = 'true'; }}/>
 : <BotSetup onDone={() => { document.body.dataset.introductionDone = 'true'; }}/>);
