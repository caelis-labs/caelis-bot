import {createRoot} from 'react-dom/client';
import {useEffect,useState} from 'react';
import {StreamingFixture} from './completed-stream-fixture';
import {I18nProvider} from '../frontend/src/i18n';
import {ContextWindow} from '../frontend/src/ContextWindow';
import {ConnectionWizard} from '../frontend/src/settings/runtime/ConnectionWizard';
import {ModelPicker} from '../frontend/src/settings/runtime/ModelPicker';
import {readComposer,writeComposer,composerSelection} from '../frontend/src/composer-document';
import type {ModelOption} from '../frontend/src/backend/contract';
import type {RuntimeSettingsClient} from '../frontend/src/settings/runtime/types';
import '../frontend/src/application.css';
if(!import.meta.env.DEV)throw Error('Development fixture only');
const w=window as any;
const params=new URLSearchParams(location.search),view=params.get('view')||'chat';
if(params.has('fallback'))for(const method of ['showPopover','hidePopover','togglePopover'])Object.defineProperty(HTMLElement.prototype,method,{value:undefined,configurable:true});
document.body.dataset.surface=view==='chat'||view==='streaming'?'context':'settings';
const root=createRoot(document.getElementById('root')!);
const bridge={read:async()=>({preference:'zh-CN' as const,locale:'zh-CN' as const,revision:1}),save:async()=>({preference:'zh-CN' as const,locale:'zh-CN' as const,revision:1}),subscribe:()=>()=>{}};
const models:ModelOption[]=['deepseek-flash','deepseek-v4-pro'].map((model,i)=>({model,name:i?'DeepSeek v4-pro':'DeepSeek flash',efforts:['none','low','high','max'],defaultEffort:'high',serviceTiers:[],default:!i,imageInput:false,description:''}));
let mutations=0,closed=0;
const client={catalog:async()=>({choices:[{id:'deepseek',name:'DeepSeek',custom:false}],unavailable:''}),apiKeyOptions:async()=>({endpoints:[{value:'https://api.deepseek.com/v1',name:'DeepSeek'}],models:models.map(m=>({value:m.model,name:m.name,parameters:m}))}),startConnection:async()=>{mutations++;throw Error('No connection authority in fixture');}} as unknown as RuntimeSettingsClient;
function SettingsFixture(){
 const [mounted,setMounted]=useState(false);
 useEffect(()=>setMounted(true),[]);
 return <main className="settings-window setup-window">{view==='onboarding'?<div className="setup-page"><div className="setup-flow"><ConnectionWizard inline onboarding initialChoice="deepseek" client={client} onClose={()=>closed++} onConnected={async()=>{mutations++}}/></div></div>:mounted&&<ModelPicker title="选择模型" models={models} value={{model:models[0].model,effort:'high',serviceTier:''}} onSave={async()=>{mutations++}} onClose={()=>closed++}/>}</main>;
}
root.render(<I18nProvider bridge={bridge}>{view==='streaming'?<StreamingFixture/>:view==='chat'?<ContextWindow/>:<SettingsFixture/>}</I18nProvider>);
const delay=(ms=0)=>new Promise(r=>setTimeout(r,ms));
const check=(ok:unknown,message:string)=>{if(!ok)throw Error(message)};
const wait=async(test:()=>boolean)=>{const end=Date.now()+6000;while(!test()){if(Date.now()>end)throw Error('fixture timeout');await delay(20)}};
const placeholder=(el:HTMLElement)=>!['none','normal','""'].includes(getComputedStyle(el,'::before').content);
const input=(el:HTMLElement)=>el.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertCompositionText',isComposing:true}));
const compose=(el:HTMLElement,type:string,data='')=>el.dispatchEvent(new CompositionEvent(type,{bubbles:true,data}));
const key=(el:HTMLElement,key:string,keyCode=0,isComposing=false)=>{const e=new KeyboardEvent('keydown',{key,keyCode,isComposing,bubbles:true,cancelable:true});el.dispatchEvent(e);return e;};
w.fixtureUIState=()=>({mutations,closed,view,preedit:document.querySelector('.composer-editor')?.textContent,placeholder:document.querySelector('.composer-editor')?placeholder(document.querySelector('.composer-editor')!):undefined,payloads:w.fixturePayloads});
w.runCompositionRegression=async()=>{
 await wait(()=>document.querySelector<HTMLElement>('.composer-editor')?.contentEditable==='true');
 const el=document.querySelector<HTMLElement>('.composer-editor')!;
 const save=()=>w.fixturePayloads?.filter((p:any)=>p.method==='SaveDraft')??[];
 const submit=()=>w.fixturePayloads?.filter((p:any)=>p.method==='Submit')??[];
 const set=(text:string)=>{el.textContent=text;el.dispatchEvent(new InputEvent('input',{bubbles:true,inputType:'insertText'}));};
 el.focus();set('');await wait(()=>save().at(-1)?.args[0].text==='');check(placeholder(el),'empty placeholder missing');
 const saves=save().length;
 compose(el,'compositionstart');check(!placeholder(el),'placeholder visible at compositionstart');
 el.append(document.createTextNode('con'));const marked=el.firstChild!;
 const range=document.createRange();range.setStart(marked,3);range.collapse(true);getSelection()!.removeAllRanges();getSelection()!.addRange(range);
 compose(el,'compositionupdate','con');input(el);
 check(!placeholder(el),'placeholder overlaps preedit');check(el.firstChild===marked&&composerSelection(el)?.startOffset===3,'preedit DOM/caret replaced');
 check(save().length===saves,'preedit persisted');
 check(!key(el,'Enter',13,false).defaultPrevented,'composition Enter intercepted');check(submit().length===0,'composition Enter sent');
 el.textContent='从';input(el);compose(el,'compositionend','从');await wait(()=>save().at(-1)?.args[0].text==='从');
 check(!placeholder(el),'committed placeholder visible');key(el,'Enter',229,false);check(submit().length===0,'WebKit boundary Enter sent');
 set('');compose(el,'compositionstart');el.textContent='con';compose(el,'compositionupdate','con');input(el);el.replaceChildren(document.createElement('br'));input(el);compose(el,'compositionend');
 await wait(()=>save().at(-1)?.args[0].text==='');check(placeholder(el),'cancelled composition placeholder missing');
 // Preserve exact whitespace, Unicode and an atomic plugin token during preedit.
 const text='😀 before\n\n after\t',id='bot-plugin:notes',offset='😀 before\n\n'.length;
 writeComposer(el,text,[id],[{id,offset}],()=> 'Notes Skill');el.dispatchEvent(new Event('input',{bubbles:true}));
 await wait(()=>save().at(-1)?.args[0].referenceIds?.[0]===id);
 const token=el.querySelector('[data-reference]');const last=el.lastChild!;const before=readComposer(el);
 const end=document.createRange();end.setStart(last,last.textContent!.length);end.collapse(true);getSelection()!.removeAllRanges();getSelection()!.addRange(end);
 compose(el,'compositionstart');last.textContent+='con';input(el);compose(el,'compositionupdate','con');
 check(el.querySelector('[data-reference]')===token&&token?.getAttribute('contenteditable')==='false','atomic token replaced');
 last.textContent=last.textContent!.slice(0,-3)+'从';input(el);compose(el,'compositionend','从');
 await wait(()=>save().at(-1)?.args[0].text===text+'从');
 const doc=readComposer(el);check(doc.referencePositions[0].offset===offset&&doc.referenceIds[0]===id,'plugin reference moved');
 check(before.text===text&&doc.text===text+'从','text lost in composition');
 set('');await wait(()=>save().at(-1)?.args[0].text==='');check(placeholder(el),'delete-to-empty placeholder missing');
 // A fresh intentional Enter remains usable after composition has ended.
 set('ready');await wait(()=>save().at(-1)?.args[0].text==='ready');w.fixtureUnknownSend=true;key(el,'Enter',13);await wait(()=>submit().length===1);
 check(submit()[0].args[0].text==='ready','intentional send changed text');
 return {ok:true,preeditDOM:true,preeditCaret:true,cancel:true,deleteToEmpty:true,atomicReference:true,textFidelity:true,imeSubmits:0,intentionalSubmits:1};
};
w.runMenuRegression=async()=>{
 await wait(()=>!!document.querySelector('.settings-select-trigger')&&!document.querySelector<HTMLButtonElement>('.settings-select-trigger')!.disabled);
 await delay(250); // Let the existing surface arrival animation finish.
 const trigger=document.querySelector<HTMLButtonElement>('.settings-select-trigger')!;
 const page=document.querySelector<HTMLElement>('.setup-page');check(!page||page.scrollWidth<=page.clientWidth+1,'provider form overflows narrow viewport');
 const secret=document.querySelector<HTMLInputElement>('input[type=password]');
 if(secret){const setter=Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value')!.set!;setter.call(secret,'synthetic-test-only');secret.dispatchEvent(new Event('input',{bubbles:true}));}
 const effort=()=>document.querySelector<HTMLInputElement>('.runtime-effort-segments input:checked')?.value;
 const originalEffort=effort();trigger.click();await wait(()=>trigger.getAttribute('aria-expanded')==='true');
 const menu=document.querySelector<HTMLElement>('.settings-select-menu')!;
 const box=menu.getBoundingClientRect();check(box.left>=0&&box.right<=innerWidth+1&&box.top>=0&&box.bottom<=innerHeight+1,'menu clipped by viewport');
 const hits=[];
 for(let y=box.top+3;y<box.bottom-2;y+=4)for(let x=box.left+3;x<box.right-2;x+=Math.max(8,box.width/8)){const hit=document.elementFromPoint(x,y);check(menu.contains(hit),'lower control paints/hits through menu: '+hit?.outerHTML);hits.push(hit?.tagName);}
 const options=[...menu.querySelectorAll<HTMLButtonElement>('[role=menuitemradio]')];
 check(options.length>=2,'missing model choices');
 const option=options.at(-1)!,rect=option.getBoundingClientRect(),hit=document.elementFromPoint(rect.left+rect.width/2,rect.top+rect.height/2) as HTMLElement;
 check(option.contains(hit),'option not hit-testable');hit.click();await wait(()=>trigger.getAttribute('aria-expanded')==='false');
 check(trigger.textContent?.includes('DeepSeek v4-pro'),'pointer did not select model');check(effort()===originalEffort,'model menu changed effort');
 check(!secret||secret===document.querySelector('input[type=password]')&&secret.value==='synthetic-test-only','menu lost form draft');
 trigger.click();await delay(20);const reopened=document.querySelector<HTMLElement>('.settings-select-menu')!;key(reopened,'Home');const focused=document.activeElement as HTMLButtonElement;check(reopened.contains(focused),'keyboard focus escaped menu');focused.click();await delay(20);
 check(trigger.textContent?.includes('DeepSeek flash'),'keyboard selection failed');
 trigger.click();await delay(20);key(document.querySelector<HTMLElement>('.settings-select-menu')!,'Escape');await delay();check(!closed&&document.activeElement===trigger,'Escape closed parent or lost focus');
 trigger.click();await delay();document.body.dispatchEvent(new PointerEvent('pointerdown',{bubbles:true}));await delay();check(trigger.getAttribute('aria-expanded')==='false','outside click did not dismiss');
 check(mutations===0&&closed===0,'menu mutated connection or closed form');
 return {ok:true,view,fallback:params.has('fallback'),width:innerWidth,height:innerHeight,hitSamples:hits.length,effort:originalEffort,formPreserved:true,mutations,closed};
};
w.recordedInputEvents=[];
for(const type of ['compositionstart','compositionupdate','compositionend','input','keydown'])document.addEventListener(type,event=>{const target=event.target as HTMLElement;if(!target.matches('.composer-editor'))return;w.recordedInputEvents.push({type,isTrusted:event.isTrusted,key:(event as KeyboardEvent).key,keyCode:(event as KeyboardEvent).keyCode,isComposing:(event as InputEvent).isComposing,data:(event as CompositionEvent).data,text:target.textContent,placeholder:placeholder(target)});});
w.showCompositionPreview=()=>{
 const el=document.querySelector<HTMLElement>('.composer-editor')!;el.focus();el.textContent='';el.dispatchEvent(new InputEvent('input',{bubbles:true}));
 compose(el,'compositionstart');el.textContent='con';compose(el,'compositionupdate','con');input(el);
};
w.uiFixtureReady=true;
