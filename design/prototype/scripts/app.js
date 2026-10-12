const ASSETS={"avatar": "assets/avatar.png", "pet": "assets/pet.webp", "portrait": "assets/portrait.png"};
'use strict';
/* Caelis Bot — offline interaction prototype. Original product code is untouched. */
const STORAGE_KEY='caelis-bot-prototype-workspace-v2';
const initialState=()=>({view:'chat',page:'general',lang:'zh',theme:'system',botName:'小爱',settings:{},experience:{},roomy:false});
const state=initialState();
try{const saved=JSON.parse(localStorage.getItem(STORAGE_KEY)||'null');if(saved&&typeof saved==='object'){Object.assign(state,saved);if(!['zh','en'].includes(state.lang))state.lang='zh';if(!['system','light','dark'].includes(state.theme))state.theme='system';if(!['chat','settings','desktop','onboarding'].includes(state.view))state.view='settings';}}catch(e){}
if(state.settings?.language==='system')state.lang=/^zh/i.test(navigator.language||'')?'zh':'en';
window.state=state;
const T=(zh,en)=>state.lang==='en'?(en===undefined?zh:en):zh;
const h=s=>String(s??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const ICON_PATHS={
 general:'<path d="M4 7h16M4 17h16"/><circle cx="9" cy="7" r="3" fill="var(--surface)"/><circle cx="15" cy="17" r="3" fill="var(--surface)"/>',
 appearance:'<path d="M12 3a9 9 0 1 0 0 18h1.4a2.2 2.2 0 0 0 1.7-3.6 1.5 1.5 0 0 1 1.1-2.4h1.3a3.5 3.5 0 0 0 3.4-3.6A8.8 8.8 0 0 0 12 3Z"/><circle cx="7.2" cy="10" r=".8"/><circle cx="10.6" cy="6.9" r=".8"/><circle cx="15.3" cy="7.7" r=".8"/><circle cx="6.8" cy="14.7" r=".8"/>',
 ai:'<path d="m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5L12 3Z"/>',
 chat:'<path d="M20 11.5a8 8 0 0 1-8 8H5l-3 2 .9-5.9A8 8 0 1 1 20 11.5Z"/><path d="M7 9h8M7 13h5"/>',
 channel:'<path d="m21 3-6.2 18-4.3-7.5L3 9.2 21 3Z"/><path d="m10.5 13.5 5-5"/>',
 plugin:'<path d="M9 4h6v3a3 3 0 1 1 0 6v3h-3a3 3 0 1 1-6 0H3v-6h3a3 3 0 1 1 3-3V4Z"/>',
 machine:'<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 21h8M12 16v5m-5-12 2 2-2 2m5 0h4"/>',
 tools:'<path d="M8 3H5a2 2 0 0 0-2 2v3m13-5h3a2 2 0 0 1 2 2v3M3 16v3a2 2 0 0 0 2 2h3m8 0h3a2 2 0 0 0 2-2v-3"/><rect x="7" y="7" width="10" height="10" rx="1.5"/>',
 shield:'<path d="m12 3 8 3v5c0 5-4.8 8.9-8 10-3.2-1.1-8-5-8-10V6l8-3Z"/><path d="m8 12 2.6 2.6L16 9"/>',
 info:'<circle cx="12" cy="12" r="9"/><path d="M12 11v6m0-10v.1"/>',
 chevron:'<path d="m9 5 7 7-7 7"/>',down:'<path d="m6 9 6 6 6-6"/>',back:'<path d="m14 5-7 7 7 7"/>',close:'<path d="m6 6 12 12M6 18 18 6"/>',plus:'<path d="M12 5v14M5 12h14"/>',check:'<path d="m5 12 4 4L19 6"/>',
 search:'<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',more:'<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
 send:'<path d="M12 19V5m-6 6 6-6 6 6"/>',stop:'<rect x="6" y="6" width="12" height="12" rx="2"/>',
 attachment:'<path d="m8 13 7.5-7.5a3.5 3.5 0 0 1 5 5L10 21a5.5 5.5 0 0 1-7.8-7.8L12 3.5a2.2 2.2 0 0 1 3.1 3.1L6.4 15.4a1 1 0 0 0 1.4 1.4l7.5-7.5"/>',
 file:'<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9l-6-6Z"/><path d="M14 3v6h6M8 13h8m-8 4h5"/>',
 folder:'<path d="M3 7a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z"/>',
 clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',refresh:'<path d="M20 7v5h-5M4 17v-5h5"/><path d="M6.1 7a7 7 0 0 1 11.6-1L20 9M4 15l2.3 3A7 7 0 0 0 17.9 17"/>',
 terminal:'<rect x="3" y="4" width="18" height="16" rx="2"/><path d="m7 9 3 3-3 3m6 0h4"/>',open:'<path d="M14 3h7v7m-8 1 8-8M10 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5"/>',
 lock:'<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3m-4 5v2"/>',copy:'<rect x="8" y="8" width="12" height="13" rx="2"/><path d="M15 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h3"/>',
 quote:'<path d="M10 6H4v7h5c0 3-2 5-4 5m15-12h-6v7h5c0 3-2 5-4 5"/>',bell:'<path d="M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9m-9 12a3 3 0 0 0 6 0"/>',
 eye:'<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z"/><circle cx="12" cy="12" r="3"/>',
 key:'<circle cx="8" cy="8" r="5"/><path d="m12 12 9 9m-4-4 3-3m-6 0 3-3"/>',pin:'<path d="m9 3 12 12-4 1-3 5-5-5-6-6 5-3 1-4Z"/><path d="m9 15-6 6"/>',
 image:'<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8" cy="8" r="1.5"/><path d="m3 16 5-4 4 4 4-6 5 7"/>',
 github:'<path d="M9 20c-4 1.2-4-2-6-2m12 4v-4a3.5 3.5 0 0 0-1-2.7c3.5-.4 7-1.7 7-7.4A5.7 5.7 0 0 0 19.5 4 5 5 0 0 0 19.4 0s-1.3-.4-4.4 1.6a15 15 0 0 0-8 0C3.9-.4 2.6 0 2.6 0a5 5 0 0 0-.1 4A5.7 5.7 0 0 0 1 7.9c0 5.7 3.5 7 7 7.4A3.5 3.5 0 0 0 7 18v4" transform="translate(1 2) scale(.9)"/>',
 wechat:'<path d="M14 14c-1 .5-2.4 1-4 1H7l-4 2 1-4a6 6 0 0 1-2-4c0-3.5 3.6-6 8-6s8 2.5 8 6"/><path d="M21 19.5 22 22l-3-1.5h-2c-3.9 0-7-2.3-7-5.3s3.1-5.2 7-5.2 7 2.2 7 5.2a4.8 4.8 0 0 1-3 4.3Z" transform="translate(-1 -1)"/><path d="M7 8h.01M12 8h.01M14 14h.01M18 14h.01"/>',
 sun:'<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/>',moon:'<path d="M20.5 13.6A8.6 8.6 0 0 1 10.4 3.5a9 9 0 1 0 10.1 10.1Z"/>',globe:'<circle cx="12" cy="12" r="9"/><ellipse cx="12" cy="12" rx="4" ry="9"/><path d="M3 12h18"/>',
 settings:'<path d="m10 3-1 3-3 1-3 3 2 3-1 3 3 3 3-1 3 2 3-2 3-3-1-3 2-3-3-3-3-1-1-3-3-1Z"/><circle cx="12" cy="12" r="3"/>',
 download:'<path d="M12 3v12m-5-5 5 5 5-5M4 16v4a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-4"/>',warning:'<path d="M10.3 4a2 2 0 0 1 3.4 0l8 14A2 2 0 0 1 20 21H4a2 2 0 0 1-1.7-3L10.3 4Z"/><path d="M12 9v5m0 3h.01"/>',upload:'<path d="M12 16V4m-5 5 5-5 5 5M4 16v4a1 1 0 0 0 1 1h14a1 1 0 0 0 1-1v-4"/>',arrowRight:'<path d="M4 12h16m-6-6 6 6-6 6"/>',
 home:'<path d="m3 10 9-7 9 7v10H3V10Z"/><path d="M9 20v-7h6v7"/>',sparkles:'<path d="m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5L12 3Z"/>',menu:'<path d="M4 6h16M4 12h16M4 18h16"/>',volume:'<path d="m10 4-5 4H2v8h3l5 4V4Zm5 4a6 6 0 0 1 0 8m3-11a10 10 0 0 1 0 14"/>'
};
function icon(name,size=20){return `<svg class="icon icon-${h(name)}" width="${Number(size)||20}" height="${Number(size)||20}" viewBox="0 0 24 24" aria-hidden="true">${ICON_PATHS[name]||ICON_PATHS.info}</svg>`;}
function btn(label,action,opts={}){const variant=opts.variant||'primary';return `<button type="button" class="btn btn-${h(variant)}${opts.class?' '+h(opts.class):''}" data-action="${h(action)}" ${opts.disabled?'disabled':''} ${opts.attrs||''}>${opts.icon?icon(opts.icon,16):''}${label?`<span>${h(label)}</span>`:''}</button>`;}
function badge(label,kind='neutral'){return `<span class="status-badge ${h(kind)}">${kind==='success'?'<span class="status-dot"></span>':''}${label}</span>`;}
function toggle(bind,checked,label){return `<button type="button" class="switch" role="switch" aria-checked="${!!checked}" aria-label="${h(label)}" data-bind="${h(bind)}"></button>`;}
let fieldCount=0;
function field(label,htmlInput,help=''){const id=htmlInput.match(/\bid=["']([^"']+)/)?.[1]||'f-'+(++fieldCount);if(!/\bid=["']/.test(htmlInput))htmlInput=htmlInput.replace(/<(input|select|textarea)(?=\s|>)/,'<$1 id="'+id+'"');return `<div class="field"><label for="${h(id)}">${label}</label>${htmlInput}${help?`<small class="field-help">${help}</small>`:''}</div>`;}
function save(){try{localStorage.setItem(STORAGE_KEY,JSON.stringify(state));}catch(e){/* File browsing may restrict storage; the live demo continues in memory. */}}
const themeQuery=window.matchMedia('(prefers-color-scheme: dark)');
function applyTheme(){const resolved=state.theme==='system'?(themeQuery.matches?'dark':'light'):state.theme;document.documentElement.dataset.theme=resolved;document.documentElement.lang=state.lang==='en'?'en':'zh-CN';document.title='Caelis Bot · '+T('交互原型','Interactive prototype');}
themeQuery.addEventListener?.('change',()=>{if(state.theme==='system')applyTheme();});
window.addEventListener('languagechange',()=>{if(state.settings.language==='system'){state.lang=/^zh/i.test(navigator.language||'')?'zh':'en';save();render();}});
let modalFactory=null,modalSpec=null,confirmCallback=null,lastFocusedElement=null,lastFocusDescriptor=null,lastRenderedKey='';
const NAV=[
 {zh:'常用',en:'EVERYDAY',items:[['general','general','常规','General'],['appearance','appearance','外观','Appearance'],['ai','ai','模型','Models']]},
 {zh:'连接与扩展',en:'CONNECTIONS',items:[['channels','channel','消息渠道','Channels'],['machines','machine','远程','Remote'],['plugins','plugin','插件','Plugins']]},
 {zh:'系统与工具',en:'SYSTEM & TOOLS',items:[['tools','tools','附加功能','Extra tools'],['privacy','shield','隐私与权限','Privacy & permissions'],['about','info','关于','About']]}
];
function pageLabel(page){const row=NAV.flatMap(g=>g.items).find(x=>x[0]===page);return row?T(row[2],row[3]):T('设置','Settings');}
function renderToolbar(){return `<header class="prototype-toolbar" aria-label="${T('原型演示控制','Prototype controls')}">
  <div class="prototype-brand"><svg class="brand-mark" viewBox="0 0 32 32" fill="none" aria-hidden="true"><path d="m26 6-8 21-4-10-9-4 21-7Z" stroke="currentColor" stroke-width="1.3" stroke-linejoin="round"/><path d="m14 17 7-7" stroke="currentColor" stroke-width="1.3"/></svg><div><span class="brand-word">CAELIS BOT</span><span class="prototype-caption">${T('产品交互原型','Product prototype')}</span></div></div>
  <nav class="prototype-views" aria-label="${T('演示视图','Demo views')}">${[['chat','chat','聊天','Chat'],['settings','general','设置','Settings'],['desktop','appearance','桌面','Desktop'],['onboarding','ai','首次引导','Onboarding']].map(([view,ic,zh,en])=>`<button type="button" data-action="r:view:${view}" ${state.view===view?'aria-current="page"':''}>${icon(ic,15)}<span>${T(zh,en)}</span></button>`).join('')}</nav>
  <div class="prototype-controls"><label class="toolbar-select">${icon('globe',16)}<span class="sr-only">${T('原型语言','Prototype language')}</span><select data-bind="r:lang" aria-label="${T('原型语言','Prototype language')}"><option value="zh" ${state.lang==='zh'?'selected':''}>简体中文</option><option value="en" ${state.lang==='en'?'selected':''}>English</option></select></label><label class="toolbar-select">${icon(state.theme==='dark'?'moon':state.theme==='light'?'sun':'appearance',16)}<span class="sr-only">${T('外观预览','Appearance preview')}</span><select data-bind="r:theme" aria-label="${T('外观预览','Appearance preview')}"><option value="system" ${state.theme==='system'?'selected':''}>${T('跟随系统','System')}</option><option value="light" ${state.theme==='light'?'selected':''}>${T('浅色','Light')}</option><option value="dark" ${state.theme==='dark'?'selected':''}>${T('深色','Dark')}</option></select></label></div>
 </header>`;}
function renderTitlebar(){const titles={chat:'Caelis Bot',settings:T('设置','Settings'),desktop:T('桌面上的 Caelis Bot','Caelis Bot on your desktop'),onboarding:T('欢迎使用 Caelis Bot','Welcome to Caelis Bot')};return `<div class="window-titlebar"><div class="traffic-lights"><button class="traffic-close" data-action="r:window-close" title="${T('关闭窗口，继续在后台工作','Close window and keep working')}" aria-label="${T('关闭窗口','Close window')}"></button><button class="traffic-min" data-action="r:window-min" title="${T('收起到桌面','Minimize to desktop')}" aria-label="${T('最小化','Minimize')}"></button><button class="traffic-max" data-action="r:window-max" title="${T('调整窗口高度','Resize window')}" aria-label="${T('调整窗口高度','Resize window')}"></button></div><div class="window-title">${h(titles[state.view])}</div><div class="titlebar-actions">${state.view==='chat'?`<button type="button" class="icon-btn" data-action="r:view:settings" aria-label="${T('打开设置','Open settings')}" title="${T('设置','Settings')}">${icon('general',16)}</button>`:state.view==='settings'?`<button type="button" class="icon-btn" data-action="r:view:chat" aria-label="${T('返回聊天','Back to chat')}" title="${T('返回聊天','Back to chat')}">${icon('chat',16)}</button>`:''}</div></div>`;}
function renderSettings(){if(!NAV.some(g=>g.items.some(i=>i[0]===state.page)))state.page='ai';return `<div class="settings-layout"><aside class="settings-sidebar"><div class="sidebar-profile"><img src="${ASSETS.avatar}" alt=""><div><strong>${h(state.botName)}</strong><span>Caelis Bot</span></div></div><nav class="settings-nav" aria-label="${T('设置导航','Settings navigation')}">${NAV.map(g=>`<div class="nav-group"><div class="nav-group-title">${T(g.zh,g.en)}</div>${g.items.map(([id,ic,zh,en])=>`<button class="nav-item ${id===state.page?'active':''}" type="button" data-action="r:page:${id}" ${id===state.page?'aria-current="page"':''}>${icon(ic,18)}<span>${T(zh,en)}</span></button>`).join('')}</div>`).join('')}</nav><div class="sidebar-return"><button type="button" class="nav-item" data-action="r:view:chat">${icon('back',16)}<span>${T('返回聊天','Back to chat')}</span></button></div></aside><div class="mobile-nav-select"><label class="sr-only" for="mobile-settings-nav">${T('设置页面','Settings page')}</label><select id="mobile-settings-nav" data-bind="r:page">${NAV.map(g=>`<optgroup label="${T(g.zh,g.en)}">${g.items.map(([id,ic,zh,en])=>`<option value="${id}" ${id===state.page?'selected':''}>${T(zh,en)}</option>`).join('')}</optgroup>`).join('')}</select></div><main class="settings-scroll" id="settings-scroll" aria-label="${h(pageLabel(state.page))}"><div class="settings-content">${window.Settings?Settings.render(state.page):'<div class="empty-state">'+T('正在准备设置…','Preparing settings…')+'</div>'}</div></main></div>`;}
function captureFocus(){const el=document.activeElement;if(!el||el===document.body)return null;return {id:el.id,bind:el.dataset?.bind,input:el.dataset?.input,action:el.dataset?.action,itemId:el.dataset?.id,optionIndex:el.dataset?.index,start:el.selectionStart,end:el.selectionEnd};}
function restoreFocus(f){if(!f)return;let el=f.id?document.getElementById(f.id):null;if(!el&&f.bind)el=document.querySelector(`[data-bind="${CSS.escape(f.bind)}"]`);if(!el&&f.input)el=document.querySelector(`[data-input="${CSS.escape(f.input)}"]`);if(!el&&f.action)el=document.querySelector(`[data-action="${CSS.escape(f.action)}"]${f.itemId?`[data-id="${CSS.escape(f.itemId)}"]`:""}${f.optionIndex!=null?`[data-index="${CSS.escape(f.optionIndex)}"]`:""}`);if(el&&el.offsetParent!==null){el.focus({preventScroll:true});if(typeof el.setSelectionRange==='function'&&f.start!==null){try{el.setSelectionRange(f.start,f.end);}catch(e){}}}}
const scrollMemory = new Map();
function render(){
  const focus=captureFocus(),key=state.view+':'+state.page;
  const scrollers=['settings-scroll','x-chat-scroll'];
  if(lastRenderedKey){for(const id of scrollers){const el=document.getElementById(id);if(el)scrollMemory.set(lastRenderedKey+':'+id,el.scrollTop);}}
  applyTheme();
  let content='';
  if(state.view==='settings')content=renderSettings();
  else if(window.Experience)content=state.view==='chat'?Experience.renderChat():state.view==='desktop'?Experience.renderDesktop():Experience.renderOnboarding();
  document.getElementById('app').innerHTML=renderToolbar()+`<div class="app-stage view-${h(state.view)}"><section class="app-window ${state.roomy?'roomy':''}" data-view="${h(state.view)}" aria-label="${T('Caelis Bot 应用原型','Caelis Bot application prototype')}">${renderTitlebar()}<div class="window-body">${content}</div></section><footer class="prototype-footer"><span class="footer-hint">${T('本地原型 · 场景模拟','Local prototype · Simulated scenarios')}</span><div class="footer-right">${state.view==='chat'&&window.Experience?Experience.renderScenarioControls():''}<button type="button" data-action="r:guide">${T('指南','Guide')}</button><button type="button" data-action="r:reset">${T('重置','Reset')}</button></div></footer></div>`;
  for(const id of scrollers){const el=document.getElementById(id);if(el&&scrollMemory.has(key+':'+id))el.scrollTop=scrollMemory.get(key+':'+id);}
  lastRenderedKey=key;
  if(modalFactory)renderModal(false);
  if(window.Experience?.afterRender)Experience.afterRender();
  restoreFocus(focus);
}
function navigate(view,page) {
  if (!['chat','settings','desktop','onboarding'].includes(view)) return;
  window.Settings?.clearTransient?.();
  window.Experience?.dismissPopover?.();
  modalFactory=null; modalSpec=null;
  document.getElementById('modal-root').innerHTML='';
  document.getElementById('app').inert=false;
  state.view=view;
  if (page) state.page=page;
  save(); render();
  requestAnimationFrame(() => {
    if (view==='chat') {
      const input=document.getElementById('x-chat-input');
      if (input) { input.focus({preventScroll:true}); input.setSelectionRange(input.value.length,input.value.length); }
    } else {
      const heading=document.querySelector('.settings-content h1,.x-onboard-content h1');
      if (heading) { heading.tabIndex=-1; heading.focus({preventScroll:true}); }
    }
  });
}
function toast(zh,en,kind='success'){if(['success','warning','danger','neutral'].includes(en)&&arguments.length===2){kind=en;en=zh;}const region=document.getElementById('toast-root');const t=document.createElement('div');t.className='toast '+kind;t.setAttribute('role','status');t.innerHTML=icon(kind==='danger'||kind==='warning'?'warning':'check',16)+`<span>${h(T(zh,en))}</span>`;region.appendChild(t);while(region.children.length>3)region.firstElementChild.remove();setTimeout(()=>{t.remove();},3800);}
function openModal(factory){if(!modalFactory){lastFocusedElement=document.activeElement;lastFocusDescriptor=captureFocus();}modalFactory=typeof factory==='function'?factory:()=>factory;document.getElementById('app').inert=true;renderModal(true);}
function renderModal(focusFirst=true){if(!modalFactory)return;const focus=captureFocus();modalSpec=modalFactory();if(!modalSpec){closeModal();return;}const spec=modalSpec;document.getElementById('modal-root').innerHTML=`<div class="modal-layer" data-modal-layer><section role="dialog" aria-modal="true" aria-labelledby="modal-title" class="modal ${h(spec.width||'medium')}"><header class="modal-header"><div><h2 id="modal-title">${h(spec.title||'')}</h2>${spec.subtitle?`<p>${h(spec.subtitle)}</p>`:''}</div><button class="icon-btn" type="button" data-action="r:modal-close" aria-label="${T('关闭弹窗','Close dialog')}">${icon('close',19)}</button></header><div class="modal-body">${spec.body||''}</div><footer class="modal-footer">${spec.footer||''}</footer></section></div>`;if(focusFirst){requestAnimationFrame(()=>{const el=document.querySelector('.modal [autofocus]')||document.querySelector('.modal input:not([type=hidden]),.modal textarea,.modal select')||document.querySelector('.modal-footer .btn-primary')||document.querySelector('.modal button');el?.focus({preventScroll:true});});}else restoreFocus(focus);}
function closeModal(){window.Settings?.clearTransient?.();modalFactory=null;modalSpec=null;confirmCallback=null;document.getElementById('modal-root').innerHTML='';document.getElementById('app').inert=false;if(lastFocusedElement?.isConnected)lastFocusedElement.focus({preventScroll:true});else restoreFocus(lastFocusDescriptor);lastFocusedElement=null;lastFocusDescriptor=null;}
function confirmDialog({title,body,confirmLabel,onConfirm,danger=false}){confirmCallback=onConfirm;openModal(()=>({title,body:typeof body==='string'?`<div class="stack">${body}</div>`:body,width:'small',footer:btn(T('取消','Cancel'),'r:modal-close',{variant:'secondary'})+btn(confirmLabel||T('确认','Confirm'),'r:confirm',{variant:danger?'danger':'primary'})}));}
async function copyText(text){try{await navigator.clipboard.writeText(text);}catch(e){const el=document.createElement('textarea');el.value=text;el.style.position='fixed';el.style.left='-9999px';document.body.appendChild(el);el.select();document.execCommand('copy');el.remove();}toast('已复制','Copied');}
function downloadFile(name,contents,type='text/plain;charset=utf-8'){const url=URL.createObjectURL(new Blob([contents],{type}));const a=document.createElement('a');a.href=url;a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}
function guideModal(){openModal(()=>({title:T('从日常开始，走完关键流程','Explore the complete experience'),subtitle:T('同一个长期助手，出现在聊天、桌面和你常用的消息渠道里。','One long-lived assistant across chat, your desktop, and messaging.'),width:'large',body:`<div class="stack"><div class="notice">${icon('info',18)}<div>${T('所有操作都在本地演示。无需 API Key、账号或后端；关闭后再次打开，可继续上次的演示状态。','Everything runs locally. No API key, account, or backend is required. Your demo preferences are remembered in this browser.')}</div></div>${[
 ['onboarding','','ai','首次见面','Meet your bot','命名 → 选择能力 → 连接 AI → 进入聊天','Name → Choose capabilities → Connect AI → Chat'],
 ['chat','','chat','现代 IM 对话','A natural conversation','流式回复、附件、审批、问题和结果，保留在同一条消息流中','Streaming, attachments, approvals, questions, and results in one message flow'],
 ['settings','ai','general','完整设置','Complete settings','AI 与账号、消息渠道、插件、远程机器及所有应用偏好','AI, accounts, messaging, plugins, remote machines, and app preferences'],
 ['desktop','','appearance','桌面陪伴','Desktop presence','桌宠气泡与任务预览；收起窗口后，工作仍在继续','Pet bubbles and task previews. Work continues when the window is hidden']
 ].map(([view,page,ic,zh,en,descZh,descEn])=>`<div class="service-row"><span class="service-icon">${icon(ic,21)}</span><div class="service-copy"><h3>${T(zh,en)}</h3><p>${T(descZh,descEn)}</p></div>${btn(T('进入','Explore'),'r:goto:'+view+':'+page,{variant:'secondary',icon:'arrowRight'})}</div>`).join('')}<details class="disclosure"><summary>${T('设计依据与原型边界','Sources and prototype scope')}</summary><p class="small muted">${T('依据 Caelis Bot 源码 e311c0d，以及《Caelis Bot 功能清单与现有 UI 实机图册》的 47 页资料设计。','Based on Caelis Bot source e311c0d and the 47-page product inventory and real UI atlas.')}</p><p class="small muted" style="margin-top:8px">${T('账号授权、系统权限、SSH 与后台任务均为交互模拟，不会向外部服务发送请求。演示场景控件与应用设置分开显示。主题默认跟随系统，上方可临时切换浅色和深色效果。','Authorization, system permissions, SSH, and background tasks are simulated without external requests. Scenario controls are separate from app settings. Theme follows your system by default, with light and dark previews above.')}</p><p class="small" style="margin-top:10px"><a href="https://github.com/caelis-labs/caelis-bot/tree/e311c0d76d5d88cedc73f2b062595087a0e6def0" target="_blank" rel="noopener noreferrer">${T('查看来源仓库','View source repository')} ↗</a></p></details></div>`,footer:btn(T('开始浏览','Start exploring'),'r:modal-close',{variant:'primary'})}));}
function rootAction(action,el){if(action.startsWith('r:view:'))return navigate(action.split(':')[2]);if(action.startsWith('r:page:'))return navigate('settings',action.split(':')[2]);if(action.startsWith('r:goto:')){const parts=action.split(':');return navigate(parts[2],parts[3]||undefined);}if(action==='r:modal-close')return closeModal();if(action==='r:confirm'){const cb=confirmCallback;closeModal();if(cb)cb();return;}if(action==='r:guide')return guideModal();if(action==='r:window-close'||action==='r:window-min'){navigate('desktop');return toast('窗口已收起，任务继续进行','Window hidden. Your tasks continue.');}if(action==='r:window-max'){state.roomy=!state.roomy;save();render();return;}if(action==='r:reset'){confirmDialog({title:T('重置演示？','Reset the demo?'),body:`<p class="muted">${T('恢复示例连接、聊天和设置。此操作只影响当前浏览器内的原型状态。','Restore sample connections, messages, and preferences. This only affects this browser’s prototype state.')}</p>`,confirmLabel:T('重置演示','Reset demo'),onConfirm:()=>{localStorage.removeItem(STORAGE_KEY);location.reload();}});}}
function dispatchChange(bind,value,el,event){if(bind==='r:lang'){state.lang=value;state.settings.language=value;save();render();return;}if(bind==='r:theme'){state.theme=value;save();render();return;}if(bind==='r:page')return navigate('settings',value);try{if(bind?.startsWith('s:')&&window.Settings)Settings.handleChange(bind,value,el,event);else if(bind?.startsWith('x:')&&window.Experience)Experience.handleChange(bind,value,el,event);}catch(error){console.error(error);toast('这一步未完成，请重新打开后再试','This step did not complete. Please reopen it and try again.','danger');}}
document.addEventListener('click',event=>{
 const switchEl=event.target.closest('[role=switch][data-bind]');if(switchEl&&!switchEl.disabled){const value=switchEl.getAttribute('aria-checked')!=='true';switchEl.setAttribute('aria-checked',String(value));dispatchChange(switchEl.dataset.bind,value,switchEl,event);return;}
 const el=event.target.closest('[data-action]');if(!el||el.disabled)return;const action=el.dataset.action;try{if(action.startsWith('r:'))rootAction(action,el);else if(action.startsWith('s:')&&window.Settings)Settings.handleAction(action,el,event);else if(action.startsWith('x:')&&window.Experience)Experience.handleAction(action,el,event);}catch(error){console.error(error);toast('这一步未完成，请重新打开后再试','This step did not complete. Please reopen it and try again.','danger');}
});
document.addEventListener('change',event=>{const el=event.target;if(el.dataset.bind&&el.getAttribute('role')!=='switch'){const value=el.type==='checkbox'?el.checked:el.value;dispatchChange(el.dataset.bind,value,el,event);}});
document.addEventListener('input',event=>{const el=event.target;if(el.dataset.input)dispatchChange(el.dataset.input,el.value,el,event);});
document.addEventListener('submit',event=>{event.preventDefault();const button=event.target.querySelector('[type=submit][data-action]');if(button)button.click();});
document.addEventListener('keydown',event=>{
 if(event.key==='Escape'&&modalFactory){event.preventDefault();closeModal();return;}
 if(event.key==='Tab'&&modalFactory){const modal=document.querySelector('.modal');const focusable=[...modal.querySelectorAll('button:not(:disabled),a[href],input:not(:disabled):not([type=hidden]),textarea:not(:disabled),select:not(:disabled),[tabindex="0"]')].filter(el=>el.getClientRects().length>0);if(!focusable.length)return;const first=focusable[0],last=focusable[focusable.length-1];if(event.shiftKey&&document.activeElement===first){event.preventDefault();last.focus();}else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first.focus();}}
 if(event.ctrlKey&&event.shiftKey&&event.code==='Space'){event.preventDefault();navigate('chat');}
});
window.CaelisPrototype={state,render,navigate,openModal,closeModal,applyTheme,T,h,save};
