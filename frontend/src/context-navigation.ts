import {isWebSurface} from './transport.ts';

// Shared navigation guard. Native buttons dispatch into their owning retained
// renderer; they never close a window or decide an outstanding interaction.
export function canReturnToChat(setup:boolean, visible:boolean, modal:boolean, allow:()=>boolean):boolean {
 return visible && !setup && !modal && allow();
}
export function settingsHasModal(root:ParentNode=document):boolean {
 return [...root.querySelectorAll<HTMLElement>('[role="dialog"]:not([data-settings-panel]),[role="alertdialog"],[data-select-open="true"],[data-settings-dirty="true"]')].some(node=>node.getClientRects().length>0);
}
export function requestChatReturn(setup:boolean,visible:boolean,open:()=>void) {
 if(canReturnToChat(setup,visible,settingsHasModal(),()=>window.dispatchEvent(new Event('settings-navigate',{cancelable:true}))))open();
}
export const nativeContextChrome=()=>!isWebSurface()&&new URLSearchParams(location.search).get('chrome')==='native';
