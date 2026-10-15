import {createContext} from 'react';

// Subpages can use the settings shell's single navigation header.
export const SettingsToolbarContext=createContext<HTMLElement|null>(null);
