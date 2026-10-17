import {useEffect,useState} from 'react';
import type {Snapshot} from './backend/contract';
import {connectionNoticeGrace,connectionNoticeKey} from './connection-notice';

export function useConnectionNotice(snapshot:Snapshot|null) {
 const key=connectionNoticeKey(snapshot);
 const delay=connectionNoticeGrace(snapshot,document.documentElement.dataset.platform==='windows');
 const [settled,setSettled]=useState('');
 useEffect(()=>{
  setSettled('');
  if(!key||!delay)return;
  const timer=window.setTimeout(()=>setSettled(key),delay);
  return()=>window.clearTimeout(timer);
 },[key,delay]);
 return !!key&&(!delay||settled===key);
}
