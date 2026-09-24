import './style.css';
import React, { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { I18nProvider } from './i18n';
import { desktop } from './desktop';
import { Panel, History } from './Panel';
import { Bubble } from './Bubble';
import { Settings } from './Settings';

function Pet() {
  const canvas = useRef<HTMLCanvasElement>(null);
  const [scale, setScale] = useState(window.innerWidth / 180);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    const resize = () => setScale(window.innerWidth / 180);
    window.addEventListener('resize', resize);
    let dispose: (() => void) | undefined, stopped=false;
    void import('./character/pet').then(({mountPet})=>{if(!stopped)dispose=mountPet(canvas.current!,()=>setFailed(true));}).catch(()=>{if(!stopped)setFailed(true);});
    return () => { stopped=true;dispose?.(); window.removeEventListener('resize', resize); };
  }, []);
  return <main className="pet-stage" style={{ transform: `scale(${scale})` }}>
    <canvas ref={canvas} width="180" height="240" aria-hidden="true" />
    <button className="pet-open" tabIndex={-1} onClick={() => void desktop('Activate')} aria-label={failed ? '桌宠加载失败，请从状态栏打开 Caelis Bot' : '单击打开输入框，双击打开聊天窗口；可拖动角色'} />
  </main>;
}
function Prop() {
 const canvas=useRef<HTMLCanvasElement>(null);
 useEffect(()=>{
  let stopped=false,dispose:(()=>void)|undefined;
  void import('./character/plane').then(({mountPlane})=>{if(!stopped)dispose=mountPlane(canvas.current!);}).catch(()=>void desktop('PlaneReady',false));
  return()=>{stopped=true;dispose?.();};
 },[]);
 return <canvas ref={canvas} style={{display:'block',width:'100vw',height:'100vh'}} aria-hidden="true"/>;
}
const surface = new URLSearchParams(location.search).get('surface');
document.body.dataset.surface = surface ?? 'pet';
createRoot(document.getElementById('root')!).render(<I18nProvider>{surface === 'prop' ? <Prop/> : <React.StrictMode>{surface === 'panel' ? <Panel /> : surface === 'history' ? <History /> : surface === 'bubble' ? <Bubble /> : surface === 'settings' ? <Settings /> : <Pet />}</React.StrictMode>}</I18nProvider>);
