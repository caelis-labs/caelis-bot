(function () {
  'use strict';

  let streamTimer = null;
  let dragDepth = 0;
  let hydrated = false;
  let chatMounted = false;
  let draggedTaskId = null;
  const localFiles = new Map();
  const translated = (zh, en) => ({ zh, en });
  const read = value => value && typeof value === 'object' ? T(value.zh || '', value.en || value.zh || '') : String(value || '');
  const esc = value => h(String(value == null ? '' : value));
  const avatar = () => ASSETS.avatar || ASSETS.pet || '';
  const bot = () => state.botName || T('小爱', 'Caelis');
  const xi = (name, size) => icon(name, size || 18);
  const xb = (label, action, options) => btn(label, action, options || {});
  const textHtml = value => esc(read(value)).replace(/\n/g, '<br>');

  function defaults() {
    const x = state.experience || (state.experience = {});
    if (!Array.isArray(x.messages)) x.messages = starterMessages();
    if (!Array.isArray(x.attachments)) x.attachments = [];
    if (!Array.isArray(x.tasks)) x.tasks = [
      { id: 'task-1', title: translated('整理本周项目更新', 'Prepare this week’s project update'), status: 'working', detail: translated('正在阅读最近的变更', 'Reading the latest changes'), machine: translated('这台电脑', 'This Mac'), pinned: true, visible: true },
      { id: 'task-2', title: translated('汇总交互改进建议', 'Summarize interaction improvements'), status: 'done', detail: translated('已整理 5 条建议', 'Five suggestions are ready'), machine: translated('这台电脑', 'This Mac'), pinned: false, visible: true }
    ];
    if (!x.setup) x.setup = { step: 0, name: state.botName || '小爱', description: '', provider: 'codex', connected: true, tools: true, permissions: { notifications: false, screen: false, accessibility: false } };
    if (!x.permissions) x.permissions = { notifications: true, screen: true, accessibility: true };
    if (x.connected === undefined) x.connected = true;
    if (x.petVisible === undefined) x.petVisible = true;
    if (x.petState === undefined) x.petState = 'idle';
    if (x.toolsEnabled === undefined) x.toolsEnabled = true;
    if (!x.draft) x.draft = '';
    if (!x.sequence) x.sequence = 20;
    if (!x.scenario) x.scenario = 'normal';
    if (!x.collapsed) x.collapsed = {};
    if (!hydrated) {
      hydrated = true;
      if (x.running) {
        const partial = x.messages.find(m => m.id === x.streamingId);
        if (partial) { partial.stopped = true; if (!read(partial.body)) partial.body = translated('这次回复已暂停。', 'This response was paused.'); }
        x.running = false;
        x.streamingId = null;
      }
    }
    const settings = state.settings || {};
    if (settings.ai) x.connected = settings.ai.connected;
    if (settings.captureEnabled !== undefined) x.toolsEnabled = settings.captureEnabled;
    if (settings.petVisible !== undefined) x.petVisible = settings.petVisible;
    if (settings.permissions) x.permissions = { notifications: settings.permissions.notifications, screen: settings.permissions.screenCapture, accessibility: settings.permissions.accessibility };
    return x;
  }

  function setConnected(connected) {
    const x = defaults();
    x.connected = connected;
    if (!state.settings) state.settings = {};
    if (!state.settings.ai) state.settings.ai = {};
    state.settings.ai.connected = connected;
  }

  function setPetVisible(visible) {
    defaults().petVisible = visible;
    if (state.settings) state.settings.petVisible = visible;
  }

  function starterMessages() {
    return [
      { id: 'intro', role: 'assistant', kind: 'text', body: translated('我在。今天想先做点什么？', 'I’m here. What would you like to work on today?'), time: '09:41' },
      { id: 'sample-user', role: 'user', kind: 'text', body: translated('帮我把今天的项目进展整理成一份简洁的更新，留一小段明天的安排。', 'Please turn today’s project progress into a short update, with a few next steps for tomorrow.'), time: '09:42' },
      { id: 'sample-result', role: 'assistant', kind: 'result', body: translated('整理好了。我把已完成的事情和下一步分开，方便你直接分享。', 'It’s ready. I separated completed work from next steps so it’s easy to share.'), time: '09:42', filename: '项目更新.md', result: translated('## 今日进展\n\n- 梳理聊天、消息渠道与设置的主要使用路径。\n- 统一连接状态和必要决定的反馈。\n- 明确远程机器的配置入口。\n\n## 明天的安排\n\n继续核对附件预览和任务恢复的体验。', '## Today’s progress\n\n- Reviewed the main flows for chat, channels, and settings.\n- Aligned connection states and decision feedback.\n- Clarified how to configure remote machines.\n\n## Tomorrow\n\nReview attachment previews and task recovery.') }
    ];
  }

  function now() {
    return new Date().toLocaleTimeString(state.lang === 'en' ? 'en-GB' : 'zh-CN', { hour: '2-digit', minute: '2-digit' });
  }

  function addMessage(data) {
    const x = defaults();
    const message = Object.assign({ id: 'm-' + (++x.sequence), time: now() }, data);
    x.messages.push(message);
    return message;
  }

  function showToast(zh, en, kind) { toast(zh, en, kind); }

  function renderAvatar(size) {
    if (state.settings && state.settings.character === 'stick' && state.settings.avatarMode !== 'caelis') return '<span class="x-avatar x-stick-avatar' + (size ? ' x-avatar-' + size : '') + '" role="img" aria-label="' + esc(bot()) + '"><svg viewBox="0 0 32 32" aria-hidden="true"><circle cx="16" cy="12" r="6"/><path d="M16 18v10m0-7-7 4m7-4 7 4"/></svg></span>';
    return '<img class="x-avatar' + (size ? ' x-avatar-' + size : '') + '" src="' + esc(avatar()) + '" alt="' + esc(bot()) + '">';
  }

  function petGraphic(name) {
    if (state.settings && state.settings.character === 'stick') return '<svg class="x-stick-pet" viewBox="0 0 160 240" role="img" aria-label="' + esc(name || bot()) + '"><circle cx="80" cy="38" r="24"/><path d="M80 64v88m0-65-42 40m42-40 44 37m-44 28-34 62m34-62 36 62"/></svg>';
    return '<img src="' + esc(ASSETS.pet) + '" alt="' + esc(name || bot()) + '" draggable="false">';
  }

  function iconButton(action, name, label, extra) {
    return '<button type="button" class="icon-btn x-icon-button" data-action="' + action + '" aria-label="' + esc(label) + '" title="' + esc(label) + '" ' + (extra || '') + '>' + xi(name, 19) + '</button>';
  }

  function pendingMessages() {
    return defaults().messages.filter(m => (m.kind === 'question' || m.kind === 'approval') && !m.resolved);
  }

  function messageMenu(m) {
    return '<div class="x-message-tools">' + iconButton('x:copy-message', 'copy', T('复制消息', 'Copy message'), 'data-id="' + esc(m.id) + '"') + iconButton('x:quote-message', 'quote', T('引用消息', 'Quote message'), 'data-id="' + esc(m.id) + '"') + '</div>';
  }

  function fileCard(filename, action, id, secondary, imageURL) {
    return '<button type="button" class="x-file-card" data-action="' + action + '" data-id="' + esc(id) + '">' + (imageURL ? '<img src="' + esc(imageURL) + '" alt="" class="x-file-thumb">' : '<span class="x-file-icon">' + xi('file', 22) + '</span>') + '<span class="x-file-copy"><strong>' + esc(filename) + '</strong><span>' + esc(secondary || T('点击查看', 'Click to preview')) + '</span></span>' + xi('open', 16) + '</button>';
  }


  function renderResultBody(m) {
    return '<p>' + textHtml(m.body) + '</p><div class="x-result-document">' +
      richDocument(m.result || m.body) + '</div>' +
      fileCard(m.filename || T('项目更新.md', 'Project-update.md'), 'x:preview-result', m.id, 'Markdown');
  }


  function renderQuestion(m) {
    const busy = !!m.submitting;
    const isCustom = !!m.custom;
    const hasCustomText = !!(m.customText || '').trim();

    const optionsHtml = m.options.map((option, index) => {
      const isSelected = !isCustom && m.selected === index;
      return '<button type="button" class="x-choice' + (isSelected ? ' is-selected' : '') +
        '" role="radio" aria-checked="' + isSelected + '" data-action="x:select-answer" data-id="' + esc(m.id) +
        '" data-index="' + index + '" ' + (busy ? 'disabled' : '') + '><span class="x-radio">' +
        (isSelected ? '<i></i>' : '') + '</span><span class="x-choice-copy"><strong>' + esc(read(option.label)) +
        '</strong>' + (option.description ? '<small>' + esc(read(option.description)) + '</small>' : '') +
        '</span></button>';
    }).join('');

    const customOptionHtml = isCustom
      ? '<div class="x-choice x-choice-custom is-selected is-editing" role="radio" aria-checked="true">' +
        '<span class="x-radio"><i></i></span>' +
        '<div class="x-custom-input-box">' +
        '<input type="text" class="x-custom-inline-input" data-input="x:answer-text" data-id="' + esc(m.id) + '" ' +
        'value="' + esc(m.customText || '') + '" ' +
        'placeholder="' + T('输入你的回答（按 Enter 提交）…', 'Type your answer (Press Enter to submit)…') + '" ' +
        (busy ? 'disabled' : '') + ' autocomplete="off">' +
        (hasCustomText ? '<button type="button" class="x-custom-clear-btn" data-action="x:clear-custom-text" data-id="' + esc(m.id) + '" title="' + T('清空', 'Clear') + '">' + xi('close', 11) + '</button>' : '') +
        '</div></div>'
      : '<button type="button" class="x-choice x-choice-custom" role="radio" aria-checked="false" ' +
        'data-action="x:custom-answer" data-id="' + esc(m.id) + '" ' + (busy ? 'disabled' : '') + '>' +
        '<span class="x-radio"></span>' +
        '<span class="x-choice-copy"><strong>' + T('其他 · 自己填写…', 'Other · Type custom answer…') + '</strong>' +
        '<small>' + T('输入特定偏好或补充说明', 'Specify your preferences or constraints') + '</small></span></button>';

    const submitDisabled = busy || (!isCustom && m.selected == null) || (isCustom && !hasCustomText);

    return '<div class="x-decision-content" aria-busy="' + busy + '"><h3>' + esc(read(m.question)) + '</h3>' +
      '<div class="x-options" role="radiogroup" aria-label="' + esc(read(m.question)) + '">' +
      optionsHtml + customOptionHtml + '</div>' +
      '<div class="x-decision-footer">' + xb(busy ? T('正在提交…', 'Submitting…') : T('提交回答', 'Submit answer'),
        'x:submit-answer', {attrs:'data-id="' + esc(m.id) + '"', disabled:submitDisabled}) +
      '</div></div>';
  }


  function renderApproval(m) {
    const busy = !!m.submitting;
    const target = read(m.target || translated('~/Documents/项目资料/项目说明.pdf', '~/Documents/Project/project-brief.pdf'));
    return '<div class="x-decision-content" aria-busy="' + busy + '"><h3>' +
      T('允许读取这份参考文件吗？', 'Allow reading this reference file?') + '</h3><p class="x-decision-help">' +
      T('文件位于当前工作目录之外。这次允许只用于读取它。', 'This file is outside the working directory. Allowing this request permits only this read.') +
      '</p><dl class="x-approval-target"><dt>' + T('读取文件', 'Read file') + '</dt><dd>' + esc(target) +
      '</dd></dl><div class="x-decision-footer">' +
      xb(T('拒绝', 'Decline'), 'x:approval-deny', {variant:'secondary', attrs:'data-id="' + esc(m.id) + '"', disabled:busy}) +
      xb(busy ? T('正在提交…', 'Submitting…') : T('允许这次', 'Allow once'), 'x:approval-allow',
        {attrs:'data-id="' + esc(m.id) + '"', disabled:busy}) + '</div></div>';
  }


  function renderMessage(m) {
    if (m.role === 'system') return '<div class="x-system-message">' +
      xi(m.kind === 'error' ? 'warning' : 'info', 14) + '<span>' + textHtml(m.body) + '</span></div>';
    const isUser = m.role === 'user';
    const isDecision = m.kind === 'question' || m.kind === 'approval';
    let content;
    if (isDecision) {
      const title = m.kind === 'question' ? read(m.question) : read(m.body);
      const status = m.submitting ? T('提交中', 'Submitting') : m.resolved ? T('已处理', 'Resolved') :
        m.kind === 'question' ? T('待回答', 'Answer needed') : T('待确认', 'Permission needed');
      content = '<button class="x-decision-message" data-action="x:open-decision" data-id="' + esc(m.id) +
        '" ' + (m.resolved || m.submitting ? 'disabled' : '') + '><span>' + esc(title) + '</span><small>' + status + '</small></button>';
      if (m.resolved) {
        const isDenied = m.answer === 'deny';
        content += '<div class="x-receipt-pill ' + (isDenied ? 'is-denied' : 'is-approved') + '">' +
          xi(isDenied ? 'warning' : 'check', 13) +
          '<span>' + (m.kind === 'question' ? T('已回答：', 'Answered: ') + esc(read(m.answer)) :
            isDenied ? T('已拒绝这次请求', 'Declined this request') : T('已允许这次读取', 'Allowed this read')) + '</span></div>';
      }
    } else if (m.kind === 'result') content = renderResultBody(m);
    else if (m.kind === 'stream' && !read(m.body)) {
      content = '<div class="x-thinking-card">' +
        '<span class="x-thinking-indicator"><span class="x-pulse-ring"></span>' + xi('sparkles', 14) + '</span>' +
        '<span class="x-thinking-text">' + T('正在分析上下文并规划回复…', 'Analyzing context & planning reply…') + '</span>' +
        '</div>';
    }
    else content = '<div class="x-message-text"' + (m.kind === 'stream' ? ' data-stream-body="' + esc(m.id) + '"' : '') +
      '>' + textHtml(m.body) + '</div>';
    if (m.quote) content = '<button class="x-message-quote" data-action="x:jump-message" data-id="' + esc(m.quote.id || '') +
      '"><strong>' + esc(m.quote.author || bot()) + '</strong><span>' + textHtml(m.quote.body) + '</span></button>' + content;
    if (m.files) content += '<div class="x-message-files">' + m.files.map(f =>
      fileCard(f.name, 'x:preview-attachment', f.id, f.type && f.type.startsWith('image/') ? T('图片', 'Image') : sizeLabel(f.size), f.url)).join('') + '</div>';
    if (m.stopped) {
      content += '<div class="x-stopped-bar">' +
        '<span class="x-stopped-tag">' + xi('stop', 12) + '<span>' + T('未完成 · 已停止生成', 'Incomplete · Generation stopped') + '</span></span>' +
        '<div class="x-stopped-actions">' +
        '<button type="button" class="btn btn-ghost small" data-action="x:retry-message" data-id="' + esc(m.id) + '">' +
        xi('refresh', 12) + '<span>' + T('重新生成', 'Regenerate') + '</span></button>' +
        '</div></div>';
    }
    if (m.delivery === 'unknown') content += '<div class="x-delivery-warning">' + xi('warning',14) + '<span>' +
      T('发送结果未确认', 'Delivery not confirmed') + '</span></div>';
    return '<article class="x-message ' + (isUser ? 'x-message-user' : 'x-message-assistant') + '" id="x-msg-' + esc(m.id) +
      '" data-message-id="' + esc(m.id) + '">' + (!isUser ? '<div class="x-message-avatar">' + renderAvatar() + '</div>' : '') +
      '<div class="x-message-column"><div class="x-message-surface' + (isDecision ? ' x-message-surface-decision' : '') + '">' +
      content + '</div>' + ((m.kind !== 'stream' || m.stopped) && !isDecision ? messageMenu(m) : '') + '</div></article>';
  }

  function sizeLabel(size) {
    if (!size) return T('文件', 'File');
    if (size < 1024) return size + ' B';
    if (size < 1024 * 1024) return Math.round(size / 1024) + ' KB';
    return (size / (1024 * 1024)).toFixed(1) + ' MB';
  }


  function renderDecisionDock() {
    const x = defaults(), pending = pendingMessages();
    if (!pending.length) return '';
    const current = pending.find(m => m.id === x.activeDecision) || pending[0];
    const index = pending.indexOf(current);
    const title = current.kind === 'question' ? T('问题待回答', 'Answer needed') : T('操作待确认', 'Permission needed');
    return '<div class="x-decision-dock"><button class="x-decision-trigger" data-action="x:toggle-decision" aria-expanded="' +
      !!x.decisionOpen + '" aria-controls="x-decision-panel">' + xi(current.kind === 'question' ? 'chat' : 'shield',14) +
      '<span>' + title + (pending.length > 1 ? ' · ' + (index + 1) + '/' + pending.length : '') + '</span>' +
      xi('down',13) + '</button>' + (x.decisionOpen ? '<section id="x-decision-panel" class="x-decision-panel" role="dialog" aria-label="' +
        T('待处理决定', 'Pending decision') + '"><header class="x-decision-panel-header"><span>' + title + '</span><div>' +
        (pending.length > 1 ? iconButton('x:decision-prev','back',T('上一个请求','Previous request'), index === 0 ? 'disabled' : '') +
          iconButton('x:decision-next','chevron',T('下一个请求','Next request'), index === pending.length-1 ? 'disabled' : '') : '') +
        iconButton('x:close-decision','close',T('收起请求，不作决定','Dismiss without deciding')) + '</div></header>' +
        (current.kind === 'question' ? renderQuestion(current) : renderApproval(current)) + '</section>' : '') + '</div>';
  }

  function renderAttachmentMenu() {
    const x = defaults();
    if (!x.addMenuOpen) return '';

    const botPlugins = [
      { id: 'notion', name: 'Notion', desc: ['Find and update pages in your Notion workspace', 'Find and update pages in your Notion workspace'] },
      { id: 'amap', name: T('高德地图', 'Amap'), desc: ['查找地点、路线和天气', 'Find places, routes and weather'] },
      { id: 'tencent', name: T('腾讯文档', 'Tencent Docs'), desc: ['创建、查询和编辑在线文档', 'Create, search and edit online docs'] }
    ];

    const codexPlugins = [
      { id: 'documents', name: 'Documents', desc: ['Create and edit documents', 'Create and edit documents'] },
      { id: 'pdf', name: 'PDF', desc: ['Read, create, and verify PDFs', 'Read, create, and verify PDFs'] },
      { id: 'presentations', name: 'Presentations', desc: ['Create and edit presentations', 'Create and edit presentations'] },
      { id: 'codex-tools', name: 'codex-app-tools', desc: ['', ''] },
      { id: 'browser', name: 'Browser', desc: ['Control the in-app browser', 'Control the in-app browser'] }
    ];

    const renderRow = (p) => {
      const descText = p.desc ? T(p.desc[0], p.desc[1]) : '';
      return '<button role="menuitem" class="attachment-menu-row" data-action="x:mention-select" data-name="' + esc(p.name) + '">' +
        xi('plugin', 17) +
        '<span class="attachment-menu-copy"><strong>' + esc(p.name) + '</strong>' +
        (descText ? '<small>' + esc(descText) + '</small>' : '') + '</span>' +
        '</button>';
    };

    return '<section class="x-attachment-menu" id="x-attachment-menu" role="menu" aria-label="' + T('附件与插件', 'Attachments & plugins') + '">' +
      '<button role="menuitem" class="attachment-menu-row attachment-file" data-action="x:choose-files">' +
      xi('attachment', 17) +
      '<span class="attachment-menu-copy"><strong>' + T('添加文件', 'Add file') + '</strong></span>' +
      '</button>' +
      '<div class="attachment-menu-group"><p class="menu-caption">' + T('插件 · Bot', 'Plugins · Bot') + '</p>' +
      botPlugins.map(renderRow).join('') + '</div>' +
      '<div class="attachment-menu-group"><p class="menu-caption">' + T('插件 · Codex', 'Plugins · Codex') + '</p>' +
      codexPlugins.map(renderRow).join('') + '</div>' +
      '</section>';
  }

  function renderComposer() {
    const x = defaults();
    const stopping = x.running && !x.draft.trim();
    let notice = '';
    if (x.offline || x.deliveryUnknown) notice = '<div class="x-connection-feedback" role="status">' + xi('warning',14) + '<span>' +
      (x.deliveryUnknown ? T('发送未确认，先检查原消息的状态。','Delivery is unconfirmed. Check the original message first.') :
        T('连接暂时不可用，草稿已保留。','Connection unavailable. Your draft is kept.')) + '</span>' +
      '<button class="x-text-action" data-action="' + (x.deliveryUnknown ? 'x:resolve-unknown' : 'x:reconnect') + '">' +
      (x.deliveryUnknown ? T('检查状态','Check status') : T('重新连接','Reconnect')) + '</button></div>';
    return '<div class="x-composer-area"><button class="x-jump-latest" id="x-jump-latest" data-action="x:latest" hidden>' +
      xi('down',14) + T('回到最新','Jump to latest') + '</button>' + notice + renderDecisionDock() +
      '<div class="x-composer" id="x-composer" aria-label="' + T('消息输入区','Message composer') + '">' +
      (x.quote ? '<div class="x-composer-quote">' + xi('quote',14) + '<div><strong>' + esc(x.quote.author) +
        '</strong><p>' + textHtml(x.quote.body) + '</p></div>' + iconButton('x:clear-quote','close',T('取消引用','Remove quote')) + '</div>' : '') +
      (x.attachments.length ? '<div class="x-attachment-tray">' + x.attachments.map(f => '<div class="x-attachment-chip">' +
        '<button data-action="x:preview-attachment" data-id="' + esc(f.id) + '" class="x-attachment-preview">' +
        (f.url && f.type && f.type.startsWith('image/') ? '<img src="' + esc(f.url) + '" alt="">' : xi('file',18)) +
        '<span><strong>' + esc(f.name) + '</strong><small>' + sizeLabel(f.size) + '</small></span></button>' +
        iconButton('x:remove-attachment','close',T('移除附件','Remove attachment') + ' ' + f.name,'data-id="' + esc(f.id) + '"') + '</div>').join('') + '</div>' : '') +
      '<div class="x-compose-line"><button type="button" class="x-icon-button x-add-button" data-action="x:add-menu" aria-label="' +
      T('添加材料','Add material') + '" aria-haspopup="menu" aria-expanded="' + !!x.addMenuOpen + '" aria-controls="x-attachment-menu">' + xi('plus',18) +
      '</button><textarea id="x-chat-input" rows="1" data-input="x:draft" aria-label="' + T('消息','Message') + '" placeholder="' +
      (x.running ? T('补充说明…','Add more context…') : T('写下想法…','Write a message…')) + '" title="' +
      T('Enter 发送，Shift + Enter 换行','Enter to send, Shift + Enter for a new line') + '">' + esc(x.draft) + '</textarea>' +
      '<button type="button" id="x-send-button" class="x-send-button' + (stopping ? ' is-stop' : '') + '" data-action="' +
      (stopping ? 'x:stop' : 'x:send') + '" aria-label="' + (stopping ? T('停止当前回复','Stop this response') : T('发送消息','Send message')) +
      '" ' + (!x.running && !x.draft.trim() && !x.attachments.length ? 'disabled' : '') + '>' + xi(stopping ? 'stop' : 'send',18) +
      '</button></div>' + renderAttachmentMenu() + '<input type="file" id="x-file-input" multiple hidden>' +
      '<div class="x-drop-mask">' + xi('attachment',24) + '<strong>' + T('松开以添加文件','Drop to add files') + '</strong></div>' +
      '</div></div>';
  }


  function renderScenarioControls() {
    const x = defaults();
    const scenarios = [['normal','日常对话','Conversation'],['stream','流式回复','Streaming'],['question','问题选择','Question'],
      ['approval','操作审批','Approval'],['attachments','附件与引用','Attachment & quote'],['offline','离线','Offline'],['unknown','发送未确认','Unconfirmed']];
    return '<label class="review-scenario"><span>' + T('状态预览','Preview state') + '</span><select data-bind="x:scenario" aria-label="' +
      T('聊天状态预览','Chat state preview') + '">' + scenarios.map(([id,zh,en]) => '<option value="' + id + '" ' +
      (x.scenario === id ? 'selected' : '') + '>' + T(zh,en) + '</option>').join('') + '</select></label>';
  }


  function renderChat() {
    const x = defaults();
    return '<div class="x-chat-layout"><main class="x-chat-scroll" id="x-chat-scroll" aria-label="' +
      T('与助手的对话','Conversation with your assistant') + '"><div class="x-chat-messages">' +
      '<div class="x-history-nav"><button data-action="x:load-earlier" ' + (x.earlierLoaded ? 'hidden' : '') + '>' +
      T('更早的消息','Earlier messages') + '</button></div><div class="x-date-divider">' + T('今天','Today') + '</div>' +
      (!x.connected ? '<div class="x-message x-message-assistant"><div class="x-message-avatar">' + renderAvatar() +
        '</div><div class="x-message-column"><div class="x-message-surface"><p>' + T('选择 AI 连接后，就可以开始聊天。','Choose an AI connection to start chatting.') +
        '</p><button class="x-text-action" data-action="x:connect-ai">' + T('连接设置…','Connection settings…') + '</button></div></div></div>' :
        x.messages.map(m => renderMessage(m)).join('')) + '<div id="x-chat-end"></div></div></main>' + renderComposer() + '</div>';
  }

  function refreshChat(scroll) {
    render();
    if (scroll !== false) scrollToEnd();
  }

  function scrollToEnd(smooth) {
    requestAnimationFrame(() => {
      const panel = document.getElementById('x-chat-scroll');
      if (panel) panel.scrollTo({ top: panel.scrollHeight, behavior: smooth ? 'smooth' : 'auto' });
    });
  }

  function stopStream(renderNow) {
    const x = defaults();
    if (streamTimer) clearInterval(streamTimer);
    streamTimer = null;
    const m = x.messages.find(item => item.id === x.streamingId);
    if (m && x.running) {
      m.stopped = true;
      if (!read(m.body)) m.body = translated('这次回复已停止。', 'This response was stopped.');
    }
    x.running = false;
    x.streamingId = null;
    x.petState = 'idle';
    save();
    if (renderNow !== false) refreshChat();
  }

  function streamReply(zh, en, options) {
    const x = defaults();
    if (x.running) stopStream(false);
    const opts = options || {};
    const m = addMessage({ role: 'assistant', kind: 'stream', body: translated('', '') });
    x.running = true;
    x.streamingId = m.id;
    x.petState = 'working';
    let zhPos = 0;
    let enPos = 0;
    let ticks = 0;
    refreshChat();
    streamTimer = setInterval(() => {
      ticks++;
      if (!x.running || x.streamingId !== m.id) { clearInterval(streamTimer); streamTimer = null; return; }
      if (ticks < 5) return;
      zhPos = Math.min(zh.length, zhPos + 2);
      enPos = Math.min(en.length, enPos + 5);
      m.body = translated(zh.slice(0, zhPos), en.slice(0, enPos));
      const article = document.getElementById('x-msg-' + m.id);
      const scroller = document.getElementById('x-chat-scroll');
      const nearEnd = !scroller || scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 120;
      if (article) {
        const surface = article.querySelector('.x-message-surface');
        if (surface) surface.innerHTML = '<div class="x-message-text" data-stream-body="' + esc(m.id) + '">' + textHtml(m.body) + '</div>';
      }
      if (nearEnd && scroller) scroller.scrollTop = scroller.scrollHeight;
      if (zhPos >= zh.length && enPos >= en.length) {
        clearInterval(streamTimer);
        streamTimer = null;
        x.running = false;
        x.streamingId = null;
        x.petState = 'done';
        m.kind = opts.result ? 'result' : 'text';
        if (opts.result) {
          m.filename = T('项目更新.md', 'Project-update.md');
          m.result = translated('## 项目更新\n\n' + zh, '## Project update\n\n' + en);
        }
        save();
        refreshChat(nearEnd);
      }
    }, 55);
  }

  function sendMessage() {
    const x = defaults();
    const input = document.getElementById('x-chat-input');
    if (input) x.draft = input.value;
    const draft = x.draft.trim();
    if (!draft && !x.attachments.length) return;
    if (!x.connected) { showToast('先连接 AI，草稿会为你保留。', 'Connect AI first. Your draft will be kept.'); return; }
    if (x.offline) { save(); refreshChat(false); return; }
    if (x.deliveryUnknown) { showToast('请先检查上一条消息是否送达。', 'Please check the previous message’s delivery first.'); return; }
    const wasRunning = x.running;
    addMessage({ role: 'user', kind: 'text', body: draft || translated('看看这些材料。', 'Take a look at these files.'), files: x.attachments.length ? x.attachments.slice() : null, quote: x.quote || null });
    x.draft = '';
    x.quote = null;
    x.attachments = [];
    save();
    if (wasRunning) {
      addMessage({ role: 'system', kind: 'info', body: translated('补充说明已收到，会一起考虑。', 'Your added context is received and will be considered.') });
      refreshChat();
      return;
    }
    if (/问题|选择|问我|question|choose|ask me/i.test(draft)) {
      addQuestion(); refreshChat(); return;
    }
    if (/审批|权限|授权|approve|permission/i.test(draft)) {
      addApproval(); refreshChat(); return;
    }
    if (/周报|更新|报告|update|report/i.test(draft)) {
      streamReply('我来整理。先把已经完成的事情、仍在推进的工作和下一步分开。\n\n今天的重点是让主要使用路径更清楚：聊天负责交代事情和查看结果，设置按使用目的组织，复杂连接流程逐步展开。\n\n明天可以继续检查文件预览和任务恢复的体验。', 'I’ll organize it into completed work, work in progress, and next steps.\n\nToday’s focus is making the main paths clearer: use chat to give instructions and see results, organize settings around your goals, and reveal connection details one step at a time.\n\nTomorrow, review file previews and task recovery.', { result: true });
    } else {
      streamReply('好，我收到啦。\n\n我会先理清你的目标和现有材料，再把过程中的重要进展与结果放在这段对话里。如果需要你做决定，我会在这里明确提出来。\n\n你也可以继续补充偏好或把相关文件拖进来。', 'Got it.\n\nI’ll start with your goal and the material you’ve shared, then keep the useful progress and results in this conversation. If a decision needs you, I’ll ask clearly here.\n\nYou can add preferences or drag in any relevant files.');
    }
  }

  function addQuestion() {
    defaults().decisionOpen = true;
    return addMessage({ role: 'assistant', kind: 'question', question: translated('这次整理哪些内容？', 'What should this update cover?'), options: [
      { label: translated('开发相关', 'Development work'), description: translated('功能、修复和下一步计划。', 'Features, fixes, and next steps.') },
      { label: translated('全部项目', 'All projects'), description: translated('连同调研、设计与其他工作。', 'Include research, design, and other work.') }
    ], selected: null });
  }


  function addApproval() {
    defaults().decisionOpen = true;
    return addMessage({role:'assistant',kind:'approval',body:translated('需要读取你提供的参考文件。','I need to read the reference file you provided.'),
      target:translated('~/Documents/项目资料/项目说明.pdf','~/Documents/Project/project-brief.pdf')});
  }

  function setScenario(scenario) {
    const x = defaults();
    if (x.running) stopStream(false);
    x.scenario = scenario;
    x.addMenuOpen = false;
    x.decisionOpen = false;
    x.activeDecision = null;
    x.offline = false;
    x.deliveryUnknown = false;
    setConnected(true);
    x.quote = null;
    x.attachments = [];
    x.draft = '';
    x.messages = starterMessages().slice(0, 1);
    x.earlierLoaded = false;
    if (scenario === 'normal') x.messages = starterMessages();
    if (scenario === 'stream') {
      addMessage({ role: 'user', kind: 'text', body: translated('帮我把今天的进展整理一下，再列出明天值得先做的事。', 'Summarize today’s progress and suggest what to do first tomorrow.') });
      streamReply('我先把手头的信息放到一起。\n\n今天完成了聊天主流程、AI 连接和设置导航的梳理。已经确定的内容包括：把账号和模型放在同一个入口；将复杂选项放到需要时再展开；让等待回答、执行审批和普通消息有各自清晰的状态。\n\n明天的第一件事，是把附件、结果文件和远程连接的异常路径再走一遍。它们决定了工作在不顺利的时候，能不能继续。\n\n这份更新可以控制在三段以内，保留具体变化和下一步就够了。', 'I’m bringing the available notes together.\n\nToday we reviewed the main chat flow, AI connections, and settings navigation. The decisions so far are to put accounts and models in one place, reveal advanced options when needed, and give questions, approvals, and regular messages clear states.\n\nTomorrow, first walk through attachments, result files, and remote connection errors. Those paths determine whether work can continue when things do not go smoothly.\n\nThe update can stay under three paragraphs: concrete changes and next steps are enough.');
      return;
    }
    if (scenario === 'question') {
      addMessage({ role: 'user', kind: 'text', body: translated('整理一份本周更新，先问我想覆盖哪些内容。', 'Prepare a weekly update. First ask me what it should cover.') });
      addQuestion();
    }
    if (scenario === 'approval') {
      addMessage({ role: 'user', kind: 'text', body: translated('读取文稿里的项目说明，再整理这周的更新。', 'Read the brief in Documents, then prepare this week’s update.') });
      addApproval();
    }
    if (scenario === 'attachments') {
      x.messages = starterMessages();
      x.quote = { id: 'sample-result', author: bot(), body: translated('明天可以继续检查附件预览和任务恢复的体验。', 'Tomorrow, review attachment previews and task recovery.') };
      x.draft = T('重点看看这张图里的间距和层级。', 'Please look at the spacing and hierarchy in this image.');
      x.attachments = [{ id: 'sample-image', name: T('设置参考.png', 'Settings-reference.png'), type: 'image/png', size: 200657, url: 'assets/settings-reference.png', sample: true }];
    }
    if (scenario === 'offline') {
      x.messages = starterMessages();
      x.offline = true;
      x.draft = T('再加上明天的优先事项。', 'Please add tomorrow’s priorities.');
    }
    if (scenario === 'unknown') {
      x.messages = starterMessages();
      addMessage({ role: 'user', kind: 'text', body: translated('可以，帮我保存这份更新。', 'That works. Please save this update.'), delivery: 'unknown' });
      x.deliveryUnknown = true;
    }
    save();
    refreshChat();
  }

  async function addFiles(files) {
    const x = defaults();
    for (const file of Array.from(files || [])) {
      const id = 'file-' + (++x.sequence);
      const item = { id, name: file.name || T('粘贴的图片.png', 'Pasted-image.png'), type: file.type || 'application/octet-stream', size: file.size || 0, url: file.type && file.type.startsWith('image/') ? URL.createObjectURL(file) : null };
      localFiles.set(id, file);
      x.attachments.push(item);
    }
    save();
    refreshChat(false);
    focusInput();
  }

  function findAttachment(id) {
    const x = defaults();
    return x.attachments.find(f => f.id === id) || x.messages.flatMap(m => m.files || []).find(f => f.id === id);
  }

  function previewAttachment(id) {
    const file = findAttachment(id);
    if (!file) return;
    openModal(() => ({ title: file.name, subtitle: file.type && file.type.startsWith('image/') ? T('图片预览', 'Image preview') : sizeLabel(file.size), width: 'large', body: file.url ? '<div class="x-image-preview"><img src="' + esc(file.url) + '" alt="' + esc(file.name) + '"></div>' : '<div class="x-file-preview-empty">' + xi('file', 48) + '<h3>' + esc(file.name) + '</h3><p>' + T('文件已添加，可以与消息一起交给助手。', 'The file is attached and can be sent with your message.') + '</p></div>', footer: xb(T('关闭', 'Close'), 'x:close-modal', { variant: 'secondary' }) }));
  }

  function richDocument(raw) {
    return esc(read(raw)).split('\n').map(line => line.startsWith('## ') ? '<h3>' + line.slice(3) + '</h3>' : line.startsWith('- ') ? '<p class="x-document-bullet">• ' + line.slice(2) + '</p>' : '<p>' + (line || '<br>') + '</p>').join('');
  }

  function previewResult(id) {
    const m = defaults().messages.find(item => item.id === id);
    if (!m) return;
    openModal(() => ({ title: m.filename || T('项目更新.md', 'Project-update.md'), subtitle: T('结果预览', 'Result preview'), width: 'large', body: '<div class="x-document-preview">' + richDocument(m.result || m.body) + '</div>', footer: xb(T('复制内容', 'Copy text'), 'x:copy-result', { variant: 'secondary', icon: 'copy', attrs: 'data-id="' + esc(id) + '"' }) + xb(T('下载文件', 'Download file'), 'x:download-result', { icon: 'download', attrs: 'data-id="' + esc(id) + '"' }) }));
  }

  function taskStatus(task) {
    return task.status === 'done' ? T('已完成', 'Complete') : task.status === 'waiting' ? T('等你决定', 'Waiting for you') : task.status === 'stopped' ? T('已停止', 'Stopped') : T('正在工作', 'Working');
  }

  function renderTask(task, inDesktop) {
    const visible = defaults().tasks.filter(t => t.visible);
    const position = visible.findIndex(t => t.id === task.id);
    return '<article class="x-task-card' + (task.pinned ? ' is-pinned' : '') + '" draggable="true" data-task-id="' + esc(task.id) + '"><div class="x-task-card-top"><span class="x-task-symbol">' + xi(task.status === 'done' ? 'check' : task.status === 'waiting' ? 'bell' : 'terminal', 18) + '</span><span class="x-task-status' + (task.status === 'done' ? ' is-done' : task.status === 'waiting' ? ' is-waiting' : '') + '">' + taskStatus(task) + '</span><div class="x-task-actions"><span class="x-task-order">' + iconButton('x:move-task-up', 'down', T('向上移动任务卡', 'Move task card up'), 'data-id="' + esc(task.id) + '" ' + (position === 0 ? 'disabled' : '')) + iconButton('x:move-task-down', 'down', T('向下移动任务卡', 'Move task card down'), 'data-id="' + esc(task.id) + '" ' + (position === visible.length - 1 ? 'disabled' : '')) + '</span>' + iconButton('x:pin-task', 'pin', task.pinned ? T('取消固定', 'Unpin task') : T('固定到桌面', 'Pin to desktop'), 'data-id="' + esc(task.id) + '" aria-pressed="' + task.pinned + '"') + iconButton('x:remove-task', 'close', T('从桌面移除，不会停止任务', 'Remove from desktop; task keeps running'), 'data-id="' + esc(task.id) + '"') + '</div></div><button class="x-task-content" data-action="x:observe-task" data-id="' + esc(task.id) + '"><strong>' + esc(read(task.title)) + '</strong><span>' + esc(read(task.detail)) + '</span></button><div class="x-task-card-bottom"><span>' + xi('machine', 14) + esc(read(task.machine)) + '</span>' + (task.status === 'done' ? '<button data-action="x:task-result" data-id="' + esc(task.id) + '">' + T('查看结果', 'View result') + xi('arrowRight', 14) + '</button>' : '<button data-action="x:observe-task" data-id="' + esc(task.id) + '">' + (task.status === 'waiting' ? T('去回答', 'Answer') : T('观察任务', 'Observe')) + xi('arrowRight', 14) + '</button>') + '</div></article>';
  }

  function reorderTask(id, targetId) {
    const tasks = defaults().tasks;
    const from = tasks.findIndex(t => t.id === id);
    const to = tasks.findIndex(t => t.id === targetId);
    if (from < 0 || to < 0 || from === to) return;
    const task = tasks.splice(from, 1)[0];
    tasks.splice(to, 0, task);
    save();
    if (state.view === 'desktop') render(); else tasksModal();
  }

  function tasksModal() {
    openModal(() => ({ title: T('后台任务', 'Background tasks'), subtitle: T('关闭面板后，正在进行的工作会继续。', 'Work continues when you close this panel.'), body: '<div class="x-task-list">' + defaults().tasks.filter(t => t.visible).map(t => renderTask(t, false)).join('') + (defaults().tasks.every(t => !t.visible) ? '<div class="empty-state">' + xi('clock', 36) + '<h3>' + T('桌面上没有任务卡', 'No task cards on the desktop') + '</h3><p>' + T('移除卡片不会停止工作，结果仍会回到聊天中。', 'Removing a card does not stop work. Results still return to chat.') + '</p></div>' : '') + '</div>', footer: xb(T('回到聊天', 'Back to chat'), 'x:close-modal', { variant: 'secondary' }) }));
  }

  function observeTask(id) {
    const task = defaults().tasks.find(t => t.id === id);
    if (!task) return;
    if (task.status === 'waiting') { navigate('chat'); setScenario('question'); return; }
    openModal(() => ({ title: T('观察任务', 'Observe task'), subtitle: read(task.title), width: 'large', body: '<div class="x-terminal-preview"><div class="x-terminal-heading">' + xi('terminal', 16) + '<span>' + T('外部终端预览', 'External terminal preview') + '</span><span>' + T('演示', 'DEMO') + '</span></div><div class="x-terminal-output"><p><span class="x-terminal-prompt">›</span> ' + esc(read(task.title)) + '</p><p class="x-terminal-muted">' + T('正在沿用这项工作的原始执行连接。', 'Observing the original connection for this task.') + '</p><p><span class="x-terminal-success">✓</span> ' + T('读取任务材料', 'Read task materials') + '</p><p><span class="x-terminal-success">✓</span> ' + T('整理最近的变更', 'Organize recent changes') + '</p><p><span class="x-terminal-prompt">…</span> ' + esc(read(task.detail)) + '</p><span class="x-terminal-caret"></span></div></div><p class="x-modal-note">' + T('实际应用会使用你偏好的外部终端观察同一任务。关闭观察窗口不会停止工作。', 'The app uses your preferred external terminal to observe this same task. Closing the window does not stop it.') + '</p>', footer: xb(T('关闭观察', 'Close preview'), 'x:close-modal', { variant: 'secondary' }) }));
  }

  function petBubble() {
    const x = defaults();
    if (x.petState === 'idle') return '';
    const stateText = x.petState === 'working' ? T('正在整理项目更新…', 'Organizing the project update…') : x.petState === 'waiting' ? T('有个小问题，需要你决定。', 'A quick choice needs you.') : T('项目更新整理好了。', 'Your project update is ready.');
    return '<div class="x-pet-bubble"><div class="x-pet-bubble-head">' + (x.petState === 'working' ? '<span class="x-state-dot is-active"></span>' : xi(x.petState === 'waiting' ? 'bell' : 'check', 15)) + '<span>' + esc(bot()) + '</span>' + iconButton('x:hide-bubble', 'close', T('收起气泡', 'Dismiss bubble')) + '</div><p>' + stateText + '</p><button data-action="' + (x.petState === 'waiting' ? 'x:pet-answer' : 'x:open-chat') + '">' + (x.petState === 'waiting' ? T('去回答', 'Answer') : T('打开聊天', 'Open chat')) + xi('arrowRight', 14) + '</button></div>';
  }

  function renderPinnedImage() {
    const p = defaults().pinnedImage;
    if (!p) return '';
    const scale = Number(p.scale || 1);
    return '<aside class="x-pinned-image" aria-label="' + T('桌面贴图', 'Pinned image') + '" style="--x-pin-scale:' + scale + '"><div class="x-pinned-image-bar"><span>' + xi('pin', 12) + T('贴图', 'Pinned image') + '</span><div><button data-action="x:pin-zoom-out" aria-label="' + T('缩小图片', 'Zoom out') + '">−</button><span>' + Math.round(scale * 100) + '%</span><button data-action="x:pin-zoom-in" aria-label="' + T('放大图片', 'Zoom in') + '">+</button>' + iconButton('x:close-pinned', 'close', T('关闭贴图', 'Close pinned image')) + '</div></div><div class="x-pinned-image-content"><img src="' + esc(p.url) + '" alt="' + esc(p.name || T('贴图预览', 'Pinned image preview')) + '"></div></aside>';
  }

  function pinImage(url) {
    const x = defaults();
    x.pinnedImage = { url: url || captureDataURL(), name: T('截图', 'Screenshot'), scale: 1 };
    closeModal();
    save();
    navigate('desktop');
    showToast('图片已贴到桌面。关闭贴图不会影响其他工作。', 'Image pinned. Closing it does not affect other work.');
  }

  function renderDesktop() {
    const x = defaults();
    return '<div class="x-desktop-layout"><div class="x-desktop-menubar"><div><span class="x-desktop-brand">' + xi('general', 15) + '</span><strong>Caelis Bot</strong><button data-action="x:desktop-menu">' + T('菜单', 'Menu') + '</button></div><span>' + T('周一 09:41', 'Mon 09:41') + '</span></div><div class="x-desktop-scene"><div class="x-desktop-ambient"><span>' + T('少一点打扰。', 'Less noise.') + '</span><span>' + T('多一点陪伴。', 'More presence.') + '</span></div><div class="x-desktop-caption"><span class="x-caption-line"></span><p>' + T('桌面上的小小陪伴。<br>需要时，随时开始。', 'A quiet presence on your desktop.<br>Ready whenever you are.') + '</p></div>' + renderPinnedImage() + '<div class="x-desktop-tasks">' + x.tasks.filter(task => task.visible).map(task => renderTask(task, true)).join('') + '</div>' + (x.petVisible ? '<div class="x-pet-anchor" id="x-pet-anchor" style="--x-pet-x:' + Number(x.petX || 0) + 'px;--x-pet-y:' + Number(x.petY || 0) + 'px;--x-pet-scale:' + Math.max(.65, Math.min(1.6, Number((state.settings || {}).petSize || 100) / 100)) + '">' + petBubble() + '<button class="x-pet" id="x-pet" data-action="x:open-chat" aria-label="' + T('点击角色打开聊天，可拖动角色', 'Open chat with your assistant; drag to move the character') + '">' + petGraphic(bot()) + '</button><div class="x-pet-shadow"></div><span class="x-pet-name">' + esc(bot()) + '</span></div>' : '<button class="x-show-pet" data-action="x:show-pet">' + renderAvatar() + '<span>' + T('显示角色', 'Show companion') + '</span></button>') + '<div class="x-desktop-dock"><button data-action="x:open-chat" aria-label="' + T('打开聊天', 'Open chat') + '">' + renderAvatar() + '</button><button data-action="x:tasks" aria-label="' + T('后台任务', 'Background tasks') + '">' + xi('clock', 25) + '</button><button data-action="x:screenshot" aria-label="' + T('截图', 'Screenshot') + '">' + xi('image', 25) + '</button><button data-action="x:chat-settings" aria-label="' + T('设置', 'Settings') + '">' + xi('settings', 25) + '</button></div></div><aside class="x-demo-strip x-desktop-demo"><span class="x-demo-caption">' + T('桌面状态', 'DESKTOP SCENARIOS') + '</span><div class="x-demo-options">' + [['idle', T('安静待机', 'Idle')], ['working', T('正在工作', 'Working')], ['waiting', T('需要回答', 'Needs you')], ['done', T('工作完成', 'Complete')]].map(item => '<button data-action="x:pet-state" data-state="' + item[0] + '" class="' + (x.petState === item[0] ? 'is-active' : '') + '">' + item[1] + '</button>').join('') + '<button data-action="x:screenshot">' + T('截图标注', 'Screenshot editor') + '</button><button data-action="x:restore-tasks">' + T('恢复任务卡', 'Restore cards') + '</button></div></aside></div>';
  }

  function permissionRows(setup) {
    const rows = [
      ['notifications', 'bell', T('通知', 'Notifications'), T('提醒你查看结果或处理请求。', 'Let you know when a result or decision needs you.')],
      ['screen', 'eye', T('屏幕录制', 'Screen Recording'), T('用于截图和你交给它的桌面任务。', 'For screenshots and desktop tasks you request.')],
      ['accessibility', 'general', T('辅助功能', 'Accessibility'), T('让它按任务读取界面、点击和输入。', 'Read interfaces, click, and type for your tasks.')]
    ];
    return '<div class="x-onboard-permissions">' + rows.map(row => '<div class="x-permission-row"><span class="x-permission-icon">' + xi(row[1], 22) + '</span><div><strong>' + row[2] + '</strong><p>' + row[3] + '</p></div>' + (setup.permissions[row[0]] ? badge(T('已开启', 'Enabled'), 'success') : xb(T('开启', 'Enable'), 'x:setup-permission', { variant: 'secondary', attrs: 'data-permission="' + row[0] + '"' })) + '</div>').join('') + '</div>';
  }

  function setupContent() {
    const s = defaults().setup;
    const name = s.name || bot();
    if (s.step === 0) return '<div class="x-onboard-intro-art">' + renderAvatar('hero') + '<span class="x-onboard-spark"></span></div><span class="x-eyebrow">' + T('认识你的助手', 'MEET YOUR ASSISTANT') + '</span><h1>' + T('很高兴认识你', 'It’s nice to meet you') + '</h1><p class="x-onboard-subtitle">' + T('先给你的助手取个名字。', 'Start with a name for your assistant.') + '</p><div class="x-onboard-fields">' + field(T('名字', 'Name'), '<input id="x-setup-name" data-input="x:setup-name" value="' + esc(s.name || '') + '" maxlength="40" placeholder="' + T('你想怎么称呼它', 'What would you like to call it?') + '">') + field(T('希望它怎样与你相处', 'How should it spend time with you?') + ' <span class="muted">' + T('可选', 'Optional') + '</span>', '<textarea rows="3" data-input="x:setup-description" placeholder="' + T('说话简洁，喜欢分享有趣的发现。', 'Keep things concise. Share interesting discoveries.') + '">' + esc(s.description || '') + '</textarea>') + '<p class="x-field-note">' + T('这段介绍会在连接后，作为第一条消息交给它。', 'After connecting, this introduction becomes your first message to it.') + '</p></div>';
    if (s.step === 1) return '<span class="x-onboard-step-symbol">' + xi('ai', 30) + '</span><span class="x-eyebrow">' + T('连接 AI', 'CONNECT AI') + '</span><h1>' + T('让', 'Let ') + esc(name) + T('开始帮忙', ' get started') + '</h1><p class="x-onboard-subtitle">' + T('选择你已有的账号或模型服务。', 'Choose an account or model service you already use.') + '</p><div class="x-onboard-provider-list"><button class="x-provider-choice" data-action="x:choose-provider" data-provider="codex"><span class="x-provider-icon x-provider-codex">' + xi('ai', 24) + '</span><span><strong>' + T('使用 ChatGPT 账号', 'Use a ChatGPT account') + '</strong><small>' + T('通过 Codex 连接', 'Connect through Codex') + '</small></span>' + xi('chevron', 20) + '</button><button class="x-provider-choice" data-action="x:choose-provider" data-provider="caelis"><span class="x-provider-icon">' + xi('key', 23) + '</span><span><strong>' + T('使用自己的模型服务', 'Use your own model service') + '</strong><small>' + T('通过 Caelis 连接', 'Connect through Caelis') + '</small></span>' + xi('chevron', 20) + '</button></div><p class="x-field-note">' + T('可以先完成设置，之后随时连接。', 'You can finish setup now and connect whenever you are ready.') + '</p>';
    if (s.step === 2) {
      if (s.provider === 'caelis') return '<span class="x-onboard-step-symbol">' + xi('key', 30) + '</span><span class="x-eyebrow">' + T('连接模型服务', 'MODEL SERVICE') + '</span><h1>' + T('用你熟悉的 AI', 'Bring the AI you know') + '</h1><p class="x-onboard-subtitle">' + T('先连接一个服务，之后可以在设置中管理模型。', 'Connect one service now. You can manage models in Settings later.') + '</p><div class="x-onboard-fields"><div class="notice">' + T('原型使用演示凭据，不会连接真实服务。', 'This prototype uses demo credentials and does not connect to a real service.') + '</div>' + field(T('模型服务', 'Model service'), '<select data-bind="x:setup-service"><option>OpenAI</option><option>Anthropic</option><option>' + T('兼容 OpenAI 的服务', 'OpenAI-compatible service') + '</option></select>') + field('API Key', '<input type="password" value="demo-prototype-key" autocomplete="off" aria-label="API Key">', T('保存在这台电脑的配置中。', 'Stored in the configuration on this computer.')) + '</div>';
      return '<span class="x-onboard-step-symbol x-onboard-success">' + xi(s.loginWaiting ? 'globe' : 'check', 30) + '</span><span class="x-eyebrow">' + T('CHATGPT 账号', 'CHATGPT ACCOUNT') + '</span><h1>' + (s.loginWaiting ? T('完成登录，就能继续', 'Finish signing in to continue') : T('可以直接开始', 'You’re ready to connect')) + '</h1><p class="x-onboard-subtitle">' + (s.loginWaiting ? T('登录页面已准备好。这个原型不会打开真实账号页面。', 'The sign-in step is ready. This prototype does not open a real account page.') : T('已找到这台电脑上的 Codex 和 ChatGPT 账号。', 'Codex and a ChatGPT account were found on this computer.')) + '</p><div class="x-account-ready"><span class="x-provider-icon x-provider-codex">' + xi('ai', 24) + '</span><div><strong>ChatGPT</strong><p>' + T('通过 Codex 连接', 'Connect through Codex') + '</p></div>' + badge(s.loginWaiting ? T('等待登录', 'Awaiting sign-in') : T('已就绪', 'Ready'), s.loginWaiting ? 'warning' : 'success') + '</div><button class="x-text-action" data-action="x:setup-manage-account">' + T('使用其他账号', 'Use a different account') + '</button><details class="x-onboard-details"><summary>' + T('安装与连接详情', 'Installation and connection details') + '</summary><p>Codex 0.162.1 · ' + T('已安装', 'Installed') + '</p><p>' + T('连接后可在设置中更换账号、工具和模型。', 'After connecting, you can change your account, tool, and model in Settings.') + '</p></details>';
    }
    if (s.step === 3) return '<span class="x-onboard-step-symbol">' + xi('image', 30) + '</span><span class="x-eyebrow">' + T('桌面工具', 'DESKTOP TOOLS') + '</span><h1>' + T('顺手的小工具', 'A few useful little tools') + '</h1><p class="x-onboard-subtitle">' + T('截图、标注，或把图片留在桌面上。', 'Capture, annotate, or keep an image on your desktop.') + '</p><div class="x-onboard-tools"><div class="x-onboard-toggle-row"><div><strong>' + T('启用截屏与贴图', 'Enable screenshots and image pinning') + '</strong><p>' + T('之后可以在设置中更改。', 'You can change this later in Settings.') + '</p></div>' + toggle('x:setup-tools', s.tools, T('启用截屏与贴图', 'Enable screenshots and image pinning')) + '</div><div class="x-onboard-tool-row"><span>' + xi('image', 22) + T('截图与标注', 'Screenshot and annotate') + '</span><kbd>F1</kbd></div><div class="x-onboard-tool-row"><span>' + xi('pin', 22) + T('桌面贴图', 'Pin an image to your desktop') + '</span><kbd>F3</kbd></div></div><p class="x-field-note">' + T('关闭这些工具，仍可在聊天中添加图片和文件。', 'You can still attach images and files in chat when these tools are off.') + '</p>';
    if (s.step === 4) return '<span class="x-onboard-step-symbol">' + xi('shield', 30) + '</span><span class="x-eyebrow">' + T('按需授权', 'PERMISSIONS, AS NEEDED') + '</span><h1>' + T('由你决定它能做什么', 'You decide what it can do') + '</h1><p class="x-onboard-subtitle">' + T('需要时再开启，也可以稍后设置。', 'Enable what you need now, or come back later.') + '</p>' + permissionRows(s) + '<button class="x-text-action" data-action="x:privacy-note">' + T('查看隐私说明', 'Read the privacy details') + '</button>';
    return '<div class="x-onboard-ready-art"><img src="' + esc(ASSETS.pet) + '" alt="' + esc(name) + '"><span class="x-ready-halo"></span></div><span class="x-eyebrow">' + T('准备好了', 'ALL SET') + '</span><h1>' + esc(name) + (s.connected ? T('，准备好了', ' is ready') : '') + (!s.connected ? T('先在这里等你', ' will be here') : '') + '</h1><p class="x-onboard-subtitle">' + (s.connected ? T('从一条消息开始，让它慢慢了解你。', 'Start with a message, and let it get to know you.') : T('介绍已保存，连接 AI 后就能开始聊天。', 'Your introduction is saved. Connect AI to start chatting.')) + '</p><div class="x-ready-tips"><div>' + xi('chat', 18) + '<span>' + T('点一下角色，打开聊天。', 'Click the character to open chat.') + '</span></div><div>' + xi('clock', 18) + '<span>' + T('关上聊天，它也会继续工作。', 'Work continues when chat is closed.') + '</span></div><div>' + xi('settings', 18) + '<span>' + T('设置和退出都在菜单栏里。', 'Find Settings and Quit in the menu bar.') + '</span></div></div>';
  }

  function renderOnboarding() {
    const s = defaults().setup;
    const main = s.step === 0 ? xb(T('继续', 'Continue'), 'x:setup-next', { icon: 'arrowRight', disabled: !(s.name || '').trim() }) : s.step === 1 ? xb(T('稍后连接', 'Connect later'), 'x:setup-skip-ai', { variant: 'secondary' }) : s.step === 2 ? xb(s.loginWaiting ? T('模拟完成登录', 'Complete demo sign-in') : s.provider === 'caelis' ? T('连接服务', 'Connect service') : T('使用这个账号', 'Use this account'), 'x:setup-connect', { icon: 'arrowRight' }) : s.step === 5 ? xb(s.connected ? T('和', 'Chat with ') + (s.name || bot()) + T('聊聊', '') : T('连接 AI', 'Connect AI'), s.connected ? 'x:setup-finish-chat' : 'x:setup-back-to-ai', { icon: 'arrowRight' }) : xb(T('继续', 'Continue'), 'x:setup-next', { icon: 'arrowRight' });
    return '<div class="x-onboard-layout"><div class="x-onboard-top"><span class="x-onboard-wordmark">' + renderAvatar() + '<strong>Caelis Bot</strong></span><div class="x-onboard-top-actions"><button class="x-onboard-restart" data-action="x:setup-restart">' + xi('refresh', 12) + T('重新开始', 'Restart') + '</button><span class="x-onboard-counter">' + (s.step + 1) + ' / 6</span></div></div><main class="x-onboard-main"><div class="x-onboard-content x-onboard-step-' + s.step + '">' + setupContent() + '</div></main><footer class="x-onboard-footer"><div>' + (s.step > 0 && s.step < 5 ? xb(T('返回', 'Back'), 'x:setup-back', { variant: 'ghost', icon: 'back' }) : '<span class="x-onboard-footer-hint">' + T('安静一点，也贴近一点。', 'Quiet by nature. Close at hand.') + '</span>') + '</div><div class="x-onboard-dots" aria-label="' + T('引导进度', 'Setup progress') + '">' + Array.from({ length: 6 }, (_, i) => '<span class="' + (i === s.step ? 'is-current' : i < s.step ? 'is-done' : '') + '"></span>').join('') + '</div><div class="x-onboard-footer-actions">' + (s.step === 4 ? xb(T('稍后设置', 'Set up later'), 'x:setup-next', { variant: 'ghost' }) : s.step === 5 ? xb(T('先回到桌面', 'Go to desktop'), 'x:setup-finish-desktop', { variant: 'ghost' }) : '') + main + '</div></footer></div>';
  }

  function finishSetup(view) {
    const x = defaults();
    const s = x.setup;
    state.botName = (s.name || '').trim() || bot();
    x.connected = s.connected;
    x.toolsEnabled = s.tools;
    x.permissions = Object.assign({}, s.permissions);
    if (!state.settings) state.settings = {};
    if (!state.settings.ai) state.settings.ai = {};
    Object.assign(state.settings.ai, { connected: s.connected, tool: s.provider, account: s.provider === 'codex' ? 'ChatGPT' : (s.service || 'OpenAI'), workTool: s.provider });
    state.settings.captureEnabled = s.tools;
    state.settings.permissions = Object.assign({}, state.settings.permissions || {}, { notifications: s.permissions.notifications, screenCapture: s.permissions.screen, accessibility: s.permissions.accessibility });
    x.setupComplete = true;
    if (!x.introductionSent && s.connected) {
      x.messages = [];
      addMessage({ role: 'user', kind: 'text', body: translated('你的名字是' + state.botName + '。' + (s.description ? '\n' + s.description : ''), 'Your name is ' + state.botName + '.' + (s.description ? '\n' + s.description : '')) });
      addMessage({ role: 'assistant', kind: 'text', body: translated('记住啦，我是' + state.botName + '。\n\n有什么想做的，直接告诉我就好。', 'I’ll remember. I’m ' + state.botName + '.\n\nWhenever there’s something you want to do, just tell me.') });
      x.introductionSent = true;
    }
    save();
    navigate(view);
  }

  function screenshotModal() {
    const x = defaults();
    if (!x.capture) x.capture = { tool: 'annotate', redacted: false, annotated: false, fullScreen: !!(state.settings && state.settings.captureContext), undo: [] };
    const c = x.capture;
    openModal(() => ({ title: T('截图', 'Screenshot'), subtitle: T('确认之后，再交给助手。', 'Review it before sharing with your assistant.'), width: 'large', body: '<div class="x-capture-editor"><div class="x-capture-toolbar"><div><button class="x-capture-tool ' + (c.tool === 'annotate' ? 'is-active' : '') + '" data-action="x:capture-tool" data-tool="annotate">' + xi('image', 17) + T('标注', 'Annotate') + '</button><button class="x-capture-tool ' + (c.tool === 'redact' ? 'is-active' : '') + '" data-action="x:capture-tool" data-tool="redact">' + xi('shield', 17) + T('遮盖', 'Redact') + '</button></div><div>' + iconButton('x:capture-undo', 'back', T('撤销', 'Undo')) + iconButton('x:capture-redo', 'refresh', T('重做', 'Redo')) + '</div></div><button class="x-capture-canvas" data-action="x:capture-mark" aria-label="' + T('点击截图添加标注或遮盖', 'Click the image to add an annotation or redaction') + '"><div class="x-capture-document"><span class="x-capture-document-eyebrow">' + T('项目笔记', 'PROJECT NOTES') + '</span><h2>' + T('把事情说清楚，<br>让工作继续。', 'Make it clear.<br>Keep work moving.') + '</h2><p>' + T('保持简洁的进展、明确的下一步，以及不打断思路的反馈。', 'Clear progress, useful next steps, and feedback that leaves room to think.') + '</p><div class="x-capture-mini-table"><span>' + T('主要流程', 'Core flow') + '</span><strong>' + T('已梳理', 'Reviewed') + '</strong><span>' + T('交互状态', 'Interaction states') + '</span><strong>' + T('继续核对', 'In review') + '</strong></div><div class="x-capture-sensitive">' + (c.redacted ? '<span class="x-redaction-bar"></span>' : 'demo@example.test') + '</div></div>' + (c.annotated ? '<span class="x-capture-annotation"></span>' : '') + '<span class="x-capture-click-hint">' + T('点击添加', 'Click to add') + (c.tool === 'redact' ? T('遮盖', ' redaction') : T('标注', ' annotation')) + '</span></button><div class="x-capture-options"><div><strong>' + T('同时附带完整屏幕', 'Also include the full screen') + '</strong><p>' + T('询问助手时，会一并发送完整屏幕。', 'The full screen will be included when you ask your assistant.') + '</p></div>' + toggle('x:capture-fullscreen', c.fullScreen, T('同时附带完整屏幕', 'Also include the full screen')) + '</div></div>', footer: xb(T('取消', 'Cancel'), 'x:close-modal', { variant: 'ghost' }) + xb(T('复制', 'Copy'), 'x:capture-copy', { variant: 'secondary', icon: 'copy' }) + xb(T('贴到桌面', 'Pin to desktop'), 'x:capture-pin', { variant: 'secondary', icon: 'pin' }) + xb(T('询问', 'Ask ') + bot(), 'x:capture-ask', { icon: 'chat' }) }));
  }

  function beginScreenshot() {
    const x = defaults();
    if (!x.toolsEnabled) {
      openModal(() => ({ title: T('先开启截屏与贴图', 'Enable capture and pin first'), width: 'small', body: '<p class="x-modal-note">' + T('可以在设置中开启这些桌面工具。普通聊天附件仍然可以使用。', 'Enable these desktop tools in Settings. Regular chat attachments are still available.') + '</p>', footer: xb(T('取消', 'Cancel'), 'x:close-modal', { variant: 'secondary' }) + xb(T('前往设置', 'Open Settings'), 'x:open-tools') }));
      return;
    }
    if (!x.permissions.screen) {
      openModal(() => ({ title: T('允许截取你选择的画面', 'Allow capture of the area you choose'), subtitle: T('需要屏幕录制权限', 'Screen Recording permission is needed'), width: 'small', body: '<div class="x-permission-dialog">' + xi('image', 40) + '<p>' + T('实际应用会打开系统设置，由你开启屏幕录制权限。', 'The app opens System Settings so you can enable Screen Recording.') + '</p><p class="muted">' + T('这里使用示例截图，不会采集你电脑的画面。', 'This preview uses a sample screenshot and does not capture your screen.') + '</p></div>', footer: xb(T('稍后', 'Not now'), 'x:close-modal', { variant: 'secondary' }) + xb(T('模拟允许并继续', 'Allow in demo'), 'x:capture-grant') }));
      return;
    }
    screenshotModal();
  }

  function captureDataURL() {
    const c = defaults().capture || {};
    const canvas = document.createElement('canvas');
    canvas.width = 1000; canvas.height = 620;
    const ctx = canvas.getContext('2d');
    ctx.fillStyle = '#f6f5f1'; ctx.fillRect(0, 0, 1000, 620);
    ctx.fillStyle = '#ffffff'; ctx.fillRect(70, 45, 860, 530);
    ctx.fillStyle = '#6c746a'; ctx.font = '15px system-ui'; ctx.fillText(T('项目笔记', 'PROJECT NOTES'), 120, 110);
    ctx.fillStyle = '#2f332f'; ctx.font = 'bold 34px system-ui'; ctx.fillText(T('把事情说清楚，让工作继续。', 'Make it clear. Keep work moving.'), 120, 188);
    ctx.font = '20px system-ui'; ctx.fillStyle = '#6b716c'; ctx.fillText(T('保持简洁的进展、明确的下一步。', 'Clear progress. Useful next steps.'), 120, 249);
    ctx.fillStyle = '#edf1eb'; ctx.fillRect(120, 301, 700, 94); ctx.font = '19px system-ui'; ctx.fillStyle = '#4d574b'; ctx.fillText(T('主要流程    已梳理', 'Core flow    Reviewed'), 145, 341); ctx.fillText(T('交互状态    继续核对', 'Interaction states    In review'), 145, 375);
    if (c.redacted) { ctx.fillStyle = '#303331'; ctx.fillRect(120, 465, 300, 26); }
    else { ctx.font = '17px system-ui'; ctx.fillStyle = '#92978e'; ctx.fillText('demo@example.test', 120, 483); }
    if (c.annotated) { ctx.strokeStyle = '#c98961'; ctx.lineWidth = 4; ctx.strokeRect(106, 287, 728, 122); }
    return canvas.toDataURL('image/png');
  }

  function dataURLBlob(data) {
    const parts = data.split(',');
    const bytes = atob(parts[1]);
    const array = new Uint8Array(bytes.length);
    for (let i = 0; i < bytes.length; i++) array[i] = bytes.charCodeAt(i);
    return new Blob([array], { type: 'image/png' });
  }

  function mentionPicker() {
    const s = state.settings || {};
    const catalog = [
      ['github', 'GitHub', 'github', translated('代码、Issue 与 Pull Request', 'Code, issues, and pull requests')],
      ['notion', 'Notion', 'file', translated('工作区页面', 'Workspace pages')],
      ['obsidian', 'Obsidian', 'folder', translated('本机笔记', 'Local notes')],
      ['brave', 'Brave Search', 'search', translated('网页与参考资料', 'Web pages and sources')],
      ['amap', T('高德地图', 'Amap'), 'pin', translated('地点、路线和天气', 'Places, routes, and weather')],
      ['tencent', T('腾讯文档', 'Tencent Docs'), 'file', translated('在线文档', 'Online documents')],
      ['markdown', 'Markdown Work', 'file', translated('Markdown 写作与审阅', 'Markdown writing and review')]
    ];
    const plugins = s.plugins || { github: { installed: true, connected: true }, obsidian: { installed: true, connected: true } };
    const installed = catalog.filter(item => plugins[item[0]] && plugins[item[0]].installed);
    openModal(() => ({ title: T('选择插件', 'Mention a plugin'), subtitle: T('把它加入消息，告诉助手你想使用什么。', 'Add it to your message to tell your assistant what to use.'), width: 'medium', body: '<div class="x-mention-list">' + (installed.length ? installed.map(item => {
      const available = plugins[item[0]].connected || ['obsidian', 'markdown'].includes(item[0]);
      return '<button data-action="' + (available ? 'x:mention-select' : 'x:mention-connect') + '" data-plugin="' + item[0] + '" data-name="' + esc(item[1]) + '"><span class="x-mention-icon">' + xi(item[2], 20) + '</span><span class="x-mention-copy"><strong>' + esc(item[1]) + '</strong><small>' + esc(read(item[3])) + '</small></span>' + (available ? xi('plus', 16) : badge(T('待连接', 'Connect first'), 'warning')) + '</button>';
    }).join('') : '<div class="empty-state"><h3>' + T('还没有安装插件', 'No plugins installed yet') + '</h3><p>' + T('安装后，就可以在这里选择。', 'Installed plugins will appear here.') + '</p></div>') + '</div>', footer: xb(T('管理插件', 'Manage plugins'), 'x:manage-plugins', { variant: 'ghost', icon: 'settings' }) + xb(T('关闭', 'Close'), 'x:close-modal', { variant: 'secondary' }) }));
  }

  function focusInput() {
    requestAnimationFrame(() => {
      const input = document.getElementById('x-chat-input');
      if (input) { input.focus(); input.setSelectionRange(input.value.length, input.value.length); }
    });
  }

  async function copyText(content) {
    try { await navigator.clipboard.writeText(content); showToast('已复制', 'Copied'); }
    catch (_) {
      const text = document.createElement('textarea'); text.value = content; text.style.position = 'fixed'; text.style.opacity = '0'; document.body.appendChild(text); text.select();
      const copied = document.execCommand('copy'); text.remove();
      showToast(copied ? '已复制' : '浏览器不允许复制，请从预览中选择文字。', copied ? 'Copied' : 'Your browser blocked copying. Select the text in the preview.');
    }
  }

  function updateComposerButton() {
    const x = defaults();
    const send = document.getElementById('x-send-button');
    if (!send) return;
    const stopping = x.running && !x.draft.trim();
    send.dataset.action = stopping ? 'x:stop' : 'x:send';
    send.disabled = !x.running && !x.draft.trim() && !x.attachments.length;
    send.classList.toggle('is-stop', stopping);
    send.setAttribute('aria-label', stopping ? T('停止当前回复', 'Stop this response') : T('发送消息', 'Send message'));
    send.innerHTML = xi(stopping ? 'stop' : 'send', 19);
  }

  function fitDesktopPet() {
    const scene = document.querySelector('.x-desktop-scene');
    const anchor = document.getElementById('x-pet-anchor');
    if (!scene || !anchor) return;
    const tasks = scene.querySelector('.x-desktop-tasks');
    const bubble = anchor.querySelector('.x-pet-bubble');
    const narrow = scene.clientWidth < 620;
    const right = narrow ? 16 : Math.round(scene.clientWidth * (scene.clientWidth < 900 ? .09 : .16));
    const bottom = narrow ? 96 : 88;
    if (tasks) {
      tasks.style.top = narrow ? '145px' : '';
      tasks.style.bottom = narrow ? '92px' : '';
      tasks.style.maxHeight = narrow ? 'none' : '';
    }
    const occupiedLeft = tasks && tasks.children.length ? tasks.offsetLeft + tasks.offsetWidth : scene.clientWidth * .28;
    const availableWidth = Math.max(70, scene.clientWidth - right - occupiedLeft - 24);
    const availableHeight = Math.max(105, scene.clientHeight - bottom - (narrow ? 145 : 132));
    const baseFit = Math.min(1, availableWidth / (198 * 1.6), availableHeight / (250 * 1.6));
    const requested = Math.max(.65, Math.min(1.6, Number((state.settings || {}).petSize || 100) / 100));
    const width = 198 * baseFit * requested;
    const height = 250 * baseFit * requested;
    const anchorLeft = scene.clientWidth - right - width;
    const anchorTop = scene.clientHeight - bottom - height;
    anchor.style.width = width + 'px';
    anchor.style.height = height + 'px';
    anchor.style.right = right + 'px';
    anchor.style.bottom = bottom + 'px';
    anchor.style.setProperty('--x-pet-fit', String(baseFit));
    if (bubble) {
      if (narrow) {
        const bubbleWidth = Math.min(340, scene.clientWidth - 32);
        bubble.style.width = bubbleWidth + 'px';
        bubble.style.left = ((scene.clientWidth - bubbleWidth) / 2 - anchorLeft) + 'px';
        bubble.style.top = (18 - anchorTop) + 'px';
        bubble.style.bottom = 'auto';
        bubble.style.transform = 'none';
        bubble.classList.add('is-docked');
      } else {
        const bubbleWidth = Math.min(248, scene.clientWidth - 32);
        bubble.style.width = bubbleWidth + 'px';
        bubble.style.left = Math.max(8 - anchorLeft, Math.min((width - bubbleWidth) / 2, scene.clientWidth - 8 - anchorLeft - bubbleWidth)) + 'px';
        bubble.style.top = 'auto';
        bubble.style.bottom = 'calc(100% + 14px)';
        bubble.style.transform = 'none';
        bubble.classList.remove('is-docked');
      }
    }
  }

  function handleChange(bind, value, el) {
    if (!bind || !bind.startsWith('x:')) return false;
    const x = defaults();
    if (bind === 'x:scenario') { setScenario(value); return true; }
    if (bind === 'x:draft') { x.draft = String(value || ''); updateComposerButton(); if (el) { el.style.height = 'auto'; el.style.height = Math.min(el.scrollHeight, 130) + 'px'; } save(); return true; }
    if (bind === 'x:answer-text') {
      const m = x.messages.find(item => item.id === el.dataset.id); if (m) { m.customText = String(value || ''); m.selected = null; const card = el.closest('.x-decision-content'); if (card) { const submit = card.querySelector('[data-action="x:submit-answer"]'); if (submit) submit.disabled = !m.customText.trim(); card.querySelectorAll('[role="radio"]').forEach(r => { r.setAttribute('aria-checked', 'false'); r.classList.remove('is-selected'); }); } } save(); return true;
    }
    if (bind === 'x:setup-name') { x.setup.name = String(value || ''); const next = document.querySelector('[data-action="x:setup-next"]'); if (next) next.disabled = !x.setup.name.trim(); save(); return true; }
    if (bind === 'x:setup-description') { x.setup.description = String(value || ''); save(); return true; }
    if (bind === 'x:setup-tools') { x.setup.tools = !!value; save(); return true; }
    if (bind === 'x:capture-fullscreen') { x.capture.fullScreen = !!value; save(); return true; }
    if (bind === 'x:setup-service') { x.setup.service = String(value || 'OpenAI'); save(); return true; }
    return true;
  }

  function handleAction(action, el, event) {
    if (!action || !action.startsWith('x:')) return false;
    const x = defaults();
    const d = el && el.dataset || {};
    if (action === 'x:close-modal') { closeModal(); return true; }
    if (action === 'x:send') { sendMessage(); return true; }
    if (action === 'x:stop') { stopStream(); return true; }
    if (action === 'x:retry-message') {
      const msg = x.messages.find(m => m.id === d.id);
      if (msg) {
        msg.stopped = false;
        streamReply('我继续为你整理这部分内容：已补充完整的项目总结与后续实施步骤。', 'Continuing to prepare this update with complete summary and next steps.');
      }
      return true;
    }
    if (action === 'x:scenario') { setScenario(d.scenario); return true; }
    if (action === 'x:latest') { scrollToEnd(true); return true; }
    if (action === 'x:load-earlier') {
      if (x.earlierLoaded) return true;
      const scroller = document.getElementById('x-chat-scroll');
      const oldHeight = scroller ? scroller.scrollHeight : 0;
      const oldTop = scroller ? scroller.scrollTop : 0;
      x.messages.unshift(
        { id: 'earlier-user', role: 'user', kind: 'text', body: translated('之后的更新尽量简洁，先告诉我结果。', 'Please keep future updates concise and lead with the result.'), time: T('昨天 18:20', 'Yesterday 18:20') },
        { id: 'earlier-bot', role: 'assistant', kind: 'text', body: translated('好。我会先说结果，再补上必要的过程和下一步。', 'Got it. I’ll start with the result, then add the necessary context and next steps.'), time: T('昨天 18:20', 'Yesterday 18:20') }
      );
      x.earlierLoaded = true;
      save();
      render();
      requestAnimationFrame(() => { const panel = document.getElementById('x-chat-scroll'); if (panel) panel.scrollTop = oldTop + Math.max(0, panel.scrollHeight - oldHeight); });
      return true;
    }
    if (action === 'x:mention-picker') { x.addMenuOpen = false; mentionPicker(); return true; }
    if (action === 'x:mention-select') { x.draft = x.draft.replace(/@\s*$/, '') + (x.draft && !/\s$/.test(x.draft) ? ' ' : '') + '@' + d.name + ' '; closeModal(); save(); refreshChat(false); focusInput(); return true; }
    if (action === 'x:manage-plugins' || action === 'x:mention-connect') { closeModal(); navigate('settings', 'plugins'); if (action === 'x:mention-connect' && window.Settings) Settings.handleAction('s:plugin:' + d.plugin, el, event); return true; }
    if (action === 'x:open-chat') { navigate('chat'); return true; }
    if (action === 'x:chat-settings') { closeModal(); navigate('settings', state.page || 'general'); return true; }
    if (action === 'x:connect-ai') { navigate('settings', 'ai'); return true; }
    if (action === 'x:tasks') { tasksModal(); return true; }
    if (action === 'x:chat-menu') {
      openModal(() => ({ title: bot(), subtitle: T('你的长期个人助手', 'Your personal companion'), width: 'small', body: '<div class="x-menu-list"><button data-action="x:go-desktop">' + xi('general', 20) + T('回到桌面', 'Back to desktop') + xi('chevron', 17) + '</button><button data-action="x:chat-settings">' + xi('settings', 20) + T('设置', 'Settings') + xi('chevron', 17) + '</button><button data-action="x:chat-shortcut">' + xi('key', 20) + T('聊天快捷键', 'Chat shortcut') + '<kbd>⌃ ⇧ Space</kbd></button></div><p class="x-modal-note">' + T('关闭聊天后，任务会继续，结果也会留在这里。', 'Tasks continue when chat is closed, and results stay here.') + '</p>', footer: xb(T('完成', 'Done'), 'x:close-modal', { variant: 'secondary' }) })); return true;
    }
    if (action === 'x:chat-shortcut') { showToast('聊天快捷键：Ctrl + Shift + Space', 'Chat shortcut: Ctrl + Shift + Space'); return true; }
    if (action === 'x:go-desktop') { closeModal(); navigate('desktop'); return true; }

    if (action === 'x:add-menu') {
      x.addMenuOpen = !x.addMenuOpen; refreshChat(false);
      if (x.addMenuOpen) requestAnimationFrame(() => document.querySelector('.x-attachment-menu button')?.focus());
      return true;
    }
    if (action === 'x:toggle-decision' || action === 'x:close-decision' || action === 'x:open-decision') {
      if (action === 'x:open-decision') { x.activeDecision = d.id; x.decisionOpen = true; }
      else x.decisionOpen = action === 'x:toggle-decision' ? !x.decisionOpen : false;
      refreshChat(false);
      if (action === 'x:close-decision') requestAnimationFrame(() => document.querySelector('.x-decision-trigger')?.focus());
      return true;
    }
    if (action === 'x:decision-prev' || action === 'x:decision-next') {
      const pending = pendingMessages();
      const i = Math.max(0,pending.findIndex(m => m.id === x.activeDecision));
      const next = pending[i + (action === 'x:decision-prev' ? -1 : 1)];
      if (next) { x.activeDecision = next.id; refreshChat(false); } return true;
    }
    if (action === 'x:choose-files') { x.addMenuOpen = false; refreshChat(false); closeModal(); const input = document.getElementById('x-file-input'); if (input) input.click(); return true; }
    if (action === 'x:remove-attachment') { x.attachments = x.attachments.filter(f => f.id !== d.id); save(); refreshChat(false); focusInput(); return true; }
    if (action === 'x:preview-attachment') { x.addMenuOpen = false; previewAttachment(d.id); return true; }
    if (action === 'x:preview-result') { previewResult(d.id); return true; }
    if (action === 'x:copy-result' || action === 'x:download-result') {
      const m = x.messages.find(item => item.id === d.id); if (!m) return true;
      if (action === 'x:copy-result') copyText(read(m.result || m.body));
      else { const blob = new Blob([read(m.result || m.body)], { type: 'text/markdown;charset=utf-8' }); const url = URL.createObjectURL(blob); const a = document.createElement('a'); a.href = url; a.download = m.filename || 'Project-update.md'; a.click(); setTimeout(() => URL.revokeObjectURL(url), 1000); showToast('文件已准备下载', 'Your file is ready to download'); }
      return true;
    }
    if (action === 'x:copy-message') { const m = x.messages.find(item => item.id === d.id); if (m) copyText(read(m.body)); return true; }
    if (action === 'x:quote-message') { const m = x.messages.find(item => item.id === d.id); if (m) { x.quote = { id: m.id, author: m.role === 'user' ? T('你', 'You') : bot(), body: m.body }; save(); refreshChat(false); focusInput(); } return true; }
    if (action === 'x:clear-quote') { x.quote = null; save(); refreshChat(false); focusInput(); return true; }
    if (action === 'x:jump-message') { const target = document.getElementById('x-msg-' + d.id); if (target) { target.scrollIntoView({ behavior: 'smooth', block: 'center' }); target.classList.add('x-highlight'); setTimeout(() => target.classList.remove('x-highlight'), 1600); } return true; }
    if (action === 'x:jump-pending') { const m = pendingMessages()[0]; if (m) { x.collapsed[m.id] = false; refreshChat(false); requestAnimationFrame(() => { const target = document.getElementById('x-msg-' + m.id); if (target) target.scrollIntoView({ block: 'center', behavior: 'smooth' }); }); } return true; }
    if (action === 'x:collapse-decision' || action === 'x:expand-decision') { x.collapsed[d.id] = action === 'x:collapse-decision'; refreshChat(false); return true; }
    if (action === 'x:select-answer') {
      const m = x.messages.find(item => item.id === d.id);
      if (m && !m.submitting) {
        m.selected = Number(d.index);
        m.custom = false;
        m.customText = '';
        save();
        refreshChat(false);
      }
      return true;
    }
    if (action === 'x:custom-answer') {
      const m = x.messages.find(item => item.id === d.id);
      if (m && !m.submitting) {
        m.custom = true;
        m.selected = null;
        save();
        refreshChat(false);
        requestAnimationFrame(() => {
          const input = document.querySelector('[data-input="x:answer-text"][data-id="' + m.id + '"]');
          if (input) {
            input.focus();
            if (m.customText) input.select();
          }
        });
      }
      return true;
    }
    if (action === 'x:clear-custom-text') {
      const m = x.messages.find(item => item.id === d.id);
      if (m && !m.submitting) {
        m.customText = '';
        save();
        refreshChat(false);
        requestAnimationFrame(() => {
          const input = document.querySelector('[data-input="x:answer-text"][data-id="' + m.id + '"]');
          if (input) input.focus();
        });
      }
      return true;
    }

    if (action === 'x:submit-answer') {
      const m = x.messages.find(item => item.id === d.id);
      if (!m || m.resolved || m.submitting || (!m.custom && m.selected == null) || (m.custom && !(m.customText || '').trim())) return true;
      const answer = m.custom ? m.customText.trim() : m.options[m.selected].label;
      m.submitting = true; refreshChat(false);
      setTimeout(() => {
        if (!x.messages.includes(m)) return;
        m.submitting = false; m.resolved = true; m.answer = answer; x.decisionOpen = false;
        streamReply('好，就按“' + read(answer) + '”来整理。我会保留具体进展和下一步。',
          'Great, I’ll focus on “' + (typeof answer === 'object' ? answer.en : answer) + '” and keep the progress and next steps concise.');
        if (state.view === 'chat') focusInput();
      },650); return true;
    }

    if (action === 'x:approval-allow' || action === 'x:approval-deny') {
      const m = x.messages.find(item => item.id === d.id);
      if (!m || m.resolved || m.submitting) return true;
      const answer = action === 'x:approval-allow' ? 'allow' : 'deny';
      m.submitting = true; refreshChat(false);
      setTimeout(() => {
        if (!x.messages.includes(m)) return;
        m.submitting = false; m.resolved = true; m.answer = answer; x.decisionOpen = false;
        if (answer === 'allow') streamReply('收到。我会只读取这份参考文件，再继续整理更新。','Received. I’ll read only this reference file and continue the update.');
        else { addMessage({role:'assistant',kind:'text',body:translated('好，不读取这份文件。你可以直接把需要的内容发给我。','I won’t read this file. You can send me the relevant content instead.')}); save(); refreshChat(); }
        if (state.view === 'chat') focusInput();
      },650); return true;
    }
    if (action === 'x:reconnect') { x.offline = false; setConnected(true); save(); refreshChat(false); showToast('连接已恢复，草稿仍然保留。', 'Connection restored. Your draft is still here.'); focusInput(); return true; }
    if (action === 'x:resolve-unknown') { const m = x.messages.find(item => item.delivery === 'unknown'); if (m) m.delivery = 'delivered'; x.deliveryUnknown = false; addMessage({ role: 'system', kind: 'info', body: translated('已确认消息送达，无需重复发送。', 'Message delivery is confirmed. No need to send it again.') }); save(); refreshChat(); return true; }
    if (action === 'x:observe-task') { observeTask(d.id); return true; }
    if (action === 'x:move-task-up' || action === 'x:move-task-down') { const visible = x.tasks.filter(t => t.visible); const i = visible.findIndex(t => t.id === d.id); const target = visible[i + (action === 'x:move-task-up' ? -1 : 1)]; if (target) { reorderTask(d.id, target.id); showToast('任务卡顺序已调整', 'Task card order updated'); } return true; }
    if (action === 'x:pin-task') { const t = x.tasks.find(item => item.id === d.id); if (t) { t.pinned = !t.pinned; x.tasks.sort((a, b) => Number(b.pinned) - Number(a.pinned)); save(); if (state.view === 'desktop') render(); else tasksModal(); showToast(t.pinned ? '已固定到桌面' : '已取消固定', t.pinned ? 'Pinned to desktop' : 'Unpinned'); } return true; }
    if (action === 'x:remove-task') { const t = x.tasks.find(item => item.id === d.id); if (t) { t.visible = false; save(); if (state.view === 'desktop') render(); else tasksModal(); showToast('已移除任务卡，任务会继续。', 'Card removed. The task keeps running.'); } return true; }
    if (action === 'x:restore-tasks') { x.tasks.forEach(t => t.visible = true); save(); render(); return true; }
    if (action === 'x:task-result') { closeModal(); navigate('chat'); setScenario('normal'); return true; }
    if (action === 'x:pet-state') { x.petState = d.state; setPetVisible(true); const first = x.tasks[0]; first.visible = true; first.status = d.state === 'waiting' ? 'waiting' : d.state === 'done' ? 'done' : 'working'; first.detail = d.state === 'waiting' ? translated('这次整理哪些内容？', 'What should this update cover?') : d.state === 'done' ? translated('项目更新.md 已准备好', 'Project-update.md is ready') : translated('正在阅读最近的变更', 'Reading the latest changes'); save(); render(); return true; }
    if (action === 'x:hide-bubble') { x.petState = 'idle'; save(); render(); return true; }
    if (action === 'x:pet-answer') { navigate('chat'); setScenario('question'); return true; }
    if (action === 'x:show-pet') { setPetVisible(true); save(); render(); return true; }
    if (action === 'x:hide-pet') { setPetVisible(false); closeModal(); save(); render(); showToast('角色已隐藏，工作会继续。', 'Companion hidden. Work keeps going.'); return true; }
    if (action === 'x:desktop-menu') {
      openModal(() => ({ title: 'Caelis Bot', width: 'small', body: '<div class="x-menu-list"><button data-action="x:menu-chat">' + xi('chat', 20) + T('打开聊天', 'Open chat') + '</button><button data-action="x:tasks">' + xi('clock', 20) + T('后台任务', 'Background tasks') + '</button><button data-action="x:chat-settings">' + xi('settings', 20) + T('设置…', 'Settings…') + '</button><hr><button data-action="' + (x.petVisible ? 'x:hide-pet' : 'x:menu-show-pet') + '">' + xi('eye', 20) + (x.petVisible ? T('隐藏角色', 'Hide companion') : T('显示角色', 'Show companion')) + '</button><button data-action="x:quit-demo">' + xi('close', 20) + T('退出 Caelis Bot', 'Quit Caelis Bot') + '</button></div>', footer: '' })); return true;
    }
    if (action === 'x:menu-chat') { closeModal(); navigate('chat'); return true; }
    if (action === 'x:menu-show-pet') { setPetVisible(true); closeModal(); save(); render(); return true; }
    if (action === 'x:quit-demo') { confirmDialog({ title: T('退出 Caelis Bot？', 'Quit Caelis Bot?'), body: T('退出会结束应用在这台电脑上的运行。原型中只演示这个决定，不会关闭浏览器。', 'Quitting ends the app on this computer. This prototype demonstrates the decision without closing your browser.'), confirmLabel: T('退出', 'Quit'), danger: true, onConfirm: () => { closeModal(); showToast('已演示退出。可以继续浏览原型。', 'Quit demonstrated. You can continue exploring the prototype.'); } }); return true; }
    if (action === 'x:setup-next') { if (x.setup.step === 0 && !(x.setup.name || '').trim()) return true; x.setup.step = Math.min(5, x.setup.step + 1); save(); render(); return true; }
    if (action === 'x:setup-restart') { x.setup = { step: 0, name: state.botName || '小爱', description: '', provider: 'codex', connected: false, tools: true, permissions: { notifications: false, screen: false, accessibility: false } }; x.introductionSent = false; save(); render(); return true; }
    if (action === 'x:setup-back') { x.setup.step = x.setup.step === 3 && x.setup.skippedAI ? 1 : Math.max(0, x.setup.step - 1); save(); render(); return true; }
    if (action === 'x:choose-provider') { x.setup.provider = d.provider; x.setup.step = 2; x.setup.skippedAI = false; x.setup.loginWaiting = false; save(); render(); return true; }
    if (action === 'x:setup-skip-ai') { x.setup.connected = false; x.setup.skippedAI = true; x.setup.step = 3; save(); render(); return true; }
    if (action === 'x:setup-connect') { x.setup.connected = true; x.setup.loginWaiting = false; x.setup.step = 3; save(); render(); return true; }
    if (action === 'x:setup-manage-account') { x.setup.loginWaiting = true; save(); render(); return true; }
    if (action === 'x:setup-permission') {
      const label = d.permission === 'notifications' ? T('通知', 'Notifications') : d.permission === 'screen' ? T('屏幕录制', 'Screen Recording') : T('辅助功能', 'Accessibility');
      openModal(() => ({ title: T('开启', 'Enable ') + label, subtitle: T('系统权限演示', 'System permission preview'), width: 'small', body: '<div class="x-permission-dialog">' + xi('shield', 40) + '<p>' + T('实际应用会打开系统设置，由你决定是否授予这项权限。', 'The app opens System Settings, where you decide whether to grant this permission.') + '</p><p class="muted">' + T('这里不会修改你电脑的真实权限。', 'This prototype does not change permissions on your computer.') + '</p></div>', footer: xb(T('暂不开启', 'Not now'), 'x:close-modal', { variant: 'secondary' }) + xb(T('模拟已开启', 'Mark enabled'), 'x:setup-permission-grant', { attrs: 'data-permission="' + d.permission + '"' }) })); return true;
    }
    if (action === 'x:setup-permission-grant') { x.setup.permissions[d.permission] = true; closeModal(); save(); render(); return true; }
    if (action === 'x:privacy-note') { openModal(() => ({ title: T('隐私说明', 'Privacy details'), width: 'medium', body: '<div class="x-privacy-copy"><h3>' + T('按任务使用桌面信息', 'Desktop information is used for your tasks') + '</h3><p>' + T('执行你交代的桌面任务时，助手会将相关窗口的界面文字、控件信息与必要截图发送给已连接的 AI 服务。', 'During a desktop task you request, relevant interface text, control information, and necessary screenshots are sent to your connected AI service.') + '</p><h3>' + T('手动截图，由你确认', 'You review manual screenshots') + '</h3><p>' + T('是否发送截图、是否附带完整屏幕，都由你在发送前决定。终端预览保留在本机。', 'Before sending, you decide whether to share a screenshot and include the full screen. Terminal previews stay on this computer.') + '</p><p>' + T('不会持续录屏，也不会采集声音。', 'The app does not continuously record the screen or capture audio.') + '</p></div>', footer: xb(T('知道了', 'Got it'), 'x:close-modal') })); return true; }
    if (action === 'x:setup-finish-chat') { finishSetup('chat'); return true; }
    if (action === 'x:setup-finish-desktop') { finishSetup('desktop'); return true; }
    if (action === 'x:setup-back-to-ai') { x.setup.step = 1; save(); render(); return true; }
    if (action === 'x:screenshot') { x.addMenuOpen = false; beginScreenshot(); return true; }
    if (action === 'x:open-tools') { closeModal(); navigate('settings', 'tools'); return true; }
    if (action === 'x:capture-grant') { x.permissions.screen = true; if (!state.settings.permissions) state.settings.permissions = {}; state.settings.permissions.screenCapture = true; save(); screenshotModal(); return true; }
    if (action === 'x:capture-pin') { pinImage(captureDataURL()); return true; }
    if (action === 'x:pin-demo') {
      if (!x.toolsEnabled) { beginScreenshot(); return true; }
      openModal(() => ({ title: T('桌面贴图', 'Pin an image'), subtitle: T('用示例图片体验独立贴图。', 'Try a floating image with a sample screenshot.'), width: 'medium', body: '<div class="x-image-preview"><img src="' + captureDataURL() + '" alt="' + T('示例截图', 'Sample screenshot') + '"></div><p class="x-modal-note">' + T('实际应用使用剪贴板里的图片。贴图可以独立缩放、查看和关闭。', 'The app uses the image in your clipboard. Pinned images can be viewed, resized, and closed independently.') + '</p>', footer: xb(T('取消', 'Cancel'), 'x:close-modal', { variant: 'secondary' }) + xb(T('贴到桌面', 'Pin to desktop'), 'x:capture-pin', { icon: 'pin' }) })); return true;
    }
    if (action === 'x:close-pinned') { x.pinnedImage = null; save(); render(); return true; }
    if (action === 'x:pin-zoom-in' || action === 'x:pin-zoom-out') { if (x.pinnedImage) { x.pinnedImage.scale = Math.max(.65, Math.min(1.6, Number(x.pinnedImage.scale || 1) + (action === 'x:pin-zoom-in' ? .15 : -.15))); save(); render(); } return true; }
    if (action === 'x:capture-tool') { x.capture.tool = d.tool; screenshotModal(); return true; }
    if (action === 'x:capture-mark') { x.capture.undo.push({ annotated: x.capture.annotated, redacted: x.capture.redacted }); if (x.capture.tool === 'redact') x.capture.redacted = true; else x.capture.annotated = true; screenshotModal(); return true; }
    if (action === 'x:capture-undo') { const prior = x.capture.undo.pop(); if (prior) { x.capture.redo = { annotated: x.capture.annotated, redacted: x.capture.redacted }; Object.assign(x.capture, prior); screenshotModal(); } return true; }
    if (action === 'x:capture-redo') { if (x.capture.redo) { x.capture.undo.push({ annotated: x.capture.annotated, redacted: x.capture.redacted }); Object.assign(x.capture, x.capture.redo); x.capture.redo = null; screenshotModal(); } return true; }
    if (action === 'x:capture-copy') { const data = captureDataURL(); if (navigator.clipboard && window.ClipboardItem) navigator.clipboard.write([new ClipboardItem({ 'image/png': dataURLBlob(data) })]).then(() => showToast('截图已复制', 'Screenshot copied')).catch(() => showToast('浏览器未授予图片复制权限，可选择“询问助手”继续。', 'Image clipboard access is unavailable. Choose “Ask” to continue.')); else showToast('浏览器不支持复制图片，可选择“询问助手”继续。', 'Your browser cannot copy images. Choose “Ask” to continue.'); return true; }
    if (action === 'x:capture-ask') { const data = captureDataURL(); x.attachments.push({ id: 'capture-' + (++x.sequence), name: T('截图.png', 'Screenshot.png'), type: 'image/png', size: 72 * 1024, url: data }); if (x.capture.fullScreen) x.attachments.push({ id: 'capture-full-' + (++x.sequence), name: T('完整屏幕.png', 'Full-screen.png'), type: 'image/png', size: 96 * 1024, url: data }); x.draft = T('帮我看看这张截图。', 'Please take a look at this screenshot.'); closeModal(); save(); navigate('chat'); focusInput(); return true; }
    return true;
  }

  function afterRender() {
    const x = defaults();
    const input = document.getElementById('x-chat-input');
    if (input) {
      input.style.height = 'auto'; input.style.height = Math.min(input.scrollHeight, 130) + 'px';
      input.addEventListener('keydown', event => { if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); sendMessage(); } });
      input.addEventListener('paste', event => { const files = Array.from(event.clipboardData && event.clipboardData.items || []).filter(item => item.kind === 'file').map(item => item.getAsFile()).filter(Boolean); if (files.length) { event.preventDefault(); addFiles(files); } });
    }
    const fileInput = document.getElementById('x-file-input');
    if (fileInput) fileInput.addEventListener('change', () => { addFiles(fileInput.files); fileInput.value = ''; });
    const chat = document.querySelector('.x-chat-layout');
    if (chat) {
      chat.addEventListener('dragenter', event => { if (!Array.from(event.dataTransfer && event.dataTransfer.types || []).includes('Files')) return; event.preventDefault(); dragDepth++; const c = document.getElementById('x-composer'); if (c) c.classList.add('is-dragging'); });
      chat.addEventListener('dragover', event => { if (Array.from(event.dataTransfer && event.dataTransfer.types || []).includes('Files')) event.preventDefault(); });
      chat.addEventListener('dragleave', () => { dragDepth = Math.max(0, dragDepth - 1); if (!dragDepth) { const c = document.getElementById('x-composer'); if (c) c.classList.remove('is-dragging'); } });
      chat.addEventListener('drop', event => { if (event.dataTransfer && event.dataTransfer.files.length) { event.preventDefault(); dragDepth = 0; addFiles(event.dataTransfer.files); } });
    }
    const scrollPanel = document.getElementById('x-chat-scroll');
    if (scrollPanel) {
      const updateLatest = () => { const button = document.getElementById('x-jump-latest'); if (button) button.hidden = scrollPanel.scrollHeight - scrollPanel.scrollTop - scrollPanel.clientHeight < 160; };
      scrollPanel.addEventListener('scroll', updateLatest, { passive: true });
      requestAnimationFrame(updateLatest);
    }
    const pet = document.getElementById('x-pet');
    const anchor = document.getElementById('x-pet-anchor');
    fitDesktopPet();
    if (pet && anchor) {
      let origin = null;
      let moved = false;
      pet.addEventListener('pointerdown', event => { if (event.button !== 0) return; origin = { x: event.clientX, y: event.clientY, baseX: Number(x.petX || 0), baseY: Number(x.petY || 0) }; moved = false; pet.setPointerCapture(event.pointerId); });
      pet.addEventListener('pointermove', event => { if (!origin) return; const dx = event.clientX - origin.x; const dy = event.clientY - origin.y; if (Math.abs(dx) + Math.abs(dy) > 5) moved = true; const scene = document.querySelector('.x-desktop-scene'); const bounds = scene ? scene.getBoundingClientRect() : { width: 900, height: 550 }; x.petX = Math.max(-bounds.width + 290, Math.min(65, origin.baseX + dx)); x.petY = Math.max(-bounds.height + 310, Math.min(48, origin.baseY + dy)); anchor.style.setProperty('--x-pet-x', x.petX + 'px'); anchor.style.setProperty('--x-pet-y', x.petY + 'px'); });
      pet.addEventListener('pointerup', event => { if (origin) { origin = null; try { pet.releasePointerCapture(event.pointerId); } catch (_) {} save(); } });
      pet.addEventListener('click', event => { if (moved) { event.preventDefault(); event.stopPropagation(); moved = false; } }, true);
    }
    if (state.view === 'chat' && !chatMounted) { chatMounted = true; scrollToEnd(); }
    document.querySelectorAll('.x-task-card[draggable]').forEach(card => {
      card.addEventListener('dragstart', event => { draggedTaskId = card.dataset.taskId; event.dataTransfer.effectAllowed = 'move'; event.dataTransfer.setData('application/x-caelis-task', draggedTaskId); card.classList.add('is-dragging'); });
      card.addEventListener('dragover', event => { if (draggedTaskId && draggedTaskId !== card.dataset.taskId) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; card.classList.add('is-drop-target'); } });
      card.addEventListener('dragleave', () => card.classList.remove('is-drop-target'));
      card.addEventListener('drop', event => { if (!draggedTaskId) return; event.preventDefault(); event.stopPropagation(); const id = draggedTaskId; draggedTaskId = null; reorderTask(id, card.dataset.taskId); });
      card.addEventListener('dragend', () => { draggedTaskId = null; document.querySelectorAll('.x-task-card').forEach(item => item.classList.remove('is-dragging', 'is-drop-target')); });
    });
  }


  function dismissPopover() { defaults().addMenuOpen = false; }
  window.Experience = { renderChat, renderDesktop, renderOnboarding, handleAction, handleChange, afterRender, renderScenarioControls, dismissPopover };
  document.addEventListener('click', event => {
    if (defaults().addMenuOpen && !event.target.closest('.x-composer')) {
      defaults().addMenuOpen = false;
      document.querySelector('.x-attachment-menu')?.remove();
      document.querySelector('.x-add-button')?.setAttribute('aria-expanded','false');
    }
  });
  document.addEventListener('keydown', event => {
    const x = defaults();
    if (event.key === 'Escape' && state.view === 'chat' && !modalFactory) {
      if (x.addMenuOpen) { event.preventDefault(); x.addMenuOpen = false; refreshChat(false); requestAnimationFrame(() => document.querySelector('.x-add-button')?.focus()); }
      else if (x.decisionOpen) { event.preventDefault(); x.decisionOpen = false; refreshChat(false); requestAnimationFrame(() => document.querySelector('.x-decision-trigger')?.focus()); }
    }
    const customInput = event.target.closest('[data-input="x:answer-text"]');
    if (customInput && event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      const id = customInput.dataset.id;
      const submitBtn = document.querySelector('[data-action="x:submit-answer"][data-id="' + id + '"]');
      if (submitBtn && !submitBtn.disabled) {
        submitBtn.click();
      }
      return;
    }
    const choice = event.target.closest('[role=radio]');
    if (choice && ['ArrowDown','ArrowUp','ArrowRight','ArrowLeft'].includes(event.key) && !event.target.closest('input')) {
      event.preventDefault(); const choices = [...choice.closest('[role=radiogroup]').querySelectorAll('[role=radio]')];
      const i = choices.indexOf(choice); const forward = ['ArrowDown','ArrowRight'].includes(event.key);
      const next = choices[(i + choices.length + (forward ? 1 : -1)) % choices.length];
      next.focus();
      next.click();
    }
    const menu = document.querySelector('.x-attachment-menu');
    if (menu && menu.contains(document.activeElement) && ['ArrowDown','ArrowUp'].includes(event.key)) {
      event.preventDefault(); const options = [...menu.querySelectorAll('button')];
      const i = options.indexOf(document.activeElement);
      options[(i + options.length + (event.key === 'ArrowDown' ? 1 : -1)) % options.length].focus();
    }
  });

  document.addEventListener('keydown', event => {
    if (event.isComposing || event.repeat) return;
    if (event.key === 'F1') { event.preventDefault(); beginScreenshot(); }
    else if (event.key === 'F3') { event.preventDefault(); handleAction('x:pin-demo', null, event); }
    else if (event.ctrlKey && event.shiftKey && event.code === 'KeyT') { event.preventDefault(); tasksModal(); }
  });
  window.addEventListener('resize', fitDesktopPet);
})();
