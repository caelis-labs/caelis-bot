import type { ReactNode } from 'react';
import { SettingGroup, SettingRow } from './SettingsUI';
import { useI18n } from './i18n';

export type UpdateInlineProgress = {
 label: string;
 detail: string;
 value?: number;
};

export function UpdateSettingsShell({
 version,
 badge,
 status,
 progress,
 actions,
}: {
 version: string;
 badge?: string;
 status: ReactNode;
 progress?: UpdateInlineProgress;
 actions: ReactNode;
}) {
 const { t } = useI18n();
 return (
  <section className="update-settings">
   <h1>{t('settings.updates')}</h1>
   <div className="about-identity">
    <img src="/icons/caelis-avatar.png" alt="" width="56" height="56"/>
    <div>
     <h2>Caelis Bot</h2>
     <p>{version}{badge ? ` · ${badge}` : ''}</p>
    </div>
   </div>
   <SettingGroup>
    <SettingRow
     label={t('settings.softwareUpdate')}
     description={
      <>
       <span role="status">{status}</span>
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
