export const settingsSections = ['general', 'appearance', 'models', 'connections', 'chatConnections', 'machines', 'extras', 'permissions', 'updates'] as const;
export type SettingsSection = typeof settingsSections[number] | 'telegram' | 'setup';

// Native repair links and older callers still use the runtime destination.
export function settingsDestination(value: string): SettingsSection | null {
 const destination = ({runtime:'connections',chat:'chatConnections',capture:'extras',screen:'extras',execution:'permissions',storage:'general',diagnostics:'updates'} as Record<string,string>)[value] ?? value;
 return destination === 'setup' || destination === 'telegram' || settingsSections.some(id => id === destination) ? destination as SettingsSection : null;
}
