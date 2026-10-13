import { SlidersHorizontalIcon } from '@phosphor-icons/react/dist/csr/SlidersHorizontal';
import { PaletteIcon } from '@phosphor-icons/react/dist/csr/Palette';
import { PlugsConnectedIcon } from '@phosphor-icons/react/dist/csr/PlugsConnected';
import { PaperPlaneTiltIcon } from '@phosphor-icons/react/dist/csr/PaperPlaneTilt';
import { PuzzlePieceIcon } from '@phosphor-icons/react/dist/csr/PuzzlePiece';
import { DesktopTowerIcon } from '@phosphor-icons/react/dist/csr/DesktopTower';
import { ScanIcon } from '@phosphor-icons/react/dist/csr/Scan';
import { ShieldCheckIcon } from '@phosphor-icons/react/dist/csr/ShieldCheck';
import { InfoIcon } from '@phosphor-icons/react/dist/csr/Info';
import { UsersThreeIcon } from '@phosphor-icons/react/dist/csr/UsersThree';
import type { SettingsSection } from './settings-navigation';
const icons = {general:SlidersHorizontalIcon,appearance:PaletteIcon,connections:PlugsConnectedIcon,team:UsersThreeIcon,chatConnections:PaperPlaneTiltIcon,plugins:PuzzlePieceIcon,machines:DesktopTowerIcon,extras:ScanIcon,permissions:ShieldCheckIcon,updates:InfoIcon};
export function SettingsNavIcon({section}:{section:SettingsSection}) {
 const Icon = icons[section as keyof typeof icons];
 return Icon ? <Icon size={18} aria-hidden="true"/> : null;
}
