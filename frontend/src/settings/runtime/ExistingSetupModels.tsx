import type { SetupState } from '../../backend/contract';
import { SetupButton } from '../../SetupLoading';
import { SettingsSelect } from '../../SettingsSelect';
import { useI18n } from '../../i18n';

// Both runtimes confirm a model after installation and authentication.
export function ExistingSetupModels({models, selectedModel, disabled, busy=false, onSelect, onContinue}: {
 models: SetupState['models']; selectedModel: string; disabled: boolean; busy?:boolean;
 onSelect: (model: string) => void; onContinue: () => void;
}) {
 const {t}=useI18n();
 const available=models.filter(m=>!m.noAuth);
 return <>
  <div className="onboarding-model-choice">
   <span className="onboarding-model-label">{t('runtime.botUseModel')}</span>
   <SettingsSelect aria-label={t('runtime.botUseModel')} placeholder={t('runtime.selectModel')} value={selectedModel} disabled={disabled||!available.length} onChange={e=>onSelect(e.target.value)}>
    {available.map(m=><option key={m.value} value={m.value}>{m.label}</option>)}
   </SettingsSelect>
  </div>
  {!available.length&&<p className="settings-note" role="status">{t('runtime.noModelsFound')}</p>}
  <div className="setup-end"><SetupButton className="primary" busy={busy} disabled={disabled||!available.some(m=>m.value===selectedModel)} onClick={onContinue}>{t('settings.setupNext')}</SetupButton></div>
 </>;
}
