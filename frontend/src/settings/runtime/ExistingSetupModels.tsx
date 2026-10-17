import type { SetupState } from '../../backend/contract';
import type { ReactNode } from 'react';
import { SettingsSelect } from '../../SettingsSelect';
import { useI18n } from '../../i18n';

// Reuse an installed Host's models without recreating its provider connection.
export function ExistingSetupModels({models, selectedModel, disabled, onSelect, onContinue, children}: {
 models: SetupState['models']; selectedModel: string; disabled: boolean;
 onSelect: (model: string) => void; onContinue: () => void;
 children?: ReactNode;
}) {
 const {t}=useI18n();
 const available=models.filter(m=>!m.noAuth);
 return <>
  <div className="onboarding-model-choice">
   <span className="onboarding-model-label">{t('runtime.botUseModel')}</span>
   <SettingsSelect aria-label={t('runtime.botUseModel')} value={selectedModel} disabled={disabled} onChange={e=>onSelect(e.target.value)}>
    <option value="" disabled>{t('runtime.selectModel')}</option>
    {available.map(m=><option key={m.value} value={m.value}>{m.label}</option>)}
   </SettingsSelect>
  </div>
  {children}
  <div className="setup-end"><button className="primary" disabled={disabled||!available.some(m=>m.value===selectedModel)} onClick={onContinue}>{t('settings.permissionContinue')}</button></div>
 </>;
}
