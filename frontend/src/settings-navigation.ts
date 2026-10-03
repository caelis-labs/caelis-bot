export const settingsSections = ['general', 'appearance', 'models', 'connections', 'machines', 'permissions', 'updates'] as const;
export type SettingsSection = typeof settingsSections[number] | 'setup';

// Native repair links and older callers still use the runtime destination.
export function settingsDestination(value: string): SettingsSection | null {
 const destination = ({runtime:'connections', execution:'permissions', storage:'general', diagnostics:'updates'} as Record<string,string>)[value] ?? value;
 return destination === 'setup' || settingsSections.some(id => id === destination) ? destination as SettingsSection : null;
}
