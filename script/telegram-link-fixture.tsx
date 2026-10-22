import {createRoot} from 'react-dom/client';
import {I18nProvider} from '../frontend/src/i18n';
import {TelegramSettings} from '../frontend/src/TelegramSettings';
import '../frontend/src/application.css';
if(!import.meta.env.DEV)throw Error('Development fixture only');
createRoot(document.getElementById('root')!).render(<I18nProvider><main className="settings-window"><div className="settings-content"><TelegramSettings/></div></main></I18nProvider>);
