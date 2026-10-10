import {useEffect, useRef, useState} from 'react';
import {desktop} from './desktop';
import {useI18n} from './i18n';
import {SettingGroup, SettingRow} from './SettingsUI';

export type LoginAtLoginStatus = {
 supported:boolean;
 state:'on'|'off'|'needsApproval'|'unavailable'|'unsupported';
 enabled:boolean;
 registered:boolean;
};

export function LoginAtLoginSetting({active,focusVersion}:{active:boolean;focusVersion:number}) {
 const {t}=useI18n();
 const [status,setStatus]=useState<LoginAtLoginStatus|null>(null);
 const [busy,setBusy]=useState(false),[error,setError]=useState('');
 const epoch=useRef(0),changing=useRef(false),errorKind=useRef<'load'|'action'|''>('');
 const mounted=useRef(true),wasActive=useRef(false),lastRead=useRef(0);
 useEffect(()=>{
  mounted.current=true;
  return()=>{mounted.current=false;epoch.current++;};
 },[]);
 useEffect(()=>{
  if(!active){wasActive.current=false;epoch.current++;return;}
  const opening=!wasActive.current;
  wasActive.current=true;
  // AppKit may report focus immediately after opening the window. Coalesce
  // that event with the open read, while still refreshing on later returns.
  if(!opening&&Date.now()-lastRead.current<1000)return;
  lastRead.current=Date.now();
  const refresh=async()=>{
   if(changing.current)return;
   const current=epoch.current;
   try{
    const value=await desktop<LoginAtLoginStatus>('LoginAtLoginStatus');
    if(mounted.current&&active&&current===epoch.current){setStatus(value);if(errorKind.current==='load'){errorKind.current='';setError('');}}
   }catch{
    if(mounted.current&&active&&current===epoch.current&&errorKind.current!=='action'){errorKind.current='load';setError(t('settings.loginAtLoginLoadFailed'));}
   }
  };
  void refresh();
 },[active,focusVersion,t]);
 const change=async(enabled:boolean)=>{
  if(changing.current)return;
  changing.current=true;epoch.current++;setBusy(true);errorKind.current='';setError('');
  try{setStatus(await desktop<LoginAtLoginStatus>('SetLoginAtLogin',enabled));}
  catch{
   errorKind.current='action';setError(t('settings.loginAtLoginChangeFailed'));
   try{setStatus(await desktop<LoginAtLoginStatus>('LoginAtLoginStatus'));}catch{/* Keep the last known state. */}
  }finally{changing.current=false;setBusy(false);}
 };
 const open=async()=>{
  errorKind.current='';setError('');
  try{await desktop('OpenLoginItemsSettings');}
  catch{errorKind.current='action';setError(t('settings.loginAtLoginOpenFailed'));}
 };
 const retry=async()=>{
  epoch.current++;errorKind.current='';setError('');
  try{setStatus(await desktop<LoginAtLoginStatus>('LoginAtLoginStatus'));}
  catch{errorKind.current='load';setError(t('settings.loginAtLoginLoadFailed'));}
 };
 const state=status?.state;
 const message=error||(!status?t('common.loading'):state==='on'?t('settings.loginAtLoginOn'):state==='off'?t('settings.loginAtLoginOff'):state==='needsApproval'?t('settings.loginAtLoginNeedsApproval'):state==='unavailable'?t('settings.loginAtLoginUnavailable'):t('settings.loginAtLoginUnsupported'));
 return <SettingGroup><SettingRow label={t('settings.loginAtLoginLabel')} htmlFor={status?.supported?'login-at-login':undefined} description={<span role={error?'alert':'status'}>{message}</span>}>
  {status?.supported&&state!=='unavailable'?<><input id="login-at-login" className="settings-switch" role="switch" type="checkbox" checked={status.enabled} disabled={busy} onChange={event=>void change(event.target.checked)}/>
   {state==='needsApproval'&&<><button className="text-action" disabled={busy} onClick={()=>void open()}>{t('settings.openSystemSettings')}</button><button className="text-action" disabled={busy} onClick={()=>void change(false)}>{t('settings.loginAtLoginRemove')}</button></>}</>:state==='unavailable'||!status&&!!error?<button disabled={busy} onClick={()=>void retry()}>{t('settings.retry')}</button>:null}
 </SettingRow></SettingGroup>;
}
