import {budget} from './a2ui-chat-examples.mjs';

export const chatResponsiveFixture = String.raw`
window.fixtureResponsiveChat = async () => {
 const initialWidth = innerWidth;
 const checks = [];
 const notes = '调整人数或费用后，可以在同一张卡片里看到合计与人均金额。选择不同的方案时，表格保留当前选项的说明。确认数据后，可以请 Bot 比较各项开销，并用普通聊天消息说明最终选择。';
 const fence = String.fromCharCode(96).repeat(3);
 const table = [
  '| 等级 | 能力 | 典型表现 |',
  '| --- | --- | --- |',
  '| L0 · 富文本 | Markdown、代码和附件 | 图片、表格和文字可以同一条消息发送。 |',
  '| L3 · 卡片状态 | 控件共享输入 | 切换标签页后可以继续使用已填的值。 |',
  '| L5 · 交给 Bot | 按需提交 | 用户主动提交方案，Bot 可以在后续消息中继续处理。 |',
  '| L6 · 第三方应用 | 外部系统交互 | desktop_chat_external_application_reference_for_long_identifiers |',
 ].join('\n');
 const source = [
  notes + notes,
  table,
  notes + notes,
  fence+'text\n'+'review_budget_inputs_'+'request_receipt_'.repeat(8)+'\n'+fence,
  '<a2ui>\n'+${JSON.stringify(budget)}+'\n</a2ui>',
 ].join('\n\n');
 const id = 'chat-responsive-fixture';
 const row = () => document.querySelector('[data-message-id="'+id+'"]');
 const rect = el => el.getBoundingClientRect();
 const elements = () => {
  const message = row().closest('.message-row');
  const body = row().querySelector('.markdown-body');
  return {
   message, body, avatar:message.querySelector('.bot-avatar'), bubbles:document.querySelector('.chat-content'),
   scroll:document.querySelector('.chat-scroll'), composer:document.querySelector('.compose-area'),
   short:document.querySelector('[data-message-id=chat-responsive-short] .message'), paragraphs:body.querySelectorAll(':scope > p'),
   table:body.querySelector('.message-table'), code:body.querySelector('.code-block'), pre:body.querySelector('.code-block pre'),
   card:body.querySelector('.chat-a2ui-surface'), columns:body.querySelector('.chat-a2ui-row'),
  };
 };
 const wait = async (name, predicate) => {
  const deadline = performance.now()+4000;
  while (!predicate()) {
   if(performance.now()>deadline) throw Error(name);
   await new Promise(resolve => requestAnimationFrame(resolve));
  }
 };
 const paint = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
 const resize = async width => {
  if (innerWidth !== width) {
   window.webkit.messageHandlers.historyLifecycle.postMessage({width});
   await wait('native content width '+width, () => innerWidth === width);
  }
  await paint();
 };
 const rounded = n => Math.round(n*10)/10;
 const lineCount = el => {
  const range = document.createRange();
  range.selectNodeContents(el);
  return new Set([...range.getClientRects()].map(line => Math.round(line.top))).size;
 };
 const check = viewport => {
  const {body, avatar, bubbles, scroll, composer, short, paragraphs, table, code, pre, card, columns} = elements();
  const fullWidth = rect(body).width;
  const align = [rect(bubbles).left-rect(composer).left, rect(bubbles).right-rect(composer).right];
  if (align.some(distance=>Math.abs(distance)>1)) throw Error('composer and message list differ '+align+' at '+viewport);
  if ([avatar,bubbles,composer,body].some(el=>rect(el).left<0||rect(el).right>viewport+1) ||
   scroll.scrollWidth>scroll.clientWidth+1 || document.documentElement.scrollWidth>viewport+1)
   throw Error('chat escaped the viewport at '+viewport);
  if ([...paragraphs,table,code,card].some(el=>Math.abs(rect(el).left-rect(body).left)>1)) throw Error('mixed blocks have different left edges at '+viewport);
  if (rect(short).width>=Math.min(240,fullWidth/2)) throw Error('short reply expanded at '+viewport);
  if ([table,code,card].some(el=>Math.abs(rect(el).width-fullWidth)>1)) throw Error('wide blocks do not share the reply width at '+viewport);
  return {
   viewport, chat:rounded(rect(bubbles).width), reply:rounded(fullWidth),
   prose:rounded(rect(paragraphs[0]).width), align, stack:getComputedStyle(columns).flexDirection,
   tableOver:table.scrollWidth-table.clientWidth, codeOver:pre.scrollWidth-pre.clientWidth,
   labelLines:[...table.querySelectorAll('tbody tr:nth-child(n+3) td:first-child')].map(lineCount),
  };
 };
 window.fixtureTarget = id;
 try {
  Object.assign(snapshot,{phase:'completed',canSend:true,canSteer:false,canInterrupt:false,currentTurn:'',activity:null,items:[
   {id:'chat-responsive-short',kind:'assistant',text:'收到。',status:'completed',artifacts:[]},
   {id,kind:'assistant',text:source,status:'completed',artifacts:[]},
  ]});
  revision++;
  await wait('mixed reply and composer rendered', () => row()?.querySelector('.chat-a2ui-row') && document.querySelector('.compose-area'));
  await resize(1340);
  const wide = check(1340);
  const proseMeasure = wide.prose / parseFloat(getComputedStyle(elements().paragraphs[0]).fontSize);
  if (wide.chat !== 1200 || wide.reply<1100 || proseMeasure<48 || proseMeasure>56 || wide.stack!=='row' ||
   wide.tableOver>1 || wide.labelLines.length!==2 || wide.labelLines.some(lines=>lines!==1))
   throw Error('wide reading measure '+JSON.stringify(wide));
  checks.push(wide);
  for (const width of [900,650,540,430,360]) {
   await resize(width);
   const current = check(width);
   if (width >= 650 && current.stack !== 'row' || width <= 540 && current.stack !== 'column')
    throw Error('card did not reflow '+JSON.stringify(current));
   checks.push(current);
  }
  const narrow = checks.at(-1);
  if (narrow.tableOver < 20 || narrow.codeOver < 20) throw Error('wide content did not scroll locally '+JSON.stringify(narrow));
  const {table:tableScroller, pre, scroll, card} = elements();
  tableScroller.scrollLeft=50;
  pre.scrollLeft=50;
  scroll.scrollTop=0;
  const control = card.querySelector('input');
  control.focus();
  await wait('narrow card focus and vertical scroll', () => document.activeElement===control && scroll.scrollTop>0);
  if (tableScroller.scrollLeft<=0 || pre.scrollLeft<=0 || scroll.scrollLeft!==0 || window.scrollX!==0)
   throw Error('block scroll leaked to the outer chat');
  Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(control,'5');
  control.dispatchEvent(new Event('input',{bubbles:true}));
  await wait('narrow card still responds', () => card.textContent.includes('42.00'));
  await resize(initialWidth);
  elements().scroll.scrollTop=0;
  await paint();
  window.fixtureExpected = {ok:true,checks,viewport:innerWidth,theme:document.documentElement.dataset.mode};
 } catch(error) {
  window.fixtureExpected = {ok:false,error:String(error),checks};
 }
};
`;
