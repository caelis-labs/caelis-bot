import { CaretRightIcon } from '@phosphor-icons/react/dist/csr/CaretRight';
export { LightningIcon } from '@phosphor-icons/react/dist/csr/Lightning';
export { CheckIcon } from '@phosphor-icons/react/dist/csr/Check';
export { ArrowClockwiseIcon } from '@phosphor-icons/react/dist/csr/ArrowClockwise';
export { MagnifyingGlassIcon } from '@phosphor-icons/react/dist/csr/MagnifyingGlass';
export { XIcon } from '@phosphor-icons/react/dist/csr/X';
export { DesktopTowerIcon } from '@phosphor-icons/react/dist/csr/DesktopTower';
export { ArrowSquareOutIcon } from '@phosphor-icons/react/dist/csr/ArrowSquareOut';
export function SettingsChevron({ open = false }: { open?: boolean }) {
 return <CaretRightIcon aria-hidden="true" size={16} weight="regular" className={`settings-chevron${open ? ' is-open' : ''}`}/>;
}
