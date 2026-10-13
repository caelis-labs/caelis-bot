export const settingsSections = ['general', 'appearance', 'connections', 'team', 'chatConnections', 'plugins', 'machines', 'extras', 'permissions', 'updates'] as const;
export type SettingsSection = typeof settingsSections[number] | 'telegram' | 'weixin' | 'setup';

// Native repair links and older callers still use the runtime destination.
export function settingsDestination(value: string): SettingsSection | null {
 const destination = ({runtime:'connections',models:'connections',chat:'chatConnections',capture:'extras',screen:'extras',execution:'connections',storage:'general',diagnostics:'updates'} as Record<string,string>)[value] ?? value;
 return destination === 'setup' || destination === 'telegram' || destination === 'weixin' || settingsSections.some(id => id === destination) ? destination as SettingsSection : null;
}

export const settingsGroups = [
 { title: 'settings.groupEveryday', sections: ['general', 'appearance', 'connections', 'team'] },
 { title: 'settings.groupConnections', sections: ['chatConnections', 'machines', 'plugins'] },
 { title: 'settings.groupSystem', sections: ['extras', 'permissions', 'updates'] },
] as const;
