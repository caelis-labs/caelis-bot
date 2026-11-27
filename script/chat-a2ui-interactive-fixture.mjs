import {budget, calculator, practice, weather} from './a2ui-chat-examples.mjs';

export const a2uiInteractiveFixture = String.raw`
window.fixtureInteractiveCards = async () => {
 const cases = ${JSON.stringify({budget, calculator, weather, practice})};
 const titles = {budget:'预算分摊',calculator:'计算器',weather:'天气查询',practice:'跨页练习'};
 const capture = ${JSON.stringify(process.env.BOT_PREVIEW_A2UI_CASE ?? 'interactive')};
 const checks = [];
 let id = '';
 const wrap = body => '<a2ui>\n'+body+'\n</a2ui>';
 const root = () => document.querySelector('[data-message-id="'+id+'"] .chat-a2ui-surface');
 const button = name => [...(root()?.querySelectorAll('.chat-a2ui-button') ?? [])].find(el => el.textContent === name);
 const label = name => [...(root()?.querySelectorAll('label') ?? [])].find(el => el.textContent.trim() === name);
 const control = name => label(name)?.control ?? label(name)?.querySelector('input');
 const editor = () => document.querySelector('.composer-input[contenteditable="true"],[contenteditable="true"]');
 const text = () => root()?.innerText ?? '';
 const has = value => text().includes(value);
 const wait = async (name, predicate) => {
  const deadline = performance.now() + 4000;
  while (!predicate()) {
   if (performance.now() > deadline) throw Error(name);
   await new Promise(resolve => setTimeout(resolve, 20));
  }
 };
 const show = async name => {
  id = 'a2ui-interactive-' + name;
  window.fixtureTarget = id;
  const item = {id,turnKey:id,kind:'assistant',text:wrap(cases[name]),status:'completed',artifacts:[]};
  snapshot.items = [item];
  snapshot.lastReceipt = {id:'',outcome:'',message:''};
  revision++;
  await wait('render '+name, () => root()?.querySelector('.chat-a2ui-title')?.textContent === titles[name] && !root().hidden);
  return item;
 };
 const edit = (element, value) => {
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(element, value);
  element.dispatchEvent(new Event('input',{bubbles:true}));
 };
 const press = async name => {
  await wait('available action '+name, () => button(name) && !button(name).disabled);
  button(name).click();
 };
 const expectedWeather = '查一下上海浦东未来七天的天气';
 const expectedPractice = '请批改这份练习。第一题选了 12，第二题选了 3、5。';
 const submitted = [];
 let finish;
 window.fixtureCardDispatch = (id,text,fields) => {
  submitted.push({id,text,fields});
  return new Promise(resolve => {finish = resolve;});
 };
 const accept = async (index) => {
  const call = submitted[index];
  snapshot.items.push({id:'native:'+call.id,requestId:call.id,kind:'user',status:'completed',text:call.text,chatCard:{fields:call.fields},seenAt:Date.now()*1000,artifacts:[]});
  snapshot.lastReceipt = {id:call.id,outcome:'accepted',message:''};
  revision++;
  finish(snapshot.lastReceipt);
  await wait('ordinary echo with receipt '+index, () => document.querySelector('[data-message-id="native:'+call.id+'"]') && root()?.querySelector('.chat-a2ui-snapshots'));
 };
 const changeTab = async (tab, key) => {
  const current = root().querySelector('[role=tab][aria-selected=true]');
  current.focus();
  current.dispatchEvent(new KeyboardEvent('keydown',{key,bubbles:true,cancelable:true}));
  await wait('keyboard tab '+tab, () => document.activeElement?.textContent === tab && document.activeElement.getAttribute('aria-selected') === 'true');
 };
 const initialComposerCalls = sendCount;
 const composerText = '独立保留的草稿';
 try {
  Object.assign(snapshot,{phase:'completed',canSend:true,canSteer:false,canInterrupt:false,currentTurn:'',activity:null,items:[]});
  revision++;
  await wait('composer ready', () => editor());
  editor().textContent = composerText;
  editor().dispatchEvent(new Event('input',{bubbles:true}));
  await wait('composer draft saved', () => draft.text === composerText);
  await show('budget');
  await wait('original budget', () => has('210.00') && has('70.00'));
  edit(control('人数'), '0');
  await wait('recoverable zero divisor and inline validation', () => control('人数').getAttribute('aria-invalid') === 'true' &&
   has('人数至少为 1') && has('请输入有效人数及费用') && button('请 Bot 优化预算').disabled);
  edit(control('人数'), '4');
  edit(control('餐费'), '90');
  await wait('budget totals recompute', () => has('240.00') && has('60.00') && !button('请 Bot 优化预算').disabled);
  if (submitted.length) throw Error('budget edits dispatched');
  await press('请 Bot 优化预算');
  await wait('budget submission', () => submitted.length === 1);
  if (submitted[0].text !== '请优化 4 人的预算，场地 120 元，餐费 90 元，交通 30 元。') throw Error('budget payload differs');
  await accept(0);
  edit(control('人数'), '5');
  await wait('budget still usable', () => has('48.00'));
  checks.push('预算即时合计、人均与原位校验，主动提交当前值后继续编辑');
  if (capture === 'interactive-budget') return finishCapture();

  await show('calculator');
  const calculate = async (keys, expected) => {
   for (const key of keys) {
    await press(key);
    // Let A2UI publish the clicked action before the next key.
    await new Promise(resolve => requestAnimationFrame(resolve));
   }
   await wait('calculator result '+expected, () => root()?.querySelectorAll('.chat-a2ui-title')[1]?.textContent === expected);
  };
  await calculate(['1','2','+','3','='], '15');
  await calculate(['×','2','='], '30');
  await calculate(['÷','0','='], '无法计算，请输入新数字');
  await calculate(['清除'], '0');
  if (submitted.length !== 1) throw Error('calculator dispatched');
  checks.push('计算器 12 + 3 = 15、× 2 = 30、除零与清除，零提交');
  if (capture === 'interactive-calculator') return finishCapture();

  const weatherItem = await show('weather');
  await wait('weather initially disabled', () => button('查天气').disabled && control('城市'));
  edit(control('城市'), '上海浦东');
  edit(control('日期范围'), '未来七天');
  await wait('weather enabled', () => !button('查天气').disabled);
  const weatherButton = button('查天气');
  weatherButton.focus({preventScroll:true});
  window.webkit.messageHandlers.mediaKeyboard.postMessage('enter');
  await wait('one keyboard submission', () => submitted.length === 2 && button('查天气').disabled);
  weatherButton.click();
  weatherButton.click();
  if (submitted.length !== 2 || submitted[1].text !== expectedWeather) throw Error('weather did not send exactly the declared natural-language text');
  await accept(1);
  window.fixtureVisibility = undefined;
  window.webkit.messageHandlers.historyLifecycle.postMessage('hide-and-reopen');
  await wait('native history reopened with edited city', () => window.fixtureVisibility?.hidden && window.fixtureVisibility.visible &&
   window.fixtureVisibility.released && control('城市')?.value === '上海浦东' && !button('查天气').disabled);
  await press('查天气');
  await press('查天气');
  const weatherRecorded = structuredClone(snapshot.items);
  snapshot.items = [];
  revision++;
  await wait('weather surface unmounted before history replay', () => !root());
  snapshot.items = weatherRecorded;
  revision++;
  await wait('fresh card restored its snapshot', () => !!root()?.querySelector('.chat-a2ui-snapshots') && control('城市')?.value === '');
  root().querySelector('.chat-a2ui-snapshots summary').click();
  await wait('weather submitted fields readable', () => has('上海浦东') && has('未来七天'));
  edit(control('城市'), '上海浦东');
  edit(control('日期范围'), '未来七天');
  await press('查天气');
  if (snapshot.items[0].text !== weatherItem.text || submitted.length !== 2) throw Error('weather replay sent or modified original source');
  checks.push('天气按钮原生 Enter、快速连点、隐藏重开与重挂历史，普通消息只提交一次');
  if (capture === 'interactive-weather') return finishCapture();

  await show('practice');
  await wait('required answers gate both actions', () => button('本地评分')?.disabled && button('请 Bot 批改')?.disabled);
  control('10').click();
  await wait('first wrong feedback', () => has('再试一次'));
  control('12').click();
  await wait('first correct feedback', () => has('答对了'));
  await changeTab('多选','ArrowRight');
  control('3').click();
  control('5').click();
  await wait('multiple select feedback and cross-page actions', () => has('答对了') && !button('本地评分').disabled && !button('请 Bot 批改').disabled);
  await press('本地评分');
  const score = () => [...(root()?.querySelectorAll('tbody td') ?? [])].map(cell => cell.textContent);
  await wait('local immutable score', () => score().join('|') === '2|12|3、5');
  edit(control('个人备注'), '这条备注留在卡内');
  if (submitted.length !== 2) throw Error('local practice dispatched');
  await press('请 Bot 批改');
  await wait('practice submission', () => submitted.length === 3);
  if (submitted[2].text !== expectedPractice || submitted[2].fields.length !== 2) throw Error('inactive page was lost or private note was sent');
  await accept(2);
  await changeTab('单选','Home');
  await wait('first page retained earlier choice', () => control('12')?.checked);
  control('10').click();
  await wait('old score remains fixed after editing', () => has('再试一次') && score()[0] === '2');
  await press('请 Bot 批改');
  await wait('a changed submission reaches Bot', () => submitted.length === 4);
  await accept(3);
  const submittedRows = root().querySelector('.chat-a2ui-snapshots');
  submittedRows.open = true;
  await wait('both immutable history snapshots', () => submittedRows.querySelectorAll('ol > li').length === 2 &&
   submittedRows.querySelectorAll('dd')[0]?.textContent === '12' && submittedRows.querySelectorAll('dd')[2]?.textContent === '10');
  checks.push('练习随练随判、跨页整卷评分、Bot 批改、两次作答快照各自保留');
  finishCapture();

  function finishCapture() {
   if (sendCount !== initialComposerCalls || editor().innerText !== composerText || draft.text !== composerText) throw Error('card action used or cleared composer');
   if (root()?.innerText.includes('答案将作为一条聊天消息发送')) throw Error('deprecated send copy is visible');
   root()?.scrollIntoView({block:'nearest'});
   window.fixtureExpected = {ok:true,checks,submitted:submitted.length,viewport:innerWidth,theme:document.documentElement.dataset.mode};
  }
 } catch (error) {
  window.fixtureExpected = {ok:false,error:String(error),checks};
 }
};
`;
