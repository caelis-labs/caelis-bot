import type { ReactNode } from 'react';
import { SettingGroup, SettingRow } from './SettingsUI';
import { useI18n } from './i18n';

export type UpdateInlineProgress = {
 label: string;
 detail: string;
 value?: number;
};

export type UpdateAutomaticToggle = {
 id: string;
 label: string;
 description?: string;
 checked: boolean;
 disabled?: boolean;
 onChange: (checked: boolean) => void;
};

export function UpdateSettingsShell({
 badge,
 status,
 progress,
 actions,
 automatic,
}: {
 badge?: string;
 status: ReactNode;
 progress?: UpdateInlineProgress;
 actions: ReactNode;
 automatic?: UpdateAutomaticToggle;
}) {
 const { t } = useI18n();
 return (
  <section className="update-settings">
   <h1>{t('settings.updates')}</h1>
   <SettingGroup>
    {automatic && (
     <SettingRow label={automatic.label} htmlFor={automatic.id} description={automatic.description}>
      <input
       id={automatic.id}
       className="settings-switch"
       role="switch"
       type="checkbox"
       checked={automatic.checked}
       disabled={automatic.disabled}
       onChange={event => automatic.onChange(event.target.checked)}
      />
     </SettingRow>
    )}
    <SettingRow
     label={t('settings.softwareUpdate')}
     description={
      <>
       <span className="update-status-line">
        {badge && <span className="update-badge">{badge}</span>}
        <span role="status">{status}</span>
       </span>
       {progress && (
        <span className="update-inline-progress">
         <progress aria-label={progress.label} max={100} value={progress.value}/>
         <span>{progress.detail}</span>
        </span>
       )}
      </>
     }
    >
     {actions}
    </SettingRow>
   </SettingGroup>
  </section>
 );
}
