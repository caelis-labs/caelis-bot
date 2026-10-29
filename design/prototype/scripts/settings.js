(function () {
  'use strict';

  const ui = { flow: null, pluginTab: 'all', pluginQuery: '', modelQuery: '', modelCategory: 'all', serial: 0 };
  const pluginCatalog = [
    { id: 'github', name: 'GitHub', icon: 'github', zh: '读取和更新代码、Issue 与 Pull Request。', en: 'Read and update code, issues and pull requests.', auth: 'github', publisher: 'GitHub', version: '1.1.0', skills: [['仓库协作', 'Repository work']], tools: [['读取仓库文件', 'Read repository files'], ['管理 Issue 与 PR', 'Manage issues and pull requests']] },
    { id: 'notion', name: 'Notion', icon: 'file', zh: '查找、整理和更新你的工作区页面。', en: 'Find, organize and update your workspace pages.', auth: 'oauth', publisher: 'Notion', version: '1.0.0', skills: [], tools: [['查找页面', 'Find pages'], ['更新页面内容', 'Update page content']] },
    { id: 'obsidian', name: 'Obsidian', icon: 'folder', zh: '使用本机 Obsidian CLI 整理笔记。', en: 'Work with your notes through the local Obsidian CLI.', auth: '', publisher: 'Caelis Labs', version: '2.0.0', skills: [['Obsidian 笔记', 'Obsidian notes']], tools: [] },
    { id: 'brave', name: 'Brave Search', icon: 'search', zh: '搜索网页，找到值得参考的资料。', en: 'Search the web for useful sources and information.', auth: 'key', publisher: 'Brave Software', version: '2.1.4', skills: [], tools: [['网页搜索', 'Web search']] },
    { id: 'amap', name: ['高德地图', 'Amap'], icon: 'pin', zh: '查找地点、路线和天气。', en: 'Find places, routes and weather.', auth: 'key', publisher: ['高德开放平台', 'Amap Open Platform'], version: '1.0.0', skills: [], tools: [['地点与路线', 'Places and routes'], ['查询天气', 'Check weather']] },
    { id: 'tencent', name: ['腾讯文档', 'Tencent Docs'], icon: 'file', zh: '创建、查找和编辑在线文档。', en: 'Create, find and edit online documents.', auth: 'token', publisher: ['腾讯文档团队', 'Tencent Docs'], version: '1.0.0', skills: [], tools: [['文档管理', 'Document management']] },
    { id: 'markdown', name: 'Markdown Work', icon: 'file', zh: '更清晰地起草、整理和审阅 Markdown。', en: 'Draft, organize and review Markdown with clarity.', auth: '', publisher: 'Caelis Labs', version: '1.0.0', skills: [['Markdown 写作与审阅', 'Markdown writing and review']], tools: [] }
  ];
  const models = [
    { id: 'default', name: ['使用工具默认模型', 'Use tool default'], provider: 'Auto', category: 'all', detail: ['跟随当前连接的全局配置', 'Follow the current global connection'], effort: true, tags: [['自动跟随', 'Auto']] },
    { id: 'gpt-6.1-sol', name: 'GPT-6.1 Sol', provider: 'OpenAI', category: 'chat', detail: ['主力日常对话 · 快速响应与多模态', 'Daily conversation · fast response & multimodal'], effort: true, tags: [['官方推荐', 'Recommended'], ['多模态', 'Vision'], ['低延迟', 'Low latency']] },
    { id: 'gpt-6-luna', name: 'GPT-6 Luna', provider: 'OpenAI', category: 'reasoning', detail: ['复杂任务与代码 · 深度推理与长上下文', 'Complex tasks & code · deep reasoning & long context'], effort: true, tags: [['深度推理', 'Deep reasoning'], ['代码专精', 'Coding']] },
    { id: 'mimo-v2.6-flash', name: 'MiMo V2.6 Flash', provider: 'MiMo', category: 'speed', detail: ['极速轻量模型 · 适合多轮日常检索', 'Ultra-fast lightweight · ideal for search'], effort: false, tags: [['极速', 'Ultra fast'], ['低消耗', 'Efficient']] },
    { id: 'claude-3.7-sonnet', name: 'Claude 3.7 Sonnet', provider: 'Anthropic', category: 'reasoning', detail: ['混合思考架构 · 复杂逻辑与系统重构', 'Hybrid reasoning · complex logic & architecture'], effort: true, tags: [['混合思考', 'Hybrid thinking'], ['深度分析', 'Analysis']] }
  ];
  const permissionInfo = {
    accessibility: ['辅助功能', 'Accessibility', '操作你授权的应用，并与桌面交互。', 'Interact with the desktop and the apps you authorize.', 'eye'],
    screenCapture: ['屏幕录制', 'Screen Recording', '截取你主动选择的窗口或屏幕区域。', 'Capture a window or area that you explicitly select.', 'image'],
    notifications: ['通知', 'Notifications', '在工作完成或需要你决定时提醒你。', 'Let you know when work finishes or needs a decision.', 'bell'],
    automation: ['自动化', 'Automation', '在需要时与指定的其他应用交互。', 'Interact with specific other apps when needed.', 'tools']
  };

  function val(v) { return Array.isArray(v) ? T(v[0], v[1]) : String(v == null ? '' : v); }
  function d() {
    const s = state.settings || (state.settings = {});
    const defaults = { startup: false, concurrency: 6, terminal: 'system', customTerminal: '', chatShortcut: 'Ctrl + Shift + Space', taskShortcut: 'Ctrl + Shift + T', chatShortcutEnabled: true, taskShortcutEnabled: true, captureShortcut: 'F1', pasteShortcut: 'F3', captureEnabled: true, petVisible: true, petSize: 100, character: 'caelis', avatarMode: 'follow', packs: [], storageCleaned: false, updatesAutomatic: true, updateStatus: 'current', approvalMode: 'auto', repairPermission: 'accessibility' };
    Object.keys(defaults).forEach(k => { if (s[k] === undefined) s[k] = defaults[k]; });
    s.ai = Object.assign({ connected: true, tool: 'codex', account: 'ChatGPT', conversationModel: 'gpt-6.1-sol', workModel: 'default', workTool: 'codex', effort: 'high', speed: 'standard', providerCount: 1 }, s.ai || {});
    s.channels = s.channels || {}; s.channels.telegram = Object.assign({ status: 'connected', bot: 'caelis_companion_bot', owner: '@xu_demo' }, s.channels.telegram || {}); s.channels.weixin = Object.assign({ status: 'unconfigured', owner: 'Xu' }, s.channels.weixin || {});
    if (!s.plugins) s.plugins = { github: { installed: true, connected: true, mode: 'oauth' }, notion: { installed: true, connected: false }, obsidian: { installed: true, connected: true }, brave: { installed: false, connected: false }, amap: { installed: false, connected: false }, tencent: { installed: false, connected: false }, markdown: { installed: true, connected: true } };
    pluginCatalog.forEach(p => { if (!s.plugins[p.id]) s.plugins[p.id] = { installed: false, connected: false }; });
    if (!s.machines) s.machines = [{ id: 'studio-linux', name: 'Studio Linux', address: '192.168.1.20', user: 'xu', port: '22', auth: 'agent', runtime: 'codex', model: 'default', status: 'ready', source: 'existing', host: 'studio-linux' }, { id: 'lab-ubuntu', name: 'Lab Ubuntu', address: '192.168.1.34', user: 'ubuntu', port: '22', auth: 'key', runtime: 'caelis', model: 'default', status: 'setup', source: 'existing', host: 'lab-ubuntu' }];
    s.permissions = Object.assign({ accessibility: true, screenCapture: false, notifications: true, automation: false }, s.permissions || {});
    return s;
  }
  function commit() { save(); render(); }
  function B(zh, en, action, variant = 'secondary', ico, attrs = '') { return btn(T(zh, en), 's:' + action, { variant, icon: ico, attrs }); }
  function input(bind, value, placeholder = '', type = 'text', extra = '') { return '<input type="' + type + '" data-input="s:' + bind + '" value="' + h(value) + '" placeholder="' + h(placeholder) + '" ' + extra + '>'; }
  function select(bind, value, options, extra = '') { return '<select data-bind="s:' + bind + '" ' + extra + '>' + options.map(o => '<option value="' + h(o[0]) + '"' + (String(value) === String(o[0]) ? ' selected' : '') + '>' + h(val(o[1])) + '</option>').join('') + '</select>'; }

  function row(title, description, control, cls = '') {
    const label = val(title);
    control = control.replace(/<(select|input|textarea)\b([^>]*)>/g, (whole, tag, attrs) =>
      /\baria-label(?:ledby)?=/.test(attrs) ? whole : '<' + tag + attrs + ' aria-label="' + h(label) + '">');
    return '<div class="setting-row ' + cls + '"><div class="row-copy"><h3>' + h(label) + '</h3>' +
      (description ? '<p>' + val(description) + '</p>' : '') + '</div><div class="row-control">' + control + '</div></div>';
  }

  function section(title, body, intro = '') { return '<section class="page-section"><div class="section-heading"><h2>' + h(val(title)) + '</h2>' + (intro ? '<p class="muted small">' + val(intro) + '</p>' : '') + '</div>' + body + '</section>'; }
  function header(title, subtitle, action = '') { return '<header class="page-header s-page-header"><div><h1>' + h(val(title)) + '</h1><p>' + val(subtitle) + '</p></div>' + action + '</header>'; }
  function disclosure(title, body, open = false) { return '<details class="disclosure s-disclosure"' + (open ? ' open' : '') + '><summary>' + icon('chevron', 15) + '<span>' + h(val(title)) + '</span></summary><div class="s-disclosure-body">' + body + '</div></details>'; }
  function note(zh, en, kind = '') { return '<div class="notice ' + kind + '">' + (kind === 'error' ? icon('warning', 17) : icon('info', 17)) + '<span>' + T(zh, en) + '</span></div>'; }
  function modelName(id) { const m = models.find(x => x.id === id); return m ? val(m.name) : id || T('使用默认模型', 'Use default model'); }
  function modelButton(kind, id) { return '<button class="s-value-button" data-action="s:model:' + h(kind) + '"><span>' + h(modelName(id)) + '</span>' + icon('chevron', 16) + '</button>'; }
  function statusLabel(status) { return ({ connected: T('已连接', 'Connected'), ready: T('可工作', 'Ready'), setup: T('待完成设置', 'Finish setup'), unconfigured: T('未连接', 'Not connected'), paused: T('已暂停', 'Paused'), offline: T('连接不可用', 'Unavailable'), waiting: T('等待授权', 'Awaiting authorization') })[status] || T('需要处理', 'Needs attention'); }
  function statusBadge(status) { return badge(statusLabel(status), ['connected', 'ready'].includes(status) ? 'success' : ['offline', 'error'].includes(status) ? 'danger' : ['setup', 'paused', 'waiting'].includes(status) ? 'warning' : 'neutral'); }
  function demoActions(success, fail, zh = '外部步骤仅作演示，不会访问真实服务。', en = 'External steps are simulated. No real service is contacted.') { return '<div class="s-demo-step"><span>' + icon('info', 14) + '<span>' + T('原型演示', 'Prototype step') + '</span></span><p>' + T(zh, en) + '</p><div class="actions">' + (success ? B('演示成功', 'Simulate success', success, 'ghost', 'check') : '') + (fail ? B('演示失败', 'Simulate failure', fail, 'ghost', 'warning') : '') + '</div></div>'; }
  function errorHTML(message) { return message ? '<div class="notice danger" role="alert">' + icon('warning', 17) + '<span>' + h(val(message)) + '</span></div>' : ''; }
  function flow(type, stage, values) { ui.flow = { type, stage: stage || 'start', v: values || {}, error: '', serial: ++ui.serial }; openModal(() => modal()); }
  function setStage(stage) { if (!ui.flow) return; ui.flow.stage = stage; ui.flow.error = ''; render(); }
  function clearTransient() { if (ui.flow && ui.flow.v) ['key', 'token', 'password', 'secret'].forEach(k => { delete ui.flow.v[k]; }); }
  function endFlow() { clearTransient(); ui.flow = null; closeModal(); }
  function footerCancel(action = 'cancel', label = ['取消', 'Cancel']) { return B(label[0], label[1], action, 'ghost'); }
  function pName(p) { return val(p.name); }
  function focusAfter(id) { setTimeout(() => { const e = document.getElementById(id); if (e) { e.focus(); if (typeof e.setSelectionRange === 'function') { const n = e.value.length; e.setSelectionRange(n, n); } } }, 0); }


  function generalPage() {
    const s = d();
    const isCustom = s.terminal === 'custom';
    const termDesc = isCustom
      ? ['从桌宠的任务预览打开。' + (s.customTerminal ? '（' + s.customTerminal + '）' : '') + ' <button type="button" class="s-inline-action" data-action="s:terminal-edit">编辑命令…</button>',
         'Used to observe a task from the companion. <button type="button" class="s-inline-action" data-action="s:terminal-edit">Edit command…</button>']
      : ['从桌宠的任务预览打开。', 'Used to observe a task from the companion.'];
    return header(['常规','General'],['语言、启动方式和日常工作偏好。','Language, startup and everyday work preferences.']) +
      section(['偏好','Preferences'],
        row(['语言','Language'],['界面语言可随时更改。','Change the interface language at any time.'],
          select('language',s.language || state.lang,[['system',['跟随系统','System default']],['zh','简体中文'],['en','English']])) +
        row(['登录时自动启动','Open at login'],['恢复桌面陪伴，不自动打开聊天。','Restore the companion without opening chat.'],
          toggle('s:pref:startup',s.startup,T('登录时自动启动','Open at login')))) +
      section(['后台工作','Background work'],
        row(['同时运行数量','Concurrent tasks'],['调低数量不会停止正在进行的工作。','Lowering the limit keeps existing work running.'],
          input('concurrency',s.concurrency,'6','number','min="1" class="s-number"')) +
        row(['查看任务的终端','Terminal for tasks'],termDesc,
          select('pref:terminal',s.terminal,[['system',['系统默认','System default']],['terminal','Terminal'],['iterm','iTerm2'],['custom',['自定义终端…','Custom…']]])) +
        (isCustom ? row(['自定义终端命令','Custom command'],[s.customTerminal ? '当前命令: ' + s.customTerminal : '未配置命令，将使用默认终端。', s.customTerminal ? 'Command: ' + s.customTerminal : 'Not configured. Default will be used.'], B(s.customTerminal ? '修改…' : '配置…', s.customTerminal ? 'Edit…' : 'Configure…', 'terminal-edit')) : '')) +
      section(['快捷键','Keyboard shortcuts'],
        row(['唤出聊天','Show or hide chat'],['在其他应用中，也能回到对话。','Return to the conversation from another app.'],B(s.chatShortcut,s.chatShortcut,'shortcut:chat')) +
        row(['展开任务预览','Show task previews'],['按住展开固定任务的预览。','Hold to preview pinned tasks.'],B(s.taskShortcut,s.taskShortcut,'shortcut:task'))) +
      section(['本机存储','Local storage'],
        row(['附件副本','Attachment copies'],s.storageCleaned ? ['已清理 30 天前的副本。','Copies older than 30 days have been cleared.'] :
          ['128 个文件，原始文件和聊天记录分别保留。','128 files. Originals and chat history are kept separately.'],
          '<span class="s-metric">' + (s.storageCleaned ? '142 MB' : '284 MB') + '</span>') +
        row(['清理旧附件','Clear older attachments'],s.storageCleaned ? ['暂无可清理的旧副本。','No older copies to clear.'] :
          ['可清理 142 MB，范围为 30 天前的副本。','142 MB available from copies older than 30 days.'],
          s.storageCleaned ? badge(T('已清理','Cleared')) : B('清理…','Clear…','storage-clean')));
  }

  function appearancePage() {
    const s = d();
    const pet = s.character === 'stick' ? '<svg class="s-stick" viewBox="0 0 160 240" aria-hidden="true"><circle cx="80" cy="38" r="24" fill="none" stroke="currentColor" stroke-width="7"/><path d="M80 64v88m0-65-42 40m42-40 44 37m-44 28-34 62m34-62 36 62" fill="none" stroke="currentColor" stroke-width="7" stroke-linecap="round"/></svg>' : '<img src="' + ASSETS.pet + '" alt="' + h(T('Caelis 桌宠形象', 'Caelis companion')) + '">';
    return header(['外观', 'Appearance'], ['桌面上的存在感与视觉偏好，由你决定。', 'Personalize your desktop pet appearance and visual theme.']) +
      '<div class="s-appearance-preview">' +
        '<div class="s-preview-caption">' +
          '<span class="s-preview-tag">' + icon('sparkles', 12) + '<span>' + T('形象预览', 'COMPANION PREVIEW') + '</span></span>' +
          '<h2>' + h(state.botName) + '</h2>' +
          '<p>' + T('换个样子，还是同一个助手。', 'A different look. The same companion.') + '</p>' +
          '<span class="s-preview-foot-badge">' + icon('image', 12) + '<span>' + T('拖动桌宠可调整桌面位置', 'Drag your companion to reposition it') + '</span></span>' +
        '</div>' +
        '<div class="s-pet-stage" style="--pet-scale:' + (Number(s.petSize) / 100) + '">' + pet + '</div>' +
      '</div>' +
      section(['形象', 'Companion'], row(['显示桌宠', 'Show desktop companion'], ['隐藏形象不会停止聊天、后台工作或消息渠道。', 'Hiding your companion keeps conversations and background work running.'], toggle('s:pref:petVisible', s.petVisible, T('显示桌宠', 'Show companion'))) + row(['角色与服装', 'Character & outfit'], '', select('pref:character', s.character, [['caelis', ['小爱 · 花园套装', 'Caelis · Garden outfit']], ['stick', ['简约火柴人', 'Simple stick figure']]])) + row(['桌宠大小', 'Companion size'], ['随时调整，保持原有比例。', 'Resize while keeping the original proportions.'], '<div class="s-range-control"><input type="range" min="65" max="160" value="' + Number(s.petSize) + '" data-input="s:petSize" aria-label="' + T('桌宠大小', 'Companion size') + '"><output data-s-value="petSize">' + Number(s.petSize) + '%</output></div>') + row(['聊天头像', 'Chat avatar'], '', select('pref:avatarMode', s.avatarMode, [['follow', ['跟随桌宠', 'Follow companion']], ['caelis', ['使用 Caelis 头像', 'Use Caelis avatar']]]))) +
      section(['界面', 'Interface'], row(['主题', 'Theme'], ['亮色、暗色，或跟随系统。', 'Light, dark, or matched to your system.'], '<div class="s-theme-options">' + [['system', 'appearance', ['系统', 'System']], ['light', 'sun', ['亮色', 'Light']], ['dark', 'moon', ['暗色', 'Dark']]].map(x => '<button class="s-theme-choice' + (state.theme === x[0] ? ' active' : '') + '" data-action="s:theme:' + x[0] + '" aria-pressed="' + (state.theme === x[0]) + '">' + icon(x[1], 17) + '<span>' + val(x[2]) + '</span></button>').join('') + '</div>')) +
      section(['扩展形象', 'Content packs'], row(['导入形象包', 'Import a character pack'], ['支持本机 .caelispack 文件。', 'Import a local .caelispack file.'], B('导入…', 'Import…', 'pack-import', 'secondary', 'upload')) + '<input id="s-pack-file" type="file" accept=".caelispack" data-bind="s:packFile" class="s-hidden-file" tabindex="-1">' + s.packs.map((p, i) => row([p.name, p.name], ['已导入 · 原型示例', 'Imported · Prototype example'], B('移除', 'Remove', 'pack-remove:' + i, 'ghost'))).join('') + (s.packs.length ? '' : '<p class="small muted s-inline-note">' + T('你的角色、头像和长期记忆彼此独立。', 'Character, avatar and long-term identity stay independent.') + '</p>'));
  }

  function aiPage() {
    const s = d(), a = s.ai;
    const convModel = models.find(m => m.id === a.conversationModel) || models[1];
    const workModel = models.find(m => m.id === a.workModel) || models[2];
    const effortLabel = { low: ['低', 'Low'], medium: ['中', 'Medium'], high: ['高', 'High'], xhigh: ['最高', 'Max'] }[a.effort] || ['高', 'High'];

    const hero = '<div class="s-ai-hero"><div class="s-app-monogram">' + icon('ai', 24) + '</div>' +
      '<div class="s-hero-copy"><div class="s-hero-title-line"><h2>' + (a.tool === 'codex' ? 'Codex' : 'Caelis') + '</h2>' +
      statusBadge(a.connected ? 'ready' : 'offline') + '</div><p>' +
      (a.connected ? icon('check', 13) + '<span>' + h(a.account) + ' · ' + T('本机沙箱连接就绪', 'Local sandbox ready') + '</span>' : T('连接账号后即可恢复对话与工作。', 'Connect an account to restore conversation and work.')) +
      '</p></div><div class="s-hero-actions">' +
      B(a.connected ? '管理连接' : '连接账号', a.connected ? 'Manage connection' : 'Connect account', 'ai-manage', a.connected ? 'secondary' : 'primary') +
      B('检测', 'Ping', 'ai-ping', 'ghost', 'refresh') +
      '</div></div>';

    const convCard = '<div class="s-tier-card"><div class="s-tier-content">' +
      '<span class="s-tier-badge">' + icon('chat', 13) + ' ' + T('日常对话', 'Resident Chat') + '</span>' +
      '<h3>' + T('与你对话的助手模型', 'Assistant Model') + '</h3>' +
      '<p>' + T('常驻桌面与你对话，反应灵敏，负责日常沟通与任务分派。', 'Resident companion for fast conversation and task dispatching.') + '</p>' +
      '<button type="button" class="s-tier-model-pill" data-action="s:model:conversation">' +
      '<span><strong>' + h(val(convModel.name)) + '</strong>' +
      (convModel.tags ? '<span class="s-pill-tag">' + val(convModel.tags[0]) + '</span>' : '') +
      '</span>' + icon('chevron', 14) + '</button>' +
      '</div><div class="s-tier-card-footer"><span>' + icon('sparkles', 13) + T('思考强度: ', 'Effort: ') + val(effortLabel) + '</span>' +
      B('切换…', 'Change…', 'model:conversation', 'ghost') + '</div></div>';

    const workCard = '<div class="s-tier-card"><div class="s-tier-content">' +
      '<span class="s-tier-badge">' + icon('terminal', 13) + ' ' + T('后台任务', 'Background Tasks') + '</span>' +
      '<h3>' + T('执行复杂任务的工作模型', 'Work & Code Model') + '</h3>' +
      '<p>' + T('负责深度研究、代码重构与独立制品制作，不阻塞日常对话。', 'Handles deep research, code refactoring and artifacts in background.') + '</p>' +
      '<button type="button" class="s-tier-model-pill" data-action="s:model:work">' +
      '<span><strong>' + h(val(workModel.name)) + '</strong>' +
      (workModel.tags ? '<span class="s-pill-tag">' + val(workModel.tags[0]) + '</span>' : '') +
      '</span>' + icon('chevron', 14) + '</button>' +
      '</div><div class="s-tier-card-footer"><span>' + icon('tools', 13) + T('工作工具: ', 'Tool: ') + (a.workTool === 'codex' ? 'Codex' : 'Caelis') + '</span>' +
      B('切换…', 'Change…', 'model:work', 'ghost') + '</div></div>';

    const twoTierSection = section(['核心模型配置', 'Model Configuration'],
      '<div class="s-model-tier-grid">' + convCard + workCard + '</div>',
      ['日常沟通与后台任务分别配置，兼顾速度与深度。', 'Separate conversation and background work for speed and reasoning depth.']);

    const runtimeSection = section(['运行环境与扩展服务', 'Runtime & Extended Services'],
      row(['新任务工作工具', 'Tool for new tasks'], ['已有任务继续沿用原来的工具环境。', 'Existing tasks retain their original environment.'],
        select('workTool', a.workTool, [['codex', 'Codex'], ['caelis', 'Caelis']])) +
      (a.tool === 'caelis' || a.workTool === 'caelis' ? row(['协作团队设置', 'Team configuration'], ['自定义多 Agent 协作团队（可选）。', 'Optional multi-agent team configuration.'], B('设置…', 'Edit…', 'ai-team', 'ghost')) : '') +
      row(['其他模型服务', 'Additional model services'], ['按需添加 OpenAI API Key、Moonshot、MiMo 或外部 Agent。', 'Add OpenAI API Key, Moonshot, MiMo or external Agent.'], B('管理…', 'Manage…', 'ai-provider', 'secondary', 'plus')) +
      row(['程序与数据位置', 'Program & data location'], ['自动检测本机已安装工具的路径。', 'Automatically discovered installation paths.'], B('查看位置…', 'View location…', 'ai-program', 'ghost')) +
      row(['连接状态核对', 'Connection check'], ['重新验证当前连接握手，保留会话历史和草稿。', 'Recheck handshake; preserves chat history and drafts.'], B('重新检查', 'Recheck', 'ai-recheck', 'ghost', 'refresh')),
      ['底层环境由本地服务提供，修改后即时生效。', 'Underlying runtime is managed locally and applies immediately.']);

    return header(['模型', 'Models'], ['驱动桌面助手的核心能力，管理连接与模型分工。', 'Manage connections and dedicated models for conversation and work.']) +
      hero + twoTierSection + runtimeSection;
  }

  function channelPage() {
    const s = d();
    return header(['消息渠道', 'Channels'], ['在常用的聊天应用中随时呼叫助手。', 'Message your companion in your everyday chat apps.']) +
      '<div class="s-channel-list">' + [['telegram', 'send', 'Telegram', ['在手机和其他设备上聊天。', 'Chat from your phone and other devices.']], ['weixin', 'wechat', T('微信', 'WeChat'), ['预览功能 · 一位用户的私聊文本。', 'Preview · Private text chat with one paired account.']]].map(x => '<button class="service-row s-channel-row" data-action="s:channel:' + x[0] + '"><span class="service-icon">' + icon(x[1], 25) + '</span><span class="service-copy"><span class="s-channel-name">' + h(x[2]) + (x[0] === 'weixin' ? badge(T('预览', 'Preview')) : '') + '</span><span class="muted">' + val(x[3]) + '</span></span>' + statusBadge(s.channels[x[0]].status) + icon('chevron', 16) + '</button>').join('') + '</div>' +
      note('保持 Caelis Bot 运行、这台电脑联网。关闭聊天窗口后，消息渠道仍会连接。', 'Keep Caelis Bot running and this computer online. Channels remain connected when you close the chat window.') +
      section(['连接方式', 'How it works'], '<div class="s-explain-grid"><div>' + icon('chat', 20) + '<h3>' + T('同一个对话', 'One conversation') + '</h3><p>' + T('共享助手的长期身份与上下文。', 'Share your companion’s identity and context.') + '</p></div><div>' + icon('lock', 20) + '<h3>' + T('确认你的账号', 'Your account, confirmed') + '</h3><p>' + T('仅接收你明确配对的私人聊天。', 'Only private chats you explicitly pair are accepted.') + '</p></div></div>');
  }
  function pluginsPage() {
    const s = d(), installed = Object.values(s.plugins).filter(x => x.installed).length;
    const items = pluginCatalog.filter(p => (ui.pluginTab !== 'installed' || s.plugins[p.id].installed) && (pName(p) + p.zh + p.en).toLowerCase().includes(ui.pluginQuery.toLowerCase()));
    return header(['插件', 'Plugins'], ['让助手与熟悉的工具一起工作。', 'Bring familiar tools into your companion’s work.']) +
      '<div class="s-list-toolbar"><div class="tabs"><button data-action="s:plugin-tab:all" class="' + (ui.pluginTab === 'all' ? 'active' : '') + '" aria-pressed="' + (ui.pluginTab === 'all') + '">' + T('浏览', 'Browse') + '</button><button data-action="s:plugin-tab:installed" class="' + (ui.pluginTab === 'installed' ? 'active' : '') + '" aria-pressed="' + (ui.pluginTab === 'installed') + '">' + T('已安装', 'Installed') + '<span>' + installed + '</span></button></div><div class="s-search">' + icon('search', 16) + '<input id="s-plugin-search" type="search" data-input="s:pluginQuery" value="' + h(ui.pluginQuery) + '" placeholder="' + T('搜索插件', 'Search plugins') + '" aria-label="' + T('搜索插件', 'Search plugins') + '"></div></div>' +
      (items.length ? '<div class="s-plugin-list">' + items.map(p => { const q = s.plugins[p.id]; return '<div class="service-row s-service-row"><button class="s-service-open" data-action="s:plugin:' + p.id + '"><span class="service-icon">' + icon(p.icon, 24) + '</span><span class="service-copy"><span class="s-channel-name">' + h(pName(p)) + '</span><span class="muted">' + T(p.zh, p.en) + '</span></span></button><div class="s-service-end">' + (q.installed ? badge(q.connected || !p.auth ? T('已安装', 'Installed') : T('待连接', 'Needs connection'), q.connected || !p.auth ? 'neutral' : 'warning') + (!q.connected && p.auth ? B('连接', 'Connect', 'plugin-connect:' + p.id, 'secondary') : B('管理', 'Manage', 'plugin:' + p.id, 'secondary')) : B('安装', 'Install', 'plugin-install:' + p.id, 'secondary')) + '</div></div>'; }).join('') + '</div>' : '<div class="empty-state">' + icon('search', 28) + '<h3>' + T('没有匹配的插件', 'No matching plugins') + '</h3><p>' + T('试试另一个名称，或清空搜索。', 'Try another name or clear the search.') + '</p>' + B('清空搜索', 'Clear search', 'plugin-clear', 'ghost') + '</div>');
  }
  function machinesPage() {
    const s = d();
    return header(['远程', 'Remote'], ['需要时，让其他 Linux 机器帮你完成工作。', 'Let remote Linux machines help when you need them.'], B('添加机器', 'Add machine', 'machine-add', 'primary', 'plus')) +
      '<div class="s-local-machine"><span class="service-icon">' + icon('machine', 22) + '</span><div><strong>' + T('这台 Mac', 'This Mac') + '</strong><p>' + T('未指定机器时，默认在这里工作。', 'Work starts here unless you specify another machine.') + '</p></div>' + badge(T('默认', 'Default')) + '</div>' +
      section(['已添加的机器', 'Your machines'], s.machines.length ? s.machines.map(m => '<button class="service-row s-channel-row" data-action="s:machine:' + h(m.id) + '"><span class="service-icon">' + icon('terminal', 24) + '</span><span class="service-copy"><span class="s-channel-name">' + h(m.name) + '</span><span class="muted">' + h(m.address) + ' · ' + (m.runtime === 'codex' ? 'Codex' : 'Caelis') + '</span></span>' + statusBadge(m.status) + icon('chevron', 16) + '</button>').join('') : '<div class="empty-state">' + icon('machine', 32) + '<h3>' + T('把工作交给另一台机器', 'Make room for another machine') + '</h3><p>' + T('连接可通过 SSH 访问的 Linux 机器，再为它单独登录工作账号。', 'Connect a Linux machine over SSH, then sign in to its own work account.') + '</p>' + B('添加机器', 'Add machine', 'machine-add', 'secondary', 'plus') + '</div>') +
      note('支持 x86_64 或 arm64 Linux 机器与 SSH 连接。连接后在聊天中明确指定机器，每台机器独立登录，账号与凭据留在原处，不会自动迁移或同步。', 'Supports x86_64 or arm64 Linux and SSH connections. Each machine signs in independently with credentials staying remote.');
  }
  function toolsPage() {
    const s = d();
    return header(['附加功能', 'Extra tools'], ['桌面截屏、区域标注与桌面贴图辅助工具。', 'Desktop capture, region annotation, and pin-to-desktop utilities.']) +
      '<div class="s-tool-preview"><div class="s-mini-window"><span></span><span></span><span></span><div class="s-capture-region">' + icon('image', 26) + '<small>' + T('选区', 'Selection') + '</small></div></div><div><h2>' + T('截取、标注，然后交给助手', 'Capture, annotate, then ask') + '</h2><p>' + T('选中窗口或屏幕区域。分享之前，你可以标注和遮盖私密内容。', 'Select a window or screen area. Annotate or cover private details before sharing.') + '</p></div></div>' +
      section(['桌面截图与贴图', 'Desktop Capture & Pin'], row(['启用截屏与贴图', 'Enable capture & pin'], ['关闭后停用这些操作，普通聊天图片不受影响。', 'Turning this off does not disable ordinary image attachments in chat.'], toggle('s:pref:captureEnabled', s.captureEnabled, T('启用截屏与贴图', 'Enable capture and pin'))) + (s.captureEnabled ? row(['截图快捷键', 'Capture shortcut'], ['截屏、标注或询问助手。', 'Capture, annotate or ask your companion.'], B(s.captureShortcut, s.captureShortcut, 'shortcut:capture')) + row(['贴图快捷键', 'Pin image shortcut'], ['将当前选区或剪贴板图片贴到桌面。', 'Pin your selection or a clipboard image to the desktop.'], B(s.pasteShortcut, s.pasteShortcut, 'shortcut:paste')) : '')) +
      (s.permissions.screenCapture ? note('屏幕录制权限已允许。仅在你主动截图时使用。', 'Screen Recording is allowed. It is used when you explicitly capture content.') : note('需要屏幕录制权限才能截图。你仍可以向聊天添加已有图片。', 'Screen Recording permission is needed for capture. You can still attach existing images.')) +
      '<div class="actions s-section-end">' + B('体验截图流程', 'Try the capture flow', 'capture-demo', 'secondary', 'image', s.captureEnabled ? '' : 'disabled') + B('管理系统权限', 'Manage permissions', 'goto:privacy', 'ghost', 'shield') + '</div>';
  }
  function permissionRow(id) {
    const s = d(), p = permissionInfo[id];
    return '<div class="s-permission-item">' +
      '<div class="perm-icon">' + icon(p[4] || 'shield', 18) + '</div>' +
      '<div class="perm-copy"><h4>' + val([p[0], p[1]]) + '</h4><p>' + val([p[2], p[3]]) + '</p></div>' +
      '<div class="s-status-action">' +
      badge(s.permissions[id] ? T('已允许', 'Allowed') : T('未允许', 'Not allowed'), s.permissions[id] ? 'success' : 'neutral') +
      B(s.permissions[id] ? '管理' : '允许…', s.permissions[id] ? 'Manage' : 'Allow…', 'permission:' + id, s.permissions[id] ? 'ghost' : 'secondary') +
      '</div></div>';
  }
  function privacyPage() {
    const s = d();
    const modeHelp = {
      auto: ['保留命令沙箱，由 AI 自动审查额外权限。', 'Keep the command sandbox and automatically review extra access.'],
      ask: ['需要额外权限时，由你确认。', 'Ask you when an action needs extra access.'],
      'read-only': ['本地命令使用只读沙箱；连接器仍遵循各自授权。', 'Local commands use a read-only sandbox. Connectors keep their own authorization.'],
      'full-access': ['关闭命令沙箱，不再询问执行权限。', 'Disable the command sandbox and execution approval prompts.']
    };

    const permissionsSection = section(['系统权限管理', 'System Permissions'],
      '<div class="s-permission-list">' +
      permissionRow('accessibility') +
      permissionRow('screenCapture') +
      permissionRow('notifications') +
      permissionRow('automation') +
      '</div>',
      ['由 macOS 统一管理授权。未获授权的功能不会静默尝试调用。', 'Managed by macOS. Denied capabilities are never silently called.']);

    const executionSection = section(['安全审查与命令沙箱', 'Execution Review & Sandbox'],
      row(['额外权限审查方式', 'Action review policy'],
        d().ai.tool === 'caelis' ? ['由 Guardian 自动审查，基础工作保留沙箱。', 'Guardian reviews extra access while sandbox stays active.'] : modeHelp[s.approvalMode],
        d().ai.tool === 'caelis' ? badge(T('自动审查', 'Automatic review'), 'success') :
          select('approvalMode', s.approvalMode, [
            ['auto', ['自动审查（推荐）', 'Automatic review (Recommended)']],
            ['ask', ['每次确认', 'Ask me']],
            ['read-only', ['只读沙箱', 'Read-only sandbox']],
            ['full-access', ['完全访问', 'Full access']]
          ])));

    const troubleshootingSection = section(['权限排障与重置', 'Permission Troubleshooting & Reset'],
      row(['重新核对权限', 'Recheck permissions'],
        ['从系统读取最新授权状态。', 'Read latest status from macOS.'],
        B('重新检查', 'Recheck', 'permissions-recheck', 'secondary', 'refresh')
      ) +
      row(['需要修复的权限类别', 'Permission category to reset'],
        ['选择需要重置授权状态的类别。', 'Choose the permission category to reset.'],
        select('pref:repairPermission', s.repairPermission, [
          ['accessibility', ['辅助功能', 'Accessibility']],
          ['screenCapture', ['屏幕录制', 'Screen Recording']],
          ['automation', ['自动化', 'Automation']]
        ])
      ) +
      row(['重置授权记录', 'Reset authorization'],
        ['重置所选类别的系统授权记录，重置后可在系统设置中重新允许。', 'Reset macOS authorization for the selected category so it can be requested again.'],
        B('重置此项授权…', 'Reset permission…', 'permission-reset', 'ghost')
      ),
      ['遇到权限不生效或状态异常时，可在此进行排障。', 'Troubleshoot permission issues or reset stuck macOS authorizations.']
    );

    return header(['隐私与权限', 'Privacy & permissions'], ['掌控系统的权限边界，管理安全审查与命令沙箱。', 'Manage macOS access boundaries, execution review, and sandbox policies.']) +
      permissionsSection + executionSection + troubleshootingSection;
  }
  function aboutPage() {
    const s = d();
    const text = s.updateStatus === 'checking' ? T('正在检查…', 'Checking…') : s.updateStatus === 'available' ? T('有可用更新 · 演示版本', 'Update available · Demo release') : s.updateStatus === 'downloaded' ? T('演示更新已准备好', 'Demo update ready') : T('当前为交互设计预览', 'You are viewing the interactive design preview');
    return header(['关于', 'About'], ['持续改进，安静陪伴。', 'Thoughtful improvements, quietly delivered.']) +
      '<div class="s-about-identity"><img src="' + ASSETS.avatar + '" alt=""><div><h2>Caelis Bot</h2><p>' + T('桌面上的小小陪伴，随时准备帮忙。', 'A little companion on your desktop. Ready to help.') + '</p><span class="small muted">Design preview · 2026.10</span></div></div>' +
      section(['软件更新', 'Software updates'], row(['自动检查更新', 'Check automatically'], ['有新版本时提醒，安装前仍由你确认。', 'Let you know about new releases and confirm before installing.'], toggle('s:pref:updatesAutomatic', s.updatesAutomatic, T('自动检查更新', 'Check for updates automatically'))) + row(['更新状态', 'Update status'], text, s.updateStatus === 'checking' ? badge(T('检查中', 'Checking')) : s.updateStatus === 'available' ? B('准备更新', 'Prepare update', 'update-download', 'primary', 'download') : s.updateStatus === 'downloaded' ? B('重新启动并更新', 'Restart & update', 'update-install', 'primary') : B('检查更新', 'Check for updates', 'update-check', 'secondary', 'refresh'))) +
      section(['支持', 'Support'], row(['诊断报告', 'Diagnostics report'], ['导出连接与状态信息，便于排查问题。', 'Export connection and status information for troubleshooting.'], B('导出报告', 'Export report', 'diagnostics', 'secondary', 'download')) + row(['发布与安装', 'Releases & installation'], ['查看更新内容和安装说明。', 'Read release notes and installation guidance.'], B('查看', 'View', 'release-info', 'ghost', 'open'))) +
      '<div class="s-about-foot"><span>Caelis Labs</span><span>' + T('此文件是本地交互原型，所有账号和服务状态均为示例。', 'This is a local interactive prototype. Accounts and service states are examples.') + '</span></div>';
  }

  function qr() {
    let cells = '';
    for (let y = 0; y < 21; y++) for (let x = 0; x < 21; x++) {
      const finder = (ox, oy) => { const dx = x - ox, dy = y - oy; return dx >= 0 && dx < 7 && dy >= 0 && dy < 7 && (dx === 0 || dy === 0 || dx === 6 || dy === 6 || (dx >= 2 && dx <= 4 && dy >= 2 && dy <= 4)); };
      const inFinder = (x < 8 && y < 8) || (x > 12 && y < 8) || (x < 8 && y > 12);
      if (finder(0, 0) || finder(14, 0) || finder(0, 14) || (!inFinder && ((x * 7 + y * 11 + x * y) % 5 < 2))) cells += '<rect x="' + (x + 2) + '" y="' + (y + 2) + '" width="1" height="1"/>';
    }
    return '<div class="s-qr-wrap"><svg viewBox="0 0 25 25" class="s-demo-qr" role="img" aria-label="' + T('演示二维码，无法扫描', 'Demo QR code, not scannable') + '"><rect width="25" height="25" fill="#fff"/><g fill="#252729">' + cells + '</g></svg><span>' + T('示例二维码 · 无法扫码', 'Demo QR · Not scannable') + '</span></div>';
  }
  function steps(labels, current) { return '<div class="s-steps">' + labels.map((x, i) => '<span class="' + (i === current ? 'current' : i < current ? 'done' : '') + '"><i>' + (i < current ? icon('check', 11) : i + 1) + '</i>' + val(x) + '</span>').join('') + '</div>'; }
  function doneMark(title, body) { return '<div class="s-result-block"><div class="s-result-icon">' + icon('check', 26) + '</div><h3>' + val(title) + '</h3><p>' + val(body) + '</p></div>'; }

  function modal() {
    const f = ui.flow;
    if (!f) return { title: T('设置', 'Settings'), body: '', footer: footerCancel() };
    if (f.type === 'terminal') return {title:T('终端配置','Terminal configuration'), subtitle:T('设置任务观察使用的自定义启动命令。','Configure the custom command for observing tasks.'), body:field(T('启动命令','Launch command'), '<textarea data-input="s:flow:command" rows="3" placeholder="&quot;/path/to/terminal&quot; --execute /bin/sh {script}">'+h(f.v.command)+'</textarea>', T('留空使用所选终端的默认启动方式。{script} 代表任务脚本。','Leave empty for the selected terminal’s default. {script} is the task script.')), footer:footerCancel()+B('保存','Save','terminal-save','primary')};
    if (f.type === 'model') return modelModal(f);
    if (f.type === 'ai') return aiModal(f);
    if (f.type === 'telegram') return telegramModal(f);
    if (f.type === 'weixin') return weixinModal(f);
    if (f.type === 'plugin') return pluginModal(f);
    if (f.type === 'plugin-auth') return pluginAuthModal(f);
    if (f.type === 'machine') return machineModal(f);
    if (f.type === 'permission') return permissionModal(f);
    if (f.type === 'shortcut') return shortcutModal(f);
    if (f.type === 'capture') return { title: T('截屏与贴图', 'Capture & pin'), subtitle: T('在分享之前，先决定分享什么。', 'Choose what to share before you share it.'), body: '<div class="s-capture-demo"><div class="s-capture-toolbar">' + ['image', 'eye', 'copy', 'pin'].map(x => icon(x, 18)).join('') + '</div><div class="s-capture-selection">' + icon('image', 40) + '<strong>' + T('你选择的屏幕区域', 'Your selected screen area') + '</strong><small>' + T('屏幕画面不会在此原型中采集', 'No real screen content is captured in this prototype') + '</small></div></div>' + note('实际应用中可标注、脱敏、复制、贴图，或发送给支持图片的当前模型。', 'In the app, annotate, redact, copy, pin, or send to the current image-capable model.'), footer: footerCancel() + B('发送到聊天', 'Send to chat', 'capture-send', 'primary') };
    if (f.type === 'info') return { title: val(f.v.title), body: f.v.body(), footer: B('完成', 'Done', 'cancel', 'primary'), width: 'medium' };
    return { title: T('设置', 'Settings'), body: '', footer: footerCancel() };
  }
  function modelModal(f) {
    const machine = f.v.kind === 'machine', work = f.v.kind === 'work';
    const cat = ui.modelCategory || 'all';
    const list = models.filter(m => {
      if (machine || d().ai.tool !== 'caelis') {
        if (m.id === 'mimo-v2.6-flash' && d().ai.tool !== 'caelis') return false;
      }
      if (cat !== 'all' && m.category !== cat && m.id !== 'default') return false;
      return (val(m.name) + val(m.detail) + (m.provider || '')).toLowerCase().includes(ui.modelQuery.toLowerCase());
    });
    const selected = models.find(m => m.id === f.v.model);

    const categories = [
      ['all', ['全部模型', 'All']],
      ['chat', ['日常推荐', 'Chat']],
      ['reasoning', ['深度推理', 'Reasoning']],
      ['speed', ['轻量快速', 'Speed']]
    ];

    const categoryTabs = '<div class="s-picker-categories">' +
      categories.map(([id, label]) => '<button type="button" class="' + (cat === id ? 'active' : '') + '" data-action="s:model-tab:' + id + '">' + val(label) + '</button>').join('') +
      '</div>';

    const searchBar = '<div class="s-search s-model-search">' + icon('search', 16) +
      '<input id="s-model-search" type="search" data-input="s:modelQuery" value="' + h(ui.modelQuery) + '" placeholder="' + T('搜索可用模型或提供商…', 'Search models or providers…') + '" aria-label="' + T('搜索模型', 'Search models') + '"></div>';

    const modelList = '<div class="s-picker-list" role="listbox">' +
      list.map(m => {
        const isSelected = f.v.model === m.id;
        const tagHTML = m.tags ? m.tags.map(t => '<span class="feature-tag">' + val(t) + '</span>').join('') : '';
        return '<button type="button" role="option" aria-selected="' + isSelected + '" data-action="s:model-choose:' + m.id + '" class="s-picker-item ' + (isSelected ? 'is-selected' : '') + '">' +
          '<div class="s-picker-item-info">' +
          '<div class="s-picker-item-header"><strong>' + h(val(m.name)) + '</strong>' +
          (m.provider ? '<span class="provider-tag">' + h(m.provider) + '</span>' : '') +
          tagHTML + '</div>' +
          '<p>' + val(m.detail) + '</p>' +
          '</div>' + (isSelected ? icon('check', 18) : '') +
          '</button>';
      }).join('') +
      (!list.length ? '<div class="empty-state" style="padding:20px 10px"><p class="muted small">' + T('没有找到匹配的模型', 'No matching models found') + '</p></div>' : '') +
      '</div>';

    const effortLevels = [
      ['low', ['低', 'Low'], ['极简步骤，更快响应', 'Minimal steps, fastest response']],
      ['medium', ['中', 'Medium'], ['平衡思考与速度', 'Balanced thinking and speed']],
      ['high', ['高', 'High'], ['深入思考，适合代码', 'Deep reasoning, ideal for code']],
      ['xhigh', ['最高', 'Max'], ['最大推理深度，复杂架构', 'Max depth, complex architecture']]
    ];

    const currentEffort = f.v.effort || 'high';
    const effortSection = selected && selected.effort ?
      '<div class="s-model-parameters"><div class="field"><label>' + T('思考与推理强度', 'Reasoning Effort') + '</label>' +
      '<div class="s-effort-selector">' +
      effortLevels.map(([id, label]) => '<button type="button" class="' + (currentEffort === id ? 'active' : '') + '" data-action="s:model-effort:' + id + '">' + val(label) + '</button>').join('') +
      '</div><small class="field-help">' + val(effortLevels.find(e => e[0] === currentEffort)?.[2] || '') + '</small></div></div>' : '';

    return {
      title: machine ? T('默认工作模型', 'Default Work Model') : work ? T('执行复杂任务的工作模型', 'Model for Background Tasks') : T('与你日常对话的模型', 'Resident Conversation Model'),
      subtitle: T('选择适用的模型，并调整思考与推理参数。', 'Choose a model and adjust its reasoning parameters.'),
      body: searchBar + categoryTabs + modelList + effortSection + errorHTML(f.error),
      footer: B('恢复默认', 'Use default', 'model-default', 'ghost', 'refresh') + footerCancel() + B('保存偏好', 'Save', 'model-save', 'primary'),
      width: 'medium'
    };
  }
  function aiModal(f) {
    const s = d(), a = s.ai, v = f.v;

    if (f.stage === 'manage') return {
      title:T('管理 AI 连接','Manage AI connection'),subtitle:T('管理当前账号和连接工具。','Manage the current account and connection tool.'),
      body:'<div class="s-manage-profile"><span class="service-icon">'+icon('ai',24)+'</span><div><strong>'+(a.tool==='codex'?'Codex':'Caelis')+
        '</strong><p>'+h(a.account)+'</p></div>'+statusBadge(a.connected?'connected':'offline')+'</div>'+
        row(['当前账号','Current account'],a.connected?['本机账号已连接。','The local account is connected.']:['请重新登录以恢复连接。','Sign in again to restore the connection.'],B('重新登录','Sign in again','ai-login'))+
        row(['连接工具','Connection tool'],['切换前会提示重新启动。','A restart is confirmed before switching.'],B('更换…','Change…','ai-choice'))+
        row(['程序与数据位置','Program & data location'],['默认自动检测，也可指定位置。','Discover automatically or choose a location.'],B('查看…','View…','ai-program','ghost'))+
        '<div class="s-section-end">'+B('退出账号…','Sign out…','ai-logout','ghost')+'</div>',
      footer:B('完成','Done','cancel','primary')};
    if (f.stage === 'program') return {
      title:T('程序与数据位置','Program & data location'),subtitle:T('留空时使用自动检测和默认位置。','Leave empty for automatic discovery and default locations.'),
      body:'<div class="form-stack">'+field(T('程序位置','Program location'),input('flow:program',v.program||'',T('自动检测','Automatic discovery')))+
        (a.tool==='caelis'?field(T('Caelis 数据目录','Caelis data directory'),input('flow:store',v.store||'',T('使用默认目录','Default directory'))):'')+
        '</div>'+errorHTML(f.error),footer:footerCancel()+B('保存','Save','ai-manage-save','primary')};
    if (f.stage === 'choice') return { title: T('选择 AI 连接', 'Choose an AI connection'), subtitle: T('使用已有账号，或连接其他模型服务。', 'Use your existing account or connect another model service.'), body: '<div class="s-choice-list"><button data-action="s:ai-pick:codex"><span class="service-icon">' + icon('ai', 23) + '</span><span><strong>' + T('ChatGPT 账号', 'ChatGPT account') + '</strong><small>' + T('通过 Codex · 已检测到本机工具', 'Through Codex · Local tool detected') + '</small></span>' + icon('chevron', 16) + '</button><button data-action="s:ai-pick:caelis"><span class="service-icon">' + icon('key', 23) + '</span><span><strong>' + T('其他 AI 模型服务', 'Other AI model services') + '</strong><small>' + T('通过 Caelis · 账号登录或 API Key', 'Through Caelis · Account login or API key') + '</small></span>' + icon('chevron', 16) + '</button></div>' + note('工具与账号各自保留配置。切换连接时会明确提示重新启动。', 'Tools keep their own configuration. Changing the connection will ask you to restart.'), footer: footerCancel() };
    if (f.stage === 'provider') return { title: T('添加模型服务', 'Add a model service'), subtitle: T('连接信息会交给 Caelis 管理。', 'Caelis manages the connection details.'), body: '<div class="tabs s-auth-tabs"><button data-action="s:ai-auth-kind:account" class="' + (v.authKind === 'account' ? 'active' : '') + '">' + T('账号登录', 'Account login') + '</button><button data-action="s:ai-auth-kind:key" class="' + (v.authKind === 'key' ? 'active' : '') + '">API Key</button><button data-action="s:ai-auth-kind:agent" class="' + (v.authKind === 'agent' ? 'active' : '') + '">' + T('外部 Agent', 'External agent') + '</button></div>' + (v.authKind === 'agent' ? field(T('启动命令', 'Launch command'), input('flow:command', v.command || '', 'agent --acp')) + note('外部 Agent 需完成安装与认证。此处只演示连接配置。', 'External agents require installation and authentication. This prototype only demonstrates setup.') : field(T('模型服务', 'Model service'), select('flow:provider', v.provider || 'openai', [['openai', 'OpenAI'], ['moonshot', 'Moonshot'], ['mimo', 'MiMo'], ['custom', [ '自定义服务', 'Custom service' ]]])) + (v.authKind === 'key' ? field(T('服务地址', 'Service URL'), input('flow:baseUrl', v.baseUrl || '', 'https://…/v1', 'url')) + field(T('模型', 'Model'), input('flow:modelName', v.modelName || '', T('选择或输入模型名称', 'Choose or enter a model name'))) + field('API Key', input('flow:key', '', 'sk-…', 'password', 'autocomplete="off" spellcheck="false"')) + '<div class="s-modal-section"><div class="section-heading"><h3>' + T('模型能力（可选）', 'Model capabilities (optional)') + '</h3></div><div class="form-grid">' + field(T('上下文窗口', 'Context window'), input('flow:context', v.context || '', '', 'number', 'min="1"')) + field(T('最大输出', 'Maximum output'), input('flow:maxOutput', v.maxOutput || '', '', 'number', 'min="1"')) + '</div>' + row(['支持图片输入', 'Supports image input'], '', toggle('s:flow:imageInput', !!v.imageInput, T('支持图片输入', 'Supports image input'))) + '</div>' : note('下一步将在浏览器中完成所选服务的登录。', 'Next, sign in to the selected service in your browser.'))) + errorHTML(f.error), footer: footerCancel() + B(v.authKind === 'key' ? '连接' : v.authKind === 'agent' ? '检查 Agent' : '继续登录', v.authKind === 'key' ? 'Connect' : v.authKind === 'agent' ? 'Check agent' : 'Continue to sign in', 'ai-submit', 'primary'), width: 'medium' };
    if (f.stage === 'codex-key') return { title: T('使用 OpenAI API Key', 'Use an OpenAI API key'), body: field('API Key', input('flow:key', '', 'sk-…', 'password', 'autocomplete="off"')) + note('这项连接使用 Codex。密钥仅作为本地原型的临时输入，不会发出请求或保存。', 'This connection uses Codex. In this prototype the key is temporary, never sent or saved.') + errorHTML(f.error), footer: footerCancel() + B('连接', 'Connect', 'ai-submit-key', 'primary') };
    if (f.stage === 'waiting') return { title: T('等待浏览器授权', 'Waiting for authorization'), subtitle: (v.tool === 'caelis' ? 'Caelis' : 'Codex') + ' · ' + (v.account || 'ChatGPT'), body: '<div class="s-waiting-state"><div class="s-spinner"></div><h3>' + T('请在浏览器中完成登录', 'Finish signing in through your browser') + '</h3><p>' + T('收到确认后，账号才会显示为已连接。', 'Your account is connected only after confirmation.') + '</p></div>' + '<div class="actions s-center-actions">' + B('重新打开登录页面', 'Reopen login page', 'ai-browser', 'secondary', 'open') + B('重新检查', 'Recheck', 'ai-check-wait', 'ghost', 'refresh') + '</div>' + (v.tool !== 'caelis' ? '<div class="s-section-end">' + B('改用 API Key', 'Use an API key', 'ai-key', 'ghost') + '</div>' : '') + demoActions('ai-success', 'ai-fail'), footer: footerCancel('cancel', ['取消连接', 'Cancel connection']) };
    if (f.stage === 'checking') return { title: T('正在检查连接', 'Checking connection'), body: '<div class="s-waiting-state"><div class="s-spinner"></div><h3>' + T('核对账号与模型状态', 'Checking account and model status') + '</h3><p>' + T('这不会重新提交未知结果的操作。', 'This does not repeat an operation with an unknown result.') + '</p></div>' + demoActions('ai-success', 'ai-fail'), footer: footerCancel() };
    if (f.stage === 'failed') return { title: T('连接未确认', 'Connection not confirmed'), body: '<div class="s-result-block s-result-warning"><div class="s-result-icon">' + icon('warning', 25) + '</div><h3>' + T('暂时无法确认连接', 'We could not confirm the connection') + '</h3><p>' + T('请检查网络，并重新核对账号与模型状态。原有历史和草稿仍然保留。', 'Check the connection and recheck the account. Your history and drafts stay in place.') + '</p></div>' + errorHTML(f.error), footer: B('返回设置', 'Back to settings', 'ai-choice', 'ghost') + B('重新检查', 'Recheck', 'ai-retry', 'primary', 'refresh') };
    return { title: T('连接已准备好', 'Connection ready'), body: doneMark(['可以开始对话了', 'Ready to talk'], ['你可以分别选择日常交流与后台任务的模型。', 'Choose separate models for conversation and background work.']), footer: B('完成', 'Done', 'ai-finish', 'primary') };
  }
  function telegramModal(f) {
    const c = d().channels.telegram, v = f.v;
    const title = 'Telegram';
    if (f.stage === 'connected' || f.stage === 'paused') return { title, subtitle: T('与你的桌面助手保持同一个对话。', 'The same conversation as your desktop companion.'), body: '<div class="s-channel-identity"><span class="service-icon">' + icon('send', 28) + '</span><h3>@' + h(c.bot) + '</h3>' + statusBadge(f.stage) + '<p>' + T('已配对账号：', 'Paired account: ') + h(c.owner) + '</p></div>' + note(f.stage === 'paused' ? '消息接收已暂停，保存的账号与令牌仍保留。' : '保持应用运行、电脑联网。关闭聊天窗口不影响连接。', f.stage === 'paused' ? 'Messaging is paused. The saved account and token are retained.' : 'Keep the app running and this computer online. Closing chat does not disconnect this channel.') + '<div class="actions">' + B(f.stage === 'paused' ? '恢复连接' : '在 Telegram 打开', f.stage === 'paused' ? 'Resume connection' : 'Open in Telegram', f.stage === 'paused' ? 'tg-resume' : 'tg-open', 'secondary', f.stage === 'paused' ? 'refresh' : 'open') + (f.stage !== 'paused' ? B('暂停', 'Pause', 'tg-pause', 'ghost') : '') + '</div><div class="s-section-end">' + B('移除连接…', 'Remove connection…', 'tg-remove', 'ghost') + '</div>', footer: B('完成', 'Done', 'cancel', 'primary') };
    if (f.stage === 'token') return { title: T('连接 Telegram', 'Connect Telegram'), body: steps([['获取令牌', 'Token'], ['配对', 'Pair'], ['确认账号', 'Confirm']], 0) + '<div class="tabs s-auth-tabs"><button data-action="s:tg-route:new" class="' + (v.route !== 'existing' ? 'active' : '') + '">' + T('创建 Bot', 'Create a bot') + '</button><button data-action="s:tg-route:existing" class="' + (v.route === 'existing' ? 'active' : '') + '">' + T('使用已有 Bot', 'Use an existing bot') + '</button></div><p class="s-body-copy">' + (v.route === 'existing' ? T('在 BotFather 中发送 /mybots，打开机器人的 API Token。切换前请先停止旧应用中的连接。', 'Send /mybots to BotFather and open your bot’s API token. Stop the old app’s connection before switching.') : T('在 BotFather 中发送 /newbot，设置名称与以 bot 结尾的用户名，然后复制 API Token。', 'Send /newbot to BotFather, choose a name and a username ending in bot, then copy the API token.')) + '</p>' + B('打开 BotFather', 'Open BotFather', 'tg-botfather', 'ghost', 'open') + field('Bot Token', input('flow:token', v.token || '', T('粘贴 BotFather 提供的令牌', 'Paste the token from BotFather'), 'password', 'autocomplete="off" spellcheck="false"'), T('实际应用仅保存在这台设备上。此原型不会保存令牌。', 'The app stores the token on this device. This prototype does not save it.')) + errorHTML(f.error) + '<div class="s-demo-step"><span>' + T('原型演示', 'Prototype step') + '</span><p>' + T('无需粘贴真实令牌，可直接使用演示数据。', 'No real token is needed. Use demo data to continue.') + '</p>' + B('使用演示令牌', 'Use demo token', 'tg-demo-token', 'ghost') + '</div>', footer: footerCancel() + B('连接', 'Connect', 'tg-connect', 'primary') };
    if (f.stage === 'pairing') return { title: T('打开你的 Bot', 'Open your bot'), body: steps([['获取令牌', 'Token'], ['配对', 'Pair'], ['确认账号', 'Confirm']], 1) + '<div class="s-channel-identity"><span class="service-icon">' + icon('send', 30) + '</span><h3>@caelis_companion_bot</h3><p>' + T('在 Telegram 点击“开始”，然后回来确认账号。', 'Tap “Start” in Telegram, then return to confirm your account.') + '</p>' + badge(T('配对链接 15 分钟内有效', 'Pairing link valid for 15 minutes')) + '</div>' + B('在 Telegram 打开', 'Open in Telegram', 'tg-open', 'secondary', 'open') + demoActions('tg-candidate', 'tg-fail'), footer: B('获取新配对链接', 'Get a new pairing link', 'tg-new-link', 'ghost') + footerCancel() };
    if (f.stage === 'candidate') return { title: T('确认 Telegram 账号', 'Confirm Telegram account'), body: steps([['获取令牌', 'Token'], ['配对', 'Pair'], ['确认账号', 'Confirm']], 2) + '<div class="s-candidate"><span>' + icon('chat', 25) + '</span><strong>@xu_demo</strong><small>' + T('演示账号', 'Demo account') + '</small></div><h3 class="s-centered">' + T('这是你的 Telegram 账号吗？', 'Is this your Telegram account?') + '</h3><p class="muted s-centered">' + T('只有此账号的私人聊天，才能向助手交代工作。', 'Only private chats from this account can ask your companion to do work.') + '</p>', footer: footerCancel('cancel', ['稍后确认', 'Confirm later']) + B('是，连接我的账号', 'Yes, connect my account', 'tg-confirm', 'primary') };
    return { title: T('Telegram 需要处理', 'Telegram needs attention'), body: errorHTML(f.error || ['连接未完成，请检查令牌或网络后重试。', 'The connection did not complete. Check the token or network and try again.']) + '<p class="small muted">' + T('返回输入令牌时会清除上一份临时内容。', 'Returning to token entry clears the previous temporary input.') + '</p>' + demoActions('tg-candidate', '', '可以演示恢复连接后的配对确认。', 'You can demonstrate pairing after recovery.'), footer: B('重新输入令牌', 'Enter token again', 'tg-reset', 'ghost') + B('重新连接', 'Reconnect', 'tg-retry', 'primary', 'refresh') };
  }
  function weixinModal(f) {
    const c = d().channels.weixin;
    if (f.stage === 'connected' || f.stage === 'paused') return { title: T('微信', 'WeChat'), subtitle: T('预览功能 · 私聊文本', 'Preview · Private text chat'), body: '<div class="s-channel-identity"><span class="service-icon">' + icon('wechat', 30) + '</span><h3>' + T('已配对账号：', 'Paired account: ') + h(c.owner) + '</h3>' + statusBadge(f.stage) + '<p>' + T('向已连接的 ClawBot 发送私聊文本，与同一个助手交流。', 'Send private text messages to your connected ClawBot to talk with the same companion.') + '</p></div>' + note('保持应用运行和这台 Mac 在线。该预览不支持群聊和附件。', 'Keep the app running and this Mac online. This preview does not support groups or attachments.') + '<div class="actions">' + B(f.stage === 'paused' ? '恢复' : '暂停', f.stage === 'paused' ? 'Resume' : 'Pause', f.stage === 'paused' ? 'wx-resume' : 'wx-pause') + B('移除连接…', 'Remove connection…', 'wx-remove', 'ghost') + '</div>', footer: B('完成', 'Done', 'cancel', 'primary') };
    if (f.stage === 'qr' || f.stage === 'verify') return { title: T('用微信扫码', 'Scan with WeChat'), subtitle: T('预览功能 · 连接一位用户的私聊文本', 'Preview · Connect private text chat for one account'), body: qr() + '<p class="s-centered s-body-copy">' + T('用手机微信扫码，并按手机上的提示完成确认。二维码约五分钟后过期。', 'Scan with WeChat on your phone and follow its confirmation steps. The code expires in about five minutes.') + '</p>' + (f.stage === 'verify' ? field(T('手机上显示的验证码', 'Verification code shown on your phone'), input('flow:code', f.v.code || '', '123456', 'text', 'inputmode="numeric" autocomplete="off" maxlength="32"')) + errorHTML(f.error) : '') + demoActions(f.stage === 'verify' ? '' : 'wx-candidate', 'wx-expired', '此二维码不可扫描。使用下方按钮演示手机上的步骤。', 'This code cannot be scanned. Use the controls below to demonstrate the phone step.') + (f.stage !== 'verify' ? B('演示需要验证码', 'Show verification-code step', 'wx-verify-stage', 'ghost') : ''), footer: footerCancel() + (f.stage === 'verify' ? B('提交验证码', 'Submit code', 'wx-submit-code', 'primary') : B('获取新配对码', 'Get a new code', 'wx-new-qr', 'secondary', 'refresh')) };
    if (f.stage === 'candidate') return { title: T('确认微信账号', 'Confirm WeChat account'), subtitle: T('预览功能 · 私聊文本', 'Preview · Private text chat'), body: '<div class="s-candidate"><span>' + icon('wechat', 27) + '</span><strong>Xu</strong><small>' + T('演示账号', 'Demo account') + '</small></div><h3 class="s-centered">' + T('这是你的微信账号吗？', 'Is this your WeChat account?') + '</h3><p class="s-centered muted">' + T('只有此账号的私聊文本会进入 Caelis Bot。', 'Only this account’s private text messages will reach Caelis Bot.') + '</p>', footer: footerCancel('cancel', ['稍后确认', 'Confirm later']) + B('连接我的账号', 'Connect my account', 'wx-confirm', 'primary') };
    return { title: T('配对需要重新开始', 'Start pairing again'), body: '<div class="s-result-block s-result-warning"><div class="s-result-icon">' + icon('clock', 26) + '</div><h3>' + T('二维码已过期', 'The pairing code expired') + '</h3><p>' + T('获取新配对码，并按手机提示继续。', 'Get a new code and follow the steps on your phone.') + '</p></div>', footer: footerCancel() + B('获取新配对码', 'Get a new code', 'wx-new-qr', 'primary', 'refresh') };
  }
  function pluginModal(f) {
    const p = pluginCatalog.find(x => x.id === f.v.id), q = d().plugins[p.id];
    const contributions = p.skills.concat(p.tools);
    return { title: pName(p), subtitle: T(p.zh, p.en), width: 'large', body: '<div class="s-plugin-detail-head"><span class="service-icon">' + icon(p.icon, 30) + '</span><div><strong>' + h(val(p.publisher)) + '</strong><p>' + h(p.version) + ' · ' + T('内置插件目录', 'Bundled catalog') + '</p></div>' + badge(!q.installed ? T('可安装', 'Available') : q.connected || !p.auth ? T('已安装', 'Installed') : T('待连接', 'Needs connection'), q.installed && !q.connected && p.auth ? 'warning' : 'neutral') + '</div>' + (q.installed && p.auth ? section(['连接', 'Connection'], row(q.connected ? ['账号已连接', 'Account connected'] : ['连接你的账号', 'Connect your account'], q.connected ? (p.auth === 'github' ? ['可访问范围由 GitHub 中的授权决定。', 'Repository access follows your GitHub authorization.'] : ['凭据已保存在本机示例状态中。', 'The connection is represented by local demo state.']) : ['完成授权后，助手就能使用这项服务。', 'Authorize the service so your companion can use it.'], B(q.connected ? '管理' : '连接', q.connected ? 'Manage' : 'Connect', 'plugin-connect:' + p.id, q.connected ? 'secondary' : 'primary'))) : !p.auth ? note('此插件提供使用指导，不需要额外的在线账号授权。', 'This plugin provides usage guidance and needs no extra online account authorization.') : '') + section(['包含的能力', 'Included capabilities'], '<div class="s-capability-list">' + contributions.map((x, i) => '<button data-action="s:plugin-capability:' + p.id + ':' + i + '"><span>' + icon(i < p.skills.length ? 'file' : 'tools', 17) + val(x) + '</span>' + icon('chevron', 14) + '</button>').join('') + '</div>') + (q.installed ? '<div class="s-section-end">' + B('卸载插件…', 'Uninstall plugin…', 'plugin-uninstall:' + p.id, 'ghost') + '</div>' : ''), footer: footerCancel('cancel', ['关闭', 'Close']) + (!q.installed ? B('安装插件', 'Install plugin', 'plugin-install:' + p.id, 'primary', 'plus') : B('完成', 'Done', 'cancel', 'primary')) };
  }
  function pluginAuthModal(f) {
    const p = pluginCatalog.find(x => x.id === f.v.id), q = d().plugins[p.id], github = p.auth === 'github';
    if (f.stage === 'connected') return { title: pName(p) + ' · ' + T('连接管理', 'Connection'), body: doneMark([github ? 'GitHub 已授权' : '账号已连接', github ? 'GitHub authorized' : 'Account connected'], [github ? '可访问范围由 GitHub 中为 Caelis Bot 安装的仓库决定。' : '这项服务可以用于后续工作。', github ? 'Access follows the repositories selected for Caelis Bot on GitHub.' : 'This service is available for future work.']) + (github ? B('管理授权仓库', 'Manage repository access', 'gh-repositories', 'secondary', 'open') : '') + row(['连接方式', 'Connection method'], '', badge(q.mode === 'token' ? 'Token' : 'OAuth')) + '<div class="actions">' + B('重新授权', 'Authorize again', 'plugin-auth-retry', 'secondary', 'refresh') + B('移除连接…', 'Remove connection…', 'plugin-disconnect:' + p.id, 'ghost') + '</div>', footer: B('完成', 'Done', 'cancel', 'primary') };
    if (f.stage === 'waiting') return { title: github ? T('等待 GitHub 授权', 'Waiting for GitHub authorization') : T('等待浏览器授权', 'Waiting for browser authorization'), body: '<div class="s-waiting-state"><div class="s-spinner"></div><h3>' + T('在浏览器中完成授权', 'Finish authorization in your browser') + '</h3><p>' + (github ? T('在 github.com/login/device 输入下方代码。', 'Enter the code below at github.com/login/device.') : T('选择工作区，并确认你想授予的访问。', 'Choose a workspace and confirm the access you want to grant.')) + '</p></div>' + (github ? '<div class="s-device-code"><code>DEMO-4826</code>' + B('复制', 'Copy', 'copy-device', 'ghost', 'copy') + '</div>' : '') + demoActions('plugin-auth-success', 'plugin-auth-fail'), footer: footerCancel('cancel', ['取消连接', 'Cancel connection']) };
    if (f.stage === 'failed') return { title: T('授权未完成', 'Authorization incomplete'), body: errorHTML(['浏览器授权未完成。已安装的插件仍可保留。', 'Browser authorization did not finish. Your installed plugin is retained.']) + '<p class="s-body-copy">' + T('你可以重新授权，也可以使用可用的令牌连接。', 'Try authorization again, or use an available token connection.') + '</p>', footer: B('返回', 'Back', 'plugin-auth-back', 'ghost') + (github || p.auth === 'key' || p.auth === 'token' ? B('使用令牌', 'Use a token', 'plugin-auth-token', 'secondary') : '') + B('重新授权', 'Try again', 'plugin-auth-retry', 'primary') };
    if (f.stage === 'token' || p.auth === 'key' || p.auth === 'token') return { title: T('连接 ', 'Connect ') + pName(p), subtitle: T('凭据只用于这个本地演示，不会发送或保存。', 'Credentials stay in this local demonstration and are never sent or saved.'), body: field(p.auth === 'key' ? 'API Key' : T('访问令牌', 'Access token'), input('flow:token', f.v.token || '', p.auth === 'key' ? 'sk-…' : '••••••••', 'password', 'autocomplete="off" spellcheck="false"')) + errorHTML(f.error) + '<div class="s-demo-step"><span>' + T('原型演示', 'Prototype step') + '</span>' + B('填入演示凭据', 'Use demo credentials', 'plugin-demo-token', 'ghost') + '</div>', footer: footerCancel() + B('保存连接', 'Save connection', 'plugin-token-save', 'primary') };
    return { title: T('连接 ', 'Connect ') + pName(p), body: github ? '<ol class="s-auth-checklist"><li><span>1</span><div><h3>' + T('选择授权仓库', 'Choose repositories') + '</h3><p>' + T('在 GitHub 为所选仓库安装 Caelis Bot。', 'Install Caelis Bot for the repositories you choose on GitHub.') + '</p>' + B('在 GitHub 中选择仓库', 'Choose repositories on GitHub', 'gh-repositories', 'secondary', 'open') + '</div></li><li><span>2</span><div><h3>' + T('完成浏览器授权', 'Authorize in the browser') + '</h3><p>' + T('返回这里，用本次设备码完成连接。', 'Return here and connect using this device authorization code.') + '</p></div></li></ol>' + '<div class="s-section-end">' + B('改用个人访问令牌', 'Use a personal access token', 'plugin-auth-token', 'ghost') + '</div>' : '<p class="s-body-copy">' + T('在浏览器中选择工作区，并同意授权。', 'Choose your workspace and authorize access in the browser.') + '</p>' + note('实际可用的 OAuth 方式由当前工具与服务决定。此处使用本地演示流程。', 'Available OAuth methods depend on the current tool and service. This is a local demonstration.'), footer: footerCancel() + B('在浏览器中连接', 'Connect in browser', 'plugin-auth-start', 'primary', 'open') };
  }
  function machineModal(f) {
    const v = f.v, saved = !!v.id;
    const source = v.source || 'existing';
    if (f.stage === 'form') return { title: saved ? T('编辑机器连接', 'Edit machine connection') : T('添加机器', 'Add machine'), subtitle: T('连接可通过 SSH 访问的 Linux 机器。', 'Connect a Linux machine that you can reach over SSH.'), width: 'medium', body: steps([['连接机器', 'Connect'], ['工作设置', 'Work setup']], 0) + '<div class="tabs s-auth-tabs"><button data-action="s:ssh-source:existing" class="' + (source === 'existing' ? 'active' : '') + '">' + T('已有 SSH 配置', 'Existing SSH config') + '</button><button data-action="s:ssh-source:new" class="' + (source === 'new' ? 'active' : '') + '">' + T('新建连接', 'New connection') + '</button></div>' + (source === 'existing' ? '<div class="s-ssh-hosts">' + [['studio-linux', '192.168.1.20'], ['lab-ubuntu', '192.168.1.34']].map(x => '<button class="' + (v.host === x[0] ? 'selected' : '') + '" data-action="s:ssh-host:' + x[0] + '"><span>' + icon('terminal', 18) + '<span><strong>' + x[0] + '</strong><small>' + x[1] + '</small></span></span>' + (v.host === x[0] ? icon('check', 17) : '') + '</button>').join('') + '</div><p class="small muted">' + T('以上为示例连接。实际应用复用本机 SSH 的用户名、端口和密钥。', 'These are demo connections. The app reuses the user, port and keys from your SSH config.') + '</p>' : field(T('主机地址', 'Host address'), input('flow:address', v.address || '', '192.168.1.20')) + '<div class="form-grid">' + field(T('用户名', 'User'), input('flow:user', v.user || '', 'xu')) + field(T('端口', 'Port'), input('flow:port', v.port ?? '22', '22', 'number', 'min="1" max="65535"')) + '</div>' + field(T('认证方式', 'Authentication'), select('flow:auth', v.auth || 'agent', [['agent', 'SSH Agent'], ['key', ['私钥', 'Private key']], ['password', ['密码', 'Password']]])) + (v.auth === 'key' ? field(T('私钥文件', 'Private key file'), input('flow:privateKey', v.privateKey || '', '~/.ssh/id_ed25519')) : '') + (v.auth !== 'agent' ? field(v.auth === 'key' ? T('私钥口令（可选）', 'Key passphrase (optional)') : T('密码', 'Password'), input('flow:secret', '', '', 'password', 'autocomplete="off"')) : '')) + '<div class="s-modal-section"><div class="section-heading"><h3>' + T('可选连接设置', 'Optional connection settings') + '</h3></div>' + field(T('机器名称', 'Machine name'), input('flow:name', v.name || '', 'Studio Linux')) + row(['保存到 macOS 钥匙串', 'Save to macOS Keychain'], ['原型中不保存任何凭据。', 'This prototype does not store credentials.'], toggle('s:flow:remember', !!v.remember, T('保存到钥匙串', 'Save to Keychain'))) + '</div>' + errorHTML(f.error), footer: footerCancel() + B('连接', 'Connect', 'ssh-connect', 'primary') };
    if (f.stage === 'checking') return { title: T('正在连接机器', 'Connecting to machine'), subtitle: v.address || v.host, body: '<div class="s-waiting-state"><div class="s-spinner"></div><h3>' + T('核对 SSH 连接与主机身份', 'Checking SSH and host identity') + '</h3><p>' + T('此步骤不会启动任何工作任务。', 'This step does not start a work task.') + '</p></div>' + demoActions('ssh-trust', 'ssh-fail'), footer: footerCancel() };
    if (f.stage === 'trust') return { title: T('确认机器身份', 'Confirm machine identity'), subtitle: v.address || v.host, body: '<p class="s-body-copy">' + T('首次连接前，请核对这台机器的主机密钥指纹。', 'Before the first connection, check this machine’s host-key fingerprint.') + '</p><div class="s-fingerprint"><span>' + T('示例主机指纹', 'Demo host fingerprint') + '</span><code>SHA256:m8DYVJlFxXT67tcC8DVNb82wAA1BSLTW9uZzBb21RTo</code></div>' + '<p class="small muted">' + T('实际主机密钥变化时，原连接会保持不变并要求重新核对。', 'If an existing host key changes, the original connection is retained and must be checked.') + '</p>', footer: B('返回', 'Back', 'ssh-back-form', 'ghost') + B('信任并连接', 'Trust & connect', 'ssh-accept-trust', 'primary') };
    if (f.stage === 'work') return { title: T('完成工作设置', 'Finish work setup'), subtitle: v.name || v.host || v.address, body: steps([['连接机器', 'Connect'], ['工作设置', 'Work setup']], 1) + '<div class="s-status-line">' + badge(T('SSH 已连接', 'SSH connected'), 'success') + badge(v.loginDone ? T('账号待核对', 'Account to check') : T('账号待登录', 'Sign-in required'), 'warning') + '</div>' + field(T('新任务工作工具', 'Tool for new work'), select('flow:runtime', v.runtime || 'codex', [['codex', 'Codex'], ['caelis', 'Caelis']])) + '<div class="s-guide-block"><h3>' + T('在这台机器上登录', 'Sign in on this machine') + '</h3><p>' + T('在远端终端中完成工具安装或账号登录，再返回这里重新检查。', 'Install the tool or sign in from the remote terminal, then return here and recheck.') + '</p>' + B('打开远端 TUI', 'Open remote TUI', 'ssh-terminal', 'secondary', 'terminal') + '</div>' + note('此机器独立使用自己的账号，凭据不会从本机复制。', 'This machine uses its own account. Credentials are not copied from your Mac.') + errorHTML(f.error), footer: B('稍后设置', 'Set up later', 'ssh-save-later', 'ghost') + B('重新检查', 'Recheck', 'ssh-recheck', 'primary', 'refresh') };
    if (f.stage === 'terminal') return { title: T('远端登录步骤', 'Remote sign-in step'), subtitle: (v.user || 'xu') + '@' + (v.address || v.host), body: '<div class="s-terminal-demo"><span>' + T('远端终端 · 演示', 'Remote terminal · Demo') + '</span><code>' + h(v.runtime === 'caelis' ? 'caelis' : 'codex login --device-auth') + '</code><p>' + T('按终端提示安装或登录。完成后返回 Caelis Bot。', 'Follow the terminal to install or sign in, then return to Caelis Bot.') + '</p></div>' + note('此 HTML 不会打开终端、执行命令或登录远端账号。', 'This HTML does not open a terminal, execute commands or sign in to a remote account.') + demoActions('ssh-login-success', 'ssh-login-fail'), footer: B('返回工作设置', 'Back to work setup', 'ssh-back-work', 'secondary', 'back') };
    if (f.stage === 'ready') return { title: v.name || v.host || T('远程机器', 'Remote machine'), subtitle: T('在聊天中指定这台机器处理工作。', 'Name this machine in chat when you want it to do work.'), body: '<div class="s-status-line">' + statusBadge('ready') + '<span class="small muted">Linux · ' + h(v.address) + '</span></div>' + row(['新任务工作工具', 'Tool for new tasks'], ['不改变已经开始的任务。', 'Existing tasks keep their original tool.'], select('flow:runtime', v.runtime || 'codex', [['codex', 'Codex'], ['caelis', 'Caelis']])) + row(['模型账号', 'Model account'], ['已在这台机器上连接。', 'Connected on this machine.'], badge(T('已连接', 'Connected'), 'success')) + row(['默认工作模型', 'Default work model'], '', '<button class="s-value-button" data-action="s:ssh-model"><span>' + h(modelName(v.model || 'default')) + '</span>' + icon('chevron', 15) + '</button>') + note('这项设置只作用于此机器，账号与凭据留在远端。', 'These settings apply to this machine only. Its account and credentials stay remote.') + '<div class="s-modal-section"><div class="section-heading"><h3>' + T('SSH 连接信息', 'SSH connection details') + '</h3></div><dl class="s-facts"><div><dt>' + T('主机', 'Host') + '</dt><dd>' + h(v.address) + '</dd></div><div><dt>' + T('用户名', 'User') + '</dt><dd>' + h(v.user) + '</dd></div><div><dt>' + T('端口', 'Port') + '</dt><dd>' + h(v.port || '22') + '</dd></div></dl><div class="actions">' + B('编辑连接', 'Edit connection', 'ssh-back-form', 'secondary') + B('打开远端终端', 'Open remote terminal', 'ssh-terminal', 'ghost', 'terminal') + '</div></div>' + (v.runtime === 'caelis' ? '<div class="s-modal-section"><div class="section-heading"><h3>' + T('高级配置（可选）', 'Advanced configuration (optional)') + '</h3></div><p class="muted small">' + T('自定义 Team 是可选增强。OAuth、ACP 和其他高级设置可前往这台机器的 Caelis TUI。', 'A custom team is optional. Use this machine’s Caelis TUI for OAuth, ACP and other advanced settings.') + '</p>' + B('打开远端 TUI', 'Open remote TUI', 'ssh-terminal', 'secondary', 'terminal') + '</div>' : '') + (saved ? '<div class="s-section-end">' + B('删除机器…', 'Remove machine…', 'ssh-remove', 'ghost') + '</div>' : ''), footer: B('完成', 'Done', 'ssh-save', 'primary') };
    if (f.stage === 'model') return { title: T('此机器的工作模型', 'Work model on this machine'), subtitle: v.name || v.host, body: field(T('默认工作模型', 'Default work model'), select('flow:model', v.model || 'default', models.filter(x => v.runtime === 'caelis' || x.id !== 'mimo-v2.6-flash').map(x => [x.id, x.name]))) + note('模型来自这台机器的连接，保持与本机设置独立。', 'Models come from this machine’s connection and stay independent of your Mac.'), footer: B('返回', 'Back', 'ssh-ready', 'ghost') + B('保存', 'Save', 'ssh-model-save', 'primary') };
    return { title: T('连接不可用', 'Connection unavailable'), subtitle: v.address || v.host, body: errorHTML(f.error || ['无法访问这台机器，请检查地址、认证信息与网络。', 'This machine could not be reached. Check its address, authentication and network.']) + '<p class="small muted">' + T('原连接信息与草稿仍然保留。不会自动重新创建任务。', 'Your connection details and draft are retained. No task is recreated automatically.') + '</p>' + demoActions('ssh-trust', '', '在原型中可以继续演示身份核对。', 'Continue to host-identity verification in the prototype.'), footer: footerCancel() + B('返回连接设置', 'Back to connection setup', 'ssh-back-form', 'primary') };
  }
  function permissionModal(f) {
    const p = permissionInfo[f.v.id], allowed = d().permissions[f.v.id];
    return { title: T(p[0], p[1]), subtitle: T(p[2], p[3]), body: '<div class="s-permission-symbol">' + icon(p[4], 34) + '</div>' + row(['当前状态', 'Current status'], '', badge(allowed ? T('已允许', 'Allowed') : T('未允许', 'Not allowed'), allowed ? 'success' : 'neutral')) + '<p class="s-body-copy">' + T('这项权限由 macOS 管理。请在系统设置中找到 Caelis Bot，按需要更改授权。', 'macOS manages this permission. Find Caelis Bot in System Settings and change access as needed.') + '</p>' + (f.stage === 'opened' ? note('已展示系统设置指引。当前网页不会更改你的系统权限。', 'System Settings guidance is shown. This page does not change your system permissions.') : '') + demoActions('permission-allow', 'permission-deny', '用示例状态查看允许或拒绝后的界面。', 'Preview how the interface responds to allowed or denied access.'), footer: footerCancel('cancel', ['完成', 'Done']) + B('打开系统设置', 'Open System Settings', 'permission-open', 'primary', 'open') };
  }
  function shortcutModal(f) {
    const names = { chat: ['唤出聊天', 'Show chat'], task: ['展开任务预览', 'Show task previews'], capture: ['截图', 'Capture'], paste: ['桌面贴图', 'Pin image'] };
    return { title: T('快捷键：', 'Shortcut: ') + val(names[f.v.kind]), body: row(['启用快捷键', 'Enable shortcut'], '', toggle('s:flow:enabled', f.v.enabled !== false, T('启用快捷键', 'Enable shortcut'))) + field(T('快捷键组合', 'Key combination'), input('flow:shortcut', f.v.shortcut || '', f.v.kind === 'chat' ? 'Ctrl + Shift + Space' : f.v.kind === 'task' ? 'Ctrl + Shift + T' : f.v.kind === 'capture' ? 'F1' : 'F3'), T('可直接输入组合键名称。原型不会注册全局快捷键。', 'Enter a key combination. This prototype does not register global shortcuts.')) + errorHTML(f.error), footer: B('恢复默认', 'Restore default', 'shortcut-default', 'ghost', 'refresh') + footerCancel() + B('保存', 'Save', 'shortcut-save', 'primary'), width: 'small' };
  }

  function info(title, body) { flow('info', 'start', { title, body }); }
  function storeMachine(status) {
    const f = ui.flow, s = d(), v = Object.assign({}, f.v);
    ['secret', 'password', 'remember', 'loginDone'].forEach(k => delete v[k]);
    v.id = v.id || ('machine-' + Date.now()); v.name = v.name || (v.source === 'existing' ? v.host : v.address); v.runtime = v.runtime || 'codex'; v.model = v.model || 'default'; v.status = status;
    const i = s.machines.findIndex(m => m.id === v.id); if (i >= 0) s.machines[i] = v; else s.machines.push(v);
    save(); endFlow(); render(); toast(status === 'ready' ? '机器已就绪，可在聊天中指定它工作。' : '连接已保存，可稍后完成工作设置。', status === 'ready' ? 'Machine ready. Name it in chat to use it.' : 'Connection saved. Finish work setup later.');
  }
  function confirmAction(title, body, confirmLabel, fn, danger = true) { confirmDialog({ title: val(title), body: val(body), confirmLabel: val(confirmLabel), danger, onConfirm: fn }); }
  function handleAction(action, el, event) {
    if (!String(action).startsWith('s:')) return false;
    const a = String(action).slice(2).split(':'), key = a[0], s = d(), f = ui.flow;
    if (key === 'cancel') { endFlow(); return true; }
    if (key === 'goto') { endFlow(); navigate('settings', a[1]); return true; }
    if (key === 'terminal-edit') { flow('terminal','start',{command:s.customTerminal || ''}); return true; }
    if (key === 'terminal-save') { s.customTerminal=String(f.v.command || '').trim(); save(); endFlow(); render(); toast('终端配置已保存到原型。','Terminal configuration saved in the prototype.'); return true; }
    if (key === 'theme') { state.theme = a[1]; commit(); return true; }
    if (key === 'storage-clean') { confirmAction(['清理旧附件副本？', 'Clear older attachment copies?'], ['30 天前的副本将移到废纸篓。原始文件和聊天记录会保留。这里仅更改原型中的示例状态。', 'Copies older than 30 days move to Trash. Originals and chat history remain. This only changes demo state.'], ['移到废纸篓', 'Move to Trash'], () => { s.storageCleaned = true; closeModal(); commit(); toast('旧附件副本已清理。', 'Older attachment copies cleared.'); }); return true; }
    if (key === 'pack-import') { const e = document.getElementById('s-pack-file'); if (e) e.click(); return true; }
    if (key === 'pack-remove') { const i = Number(a[1]); confirmAction(['移除形象包？', 'Remove this character pack?'], ['仅从此原型移除这个导入记录。助手身份和对话保留。', 'Remove this import record from the prototype. Identity and conversation stay intact.'], ['移除', 'Remove'], () => { s.packs.splice(i, 1); closeModal(); commit(); }); return true; }
    if (key === 'shortcut') { const name = a[1], prefix = name === 'task' ? 'task' : name === 'capture' ? 'capture' : name === 'paste' ? 'paste' : 'chat'; flow('shortcut', 'start', { kind: name, shortcut: s[prefix + 'Shortcut'], enabled: s[prefix + 'ShortcutEnabled'] !== false }); return true; }
    if (key === 'shortcut-default') { f.v.shortcut = ({ chat: 'Ctrl + Shift + Space', task: 'Ctrl + Shift + T', capture: 'F1', paste: 'F3' })[f.v.kind]; render(); return true; }
    if (key === 'shortcut-save') { if (!String(f.v.shortcut).trim()) { f.error = ['请输入快捷键组合。', 'Enter a key combination.']; render(); return true; } s[f.v.kind + 'Shortcut'] = String(f.v.shortcut).trim(); s[f.v.kind + 'ShortcutEnabled'] = f.v.enabled !== false; save(); endFlow(); render(); toast('快捷键已保存到原型。', 'Shortcut saved in the prototype.'); return true; }
    if (key === 'model') { ui.modelQuery = ''; ui.modelCategory = 'all'; const kind = a[1]; flow('model', 'start', { kind, model: kind === 'conversation' ? s.ai.conversationModel : s.ai.workModel, effort: s.ai.effort || 'high' }); return true; }
    if (key === 'model-tab') { ui.modelCategory = a[1]; render(); return true; }
    if (key === 'model-effort') { f.v.effort = a[1]; render(); return true; }
    if (key === 'model-choose') { f.v.model = a[1]; render(); return true; }
    if (key === 'model-default') { f.v.model = 'default'; f.v.effort = 'high'; render(); return true; }
    if (key === 'model-save') { s.ai[f.v.kind === 'conversation' ? 'conversationModel' : 'workModel'] = f.v.model; s.ai.effort = f.v.effort; save(); endFlow(); render(); toast('模型偏好已保存。', 'Model preference saved.'); return true; }
    if (key === 'ai-ping') { toast('Codex 本机连接正常 · 延迟 24ms · 沙箱已就绪', 'Codex connection healthy · 24ms · Sandbox ready'); return true; }
    if (key === 'ai-manage') { flow('ai', s.ai.connected ? 'manage' : 'choice', { tool: s.ai.tool, program: s.ai.program || '', store: s.ai.store || '' }); return true; }
    if (key === 'ai-choice') { if (f && f.type === 'ai') setStage('choice'); else flow('ai', 'choice', {}); return true; }
    if (key === 'ai-provider') { flow('ai', 'provider', { tool: 'caelis', authKind: 'key', provider: 'openai', baseUrl: '', modelName: '', key: '', adding: true }); return true; }
    if (key === 'ai-pick') { f.v.adding = false; f.v.tool = a[1]; f.v.switching = s.ai.connected && s.ai.tool !== a[1]; f.v.authKind = 'account'; f.v.account = a[1] === 'codex' ? 'ChatGPT' : 'AI service'; setStage(a[1] === 'codex' ? 'waiting' : 'provider'); return true; }
    if (key === 'ai-login') { f.v.tool = s.ai.tool; f.v.account = s.ai.account; setStage('waiting'); return true; }
    if (key === 'ai-auth-kind') { f.v.authKind = a[1]; f.v.key = ''; setStage('provider'); return true; }
    if (key === 'ai-key') { f.v.key = ''; f.v.authKind = 'key'; setStage('codex-key'); return true; }
    if (key === 'ai-submit-key' || key === 'ai-submit') {
      const v = f.v;
      if (key === 'ai-submit-key' ? !String(v.key || '').trim() : v.authKind === 'key' && (!String(v.key || '').trim() || !String(v.baseUrl || '').startsWith('https://') || !String(v.modelName || '').trim())) { f.error = ['请填写完整的 HTTPS 服务地址、模型与密钥；此处可使用演示数据。', 'Enter the HTTPS service URL, model and key. Demo data is fine here.']; if (key === 'ai-submit-key') f.error = ['请输入 API Key，或返回账号登录。', 'Enter an API key or return to account login.']; render(); return true; }
      if (v.authKind === 'agent' && !String(v.command || '').trim()) { f.error = ['请输入外部 Agent 的启动命令。', 'Enter the external agent launch command.']; render(); return true; }
      v.key = ''; v.account = v.authKind === 'agent' ? T('外部 Agent', 'External agent') : key === 'ai-submit-key' ? 'OpenAI API Key' : v.authKind === 'key' ? (v.provider || 'Model service') + ' · API Key' : v.provider || 'ChatGPT'; setStage(v.authKind === 'account' ? 'waiting' : 'checking'); return true;
    }
    if (key === 'ai-browser') { toast('原型不会打开真实登录页面，请用下方演示按钮继续。', 'No real login page is opened. Use the prototype controls below.'); return true; }
    if (key === 'ai-check-wait') { toast('仍在等待授权确认。', 'Still waiting for authorization confirmation.'); return true; }
    if (key === 'ai-recheck') { flow('ai', 'checking', { tool: s.ai.tool, account: s.ai.account }); return true; }
    if (key === 'ai-detect') { toast('已检测到本机工具 · 演示状态。', 'Local tool detected · Demo state.'); return true; }
    if (key === 'ai-retry') { setStage('checking'); return true; }
    if (key === 'ai-fail') { setStage('failed'); return true; }
    if (key === 'ai-success') { setStage('done'); return true; }
    if (key === 'ai-finish') { const apply = () => { if (f.v.adding) s.ai.providerCount += 1; else { s.ai.tool = f.v.tool || s.ai.tool; s.ai.account = f.v.account || 'ChatGPT'; } s.ai.connected = true; save(); endFlow(); render(); toast('AI 连接已更新（演示）。', 'AI connection updated (demo).'); }; if (f.v.switching) confirmAction(['切换 AI 连接？', 'Change AI connection?'], ['实际应用会重新启动，原有对话和配置分别保留。此原型只切换示例状态。', 'The app would restart and retain each tool’s conversation and configuration. This prototype only updates demo state.'], ['切换并重新启动', 'Switch & restart'], apply, false); else apply(); return true; }
    if (key === 'ai-logout') { confirmAction(['退出当前账号？', 'Sign out of this account?'], ['后续对话需要重新连接。原有历史和草稿保留。', 'You will need to reconnect for future conversations. History and drafts remain.'], ['退出账号', 'Sign out'], () => { s.ai.connected = false; save(); endFlow(); render(); }); return true; }
    if (key === 'ai-program') { flow('ai', 'program', { program: s.ai.program || '', store: s.ai.store || '' }); return true; }
    if (key === 'ai-manage-save') { s.ai.program = String(f.v.program || '').trim(); s.ai.store = String(f.v.store || '').trim(); save(); endFlow(); render(); toast('连接偏好已保存。', 'Connection preferences saved.'); return true; }
    if (key === 'ai-team') { info(['协作团队（可选）', 'Team configuration (optional)'], () => '<p class="s-body-copy">' + T('自定义 Team 可为不同角色分配模型。未配置 Team 也可以正常完成基础工作。', 'A custom team can assign models to different roles. Basic work remains available without one.') + '</p>' + row(['默认工作模型', 'Default work model'], '', modelButton('work', s.ai.workModel)) + note('复杂角色绑定与高级认证可在 Caelis TUI 中管理。', 'Manage advanced role bindings and authentication in the Caelis TUI.')); return true; }
    if (key === 'channel') { if (a[1] === 'telegram') { const c = s.channels.telegram; flow('telegram', ['connected', 'paused'].includes(c.status) ? c.status : 'token', { token: '', route: 'new' }); } else { const c = s.channels.weixin; flow('weixin', ['connected', 'paused'].includes(c.status) ? c.status : 'qr', {}); } return true; }
    if (key === 'tg-route') { f.v.route = a[1]; render(); return true; }
    if (key === 'tg-demo-token') { f.v.token = '123456:DEMO_TOKEN_NOT_VALID'; render(); return true; }
    if (key === 'tg-connect') { if (!String(f.v.token || '').trim()) { f.error = ['请输入令牌，或使用演示令牌。', 'Enter a token or use the demo token.']; render(); } else { f.v.token = ''; setStage('pairing'); } return true; }
    if (key === 'tg-open' || key === 'tg-botfather') { toast(key === 'tg-open' ? '原型不会打开 Telegram，可继续演示配对结果。' : '在实际应用中，这里会打开 BotFather。', key === 'tg-open' ? 'Telegram is not opened. Continue with the demo pairing result.' : 'The actual app opens BotFather here.'); return true; }
    if (key === 'tg-new-link') { setStage('pairing'); toast('已生成新的演示配对链接。', 'A new demo pairing link is ready.'); return true; }
    if (key === 'tg-candidate') { setStage('candidate'); return true; }
    if (key === 'tg-confirm') { s.channels.telegram = { status: 'connected', bot: 'caelis_companion_bot', owner: '@xu_demo' }; save(); setStage('connected'); toast('Telegram 示例账号已连接。', 'Demo Telegram account connected.'); return true; }
    if (key === 'tg-pause' || key === 'tg-resume') { const v = key === 'tg-pause' ? 'paused' : 'connected'; s.channels.telegram.status = v; save(); setStage(v); return true; }
    if (key === 'tg-remove') { confirmAction(['移除 Telegram 连接？', 'Remove Telegram connection?'], ['从本机移除账号与令牌，不会删除 Telegram Bot。', 'Remove the local account and token. The Telegram bot is not deleted.'], ['移除连接', 'Remove connection'], () => { s.channels.telegram.status = 'unconfigured'; save(); flow('telegram', 'token', { token: '', route: 'new' }); render(); }); return true; }
    if (key === 'tg-fail') { f.error = ['连接暂不可用。请检查网络或令牌，原配对流程可以重新核对。', 'Connection unavailable. Check the network or token and reconcile the original pairing.']; f.stage = 'error'; render(); return true; }
    if (key === 'tg-reset') { f.v.token = ''; setStage('token'); return true; }
    if (key === 'tg-retry') { setStage('pairing'); return true; }
    if (key === 'wx-new-qr') { setStage('qr'); toast('演示二维码已更新。', 'Demo pairing code refreshed.'); return true; }
    if (key === 'wx-verify-stage') { setStage('verify'); return true; }
    if (key === 'wx-submit-code') { if (String(f.v.code || '').trim().length < 3) { f.error = ['请输入手机上的验证码，演示可用 123456。', 'Enter the phone code. Use 123456 for this demo.']; render(); } else setStage('candidate'); return true; }
    if (key === 'wx-candidate') { setStage('candidate'); return true; }
    if (key === 'wx-expired') { setStage('expired'); return true; }
    if (key === 'wx-confirm') { s.channels.weixin = { status: 'connected', owner: 'Xu' }; save(); setStage('connected'); return true; }
    if (key === 'wx-pause' || key === 'wx-resume') { const v = key === 'wx-pause' ? 'paused' : 'connected'; s.channels.weixin.status = v; save(); setStage(v); return true; }
    if (key === 'wx-remove') { confirmAction(['移除微信连接？', 'Remove WeChat connection?'], ['删除本地配对信息。若要撤销微信侧授权，请在微信中移除 ClawBot 连接。', 'Delete local pairing information. Remove the ClawBot connection in WeChat to revoke its authorization there.'], ['移除本地连接', 'Remove local connection'], () => { s.channels.weixin.status = 'unconfigured'; save(); flow('weixin', 'qr', {}); render(); }); return true; }
    if (key === 'plugin-tab') { ui.pluginTab = a[1]; render(); return true; }
    if (key === 'plugin-clear') { ui.pluginQuery = ''; render(); return true; }
    if (key === 'plugin') { flow('plugin', 'detail', { id: a[1] }); return true; }
    if (key === 'plugin-install') { const p = pluginCatalog.find(x => x.id === a[1]); const q = s.plugins[p.id]; q.installed = true; if (!p.auth) q.connected = true; save(); flow('plugin', 'detail', { id: p.id }); render(); toast('插件已安装到示例状态。', 'Plugin installed in demo state.'); return true; }
    if (key === 'plugin-uninstall') { const id = a[1], p = pluginCatalog.find(x => x.id === id); confirmAction([`卸载 ${pName(p)}？`, `Uninstall ${pName(p)}?`], ['此插件提供的能力将不再用于后续工作。', 'This plugin’s capabilities will no longer be used for future work.'], ['卸载插件', 'Uninstall'], () => { s.plugins[id] = { installed: false, connected: false }; save(); endFlow(); render(); }); return true; }
    if (key === 'plugin-connect') { const id = a[1], q = s.plugins[id]; flow('plugin-auth', q.connected ? 'connected' : 'start', { id, token: '' }); return true; }
    if (key === 'plugin-capability') { const p = pluginCatalog.find(x => x.id === a[1]); const x = p.skills.concat(p.tools)[Number(a[2])]; info(x, () => '<p class="s-body-copy">' + (Number(a[2]) < p.skills.length ? T('这项使用指导会帮助助手按插件约定完成任务。', 'This guidance helps your companion follow the plugin’s workflow.') : T('这项服务能力由插件提供。真实工具与授权范围以当前连接返回的信息为准。', 'The plugin provides this capability. Real tools and access follow the current connection.')) + '</p>' + note('这里展示能力概览，不伪造实时工具调用或服务返回结果。', 'This is a capability overview, without fabricated live tool calls or service responses.')); return true; }
    if (key === 'plugin-auth-start' || key === 'plugin-auth-retry') { f.v.token = ''; setStage('waiting'); return true; }
    if (key === 'plugin-auth-back') { setStage('start'); return true; }
    if (key === 'plugin-auth-token') { f.v.token = ''; setStage('token'); return true; }
    if (key === 'plugin-demo-token') { f.v.token = 'demo_token_not_valid'; render(); return true; }
    if (key === 'plugin-token-save') { if (!String(f.v.token || '').trim()) { f.error = ['请输入令牌或填入演示凭据。', 'Enter a token or use demo credentials.']; render(); return true; } f.v.token = ''; const q = s.plugins[f.v.id]; q.connected = true; q.mode = 'token'; save(); setStage('connected'); return true; }
    if (key === 'plugin-auth-success') { const q = s.plugins[f.v.id]; q.connected = true; q.mode = 'oauth'; save(); setStage('connected'); return true; }
    if (key === 'plugin-auth-fail') { setStage('failed'); return true; }
    if (key === 'plugin-disconnect') { const id = a[1]; confirmAction(['移除此项连接？', 'Remove this connection?'], ['移除本机保存的连接。若要撤销服务侧授权，请前往对应服务管理。', 'Remove the locally saved connection. Manage authorization in the service itself to revoke it there.'], ['移除连接', 'Remove connection'], () => { s.plugins[id].connected = false; save(); flow('plugin', 'detail', { id }); render(); }); return true; }
    if (key === 'gh-repositories') { toast('实际应用会前往 GitHub 选择仓库；此原型不访问真实账号。', 'The app opens GitHub repository selection. This prototype does not access real accounts.'); return true; }
    if (key === 'copy-device') { copyText('DEMO-4826'); return true; }
    if (key === 'machine-add') { flow('machine', 'form', { source: 'existing', host: 'studio-linux', address: '192.168.1.20', name: '', user: 'xu', port: '22', auth: 'agent', runtime: 'codex', model: 'default' }); return true; }
    if (key === 'machine') { const m = s.machines.find(x => x.id === a[1]); if (m) flow('machine', m.status === 'ready' ? 'ready' : m.status === 'offline' ? 'error' : 'work', Object.assign({ loginDone: m.status === 'ready' }, m)); return true; }
    if (key === 'ssh-source') { f.v.source = a[1]; f.error = ''; render(); return true; }
    if (key === 'ssh-host') { f.v.host = a[1]; f.v.address = a[1] === 'studio-linux' ? '192.168.1.20' : '192.168.1.34'; f.v.user = a[1] === 'studio-linux' ? 'xu' : 'ubuntu'; render(); return true; }
    if (key === 'ssh-connect') { const v = f.v, isNew = v.source === 'new', target = String((isNew ? v.address : v.host) ?? '').trim(), user = String(v.user ?? '').trim(), portText = String(v.port ?? '').trim(), port = Number(portText); if (!target || !user || !portText || !Number.isInteger(port) || port < 1 || port > 65535 || isNew && v.auth === 'key' && !String(v.privateKey || '').trim() || isNew && v.auth === 'password' && !String(v.secret || '').trim()) { f.error = ['请填写有效的主机、用户名、端口和所需的认证信息。', 'Enter a valid host, user, port and required authentication details.']; render(); } else setStage('checking'); return true; }
    if (key === 'ssh-trust') { setStage('trust'); return true; }
    if (key === 'ssh-accept-trust') { f.v.secret = ''; setStage('work'); return true; }
    if (key === 'ssh-back-form') { setStage('form'); return true; }
    if (key === 'ssh-fail') { f.error = ['SSH 未能连接，请检查主机地址、用户名、认证信息与网络。', 'SSH could not connect. Check host, user, authentication and network.']; f.stage = 'error'; render(); return true; }
    if (key === 'ssh-terminal') { setStage('terminal'); return true; }
    if (key === 'ssh-login-success') { f.v.loginDone = true; setStage('work'); toast('演示登录已完成，请重新检查。', 'Demo sign-in completed. Recheck to continue.'); return true; }
    if (key === 'ssh-login-fail') { f.v.loginDone = false; f.error = ['远端账号尚未就绪，请在终端完成登录。', 'The remote account is not ready. Finish sign-in in the terminal.']; f.stage = 'work'; render(); return true; }
    if (key === 'ssh-back-work') { setStage('work'); return true; }
    if (key === 'ssh-recheck') { if (f.v.loginDone) setStage('ready'); else { f.error = ['尚未检测到已登录账号。请先完成远端登录，再重新检查。', 'No signed-in account detected. Finish remote sign-in, then recheck.']; render(); } return true; }
    if (key === 'ssh-save-later') { storeMachine('setup'); return true; }
    if (key === 'ssh-save') { storeMachine('ready'); return true; }
    if (key === 'ssh-model') { setStage('model'); return true; }
    if (key === 'ssh-ready' || key === 'ssh-model-save') { setStage('ready'); if (key === 'ssh-model-save') toast('此机器的模型选择已更新。', 'This machine’s model selection is updated.'); return true; }
    if (key === 'ssh-remove') { const id = f.v.id; confirmAction(['删除这台机器？', 'Remove this machine?'], ['仅移除 Bot 中的连接。本机 SSH 配置、远端文件和工作工具均保留。', 'Remove only the Bot connection. Local SSH configuration, remote files and tools remain.'], ['删除机器', 'Remove machine'], () => { s.machines = s.machines.filter(x => x.id !== id); save(); endFlow(); render(); }); return true; }
    if (key === 'permission') { flow('permission', 'start', { id: a[1] }); return true; }
    if (key === 'permission-open') { setStage('opened'); return true; }
    if (key === 'permission-allow' || key === 'permission-deny') { s.permissions[f.v.id] = key === 'permission-allow'; save(); render(); toast(key === 'permission-allow' ? '演示权限已允许。' : '演示权限已拒绝。', key === 'permission-allow' ? 'Demo permission allowed.' : 'Demo permission denied.'); return true; }
    if (key === 'permissions-recheck') { toast('权限示例状态已重新核对。', 'Demo permission status rechecked.'); return true; }
    if (key === 'permission-reset') { const id = s.repairPermission; confirmAction(['重置此项授权？', 'Reset this permission?'], ['只清除选中的授权示例，之后需要重新允许。其他权限不变。', 'Clear only this permission’s demo state. It will need to be allowed again. Other permissions stay unchanged.'], ['重置授权', 'Reset permission'], () => { s.permissions[id] = false; closeModal(); commit(); }); return true; }
    if (key === 'capture-demo') { if (!s.captureEnabled) return true; if (window.Experience) Experience.handleAction('x:screenshot', el, event); else flow('capture', 'start', {}); return true; }
    if (key === 'capture-send') { endFlow(); navigate('chat'); toast('已打开聊天。此处未采集或发送真实截图。', 'Chat opened. No real screenshot was captured or sent.'); return true; }
    if (key === 'update-check') { s.updateStatus = 'checking'; commit(); setTimeout(() => { s.updateStatus = 'available'; save(); render(); toast('已找到一个演示更新。', 'A demo update is available.'); }, 750); return true; }
    if (key === 'update-download') { s.updateStatus = 'downloaded'; commit(); toast('演示更新已准备好，没有下载任何程序。', 'Demo update ready. No program was downloaded.'); return true; }
    if (key === 'update-install') { confirmAction(['重新启动并更新？', 'Restart and update?'], ['实际应用会在确认后重新启动。此原型只演示完成状态。', 'The app would restart after confirmation. This prototype only demonstrates completion.'], ['重新启动', 'Restart'], () => { s.updateStatus = 'current'; closeModal(); commit(); toast('更新流程演示完成。', 'Update demonstration complete.'); }, false); return true; }
    if (key === 'diagnostics') { const report = { prototype: true, generatedAt: new Date().toISOString(), app: 'Caelis Bot Design Preview', ai: { tool: s.ai.tool, connected: s.ai.connected }, channels: { telegram: s.channels.telegram.status, weixin: s.channels.weixin.status }, machineCount: s.machines.length, permissions: s.permissions }; const blob = new Blob([JSON.stringify(report, null, 2)], { type: 'application/json' }), url = URL.createObjectURL(blob), aTag = document.createElement('a'); aTag.href = url; aTag.download = 'CaelisBot-Diagnostics-Demo.json'; aTag.click(); setTimeout(() => URL.revokeObjectURL(url), 500); toast('已导出演示诊断报告，不包含凭据。', 'Demo diagnostics exported without credentials.'); return true; }
    if (key === 'release-info') { info(['发布与安装', 'Releases & installation'], () => '<div class="s-release-notes"><h3>' + T('Caelis Bot · 设计预览', 'Caelis Bot · Design preview') + '</h3><p>' + T('此原型基于 2026-10-12 的源码与实机资料，重点演示统一设置、聊天与配置流程。', 'This prototype is based on source and real-interface references from October 12, 2026. It demonstrates a unified settings, chat and setup experience.') + '</p><ul><li>' + T('AI 模型与账号整合到一个入口。', 'AI models and accounts share one place.') + '</li><li>' + T('配置流程逐步推进，保留明确的等待与恢复状态。', 'Setup progresses step by step with clear waiting and recovery states.') + '</li><li>' + T('中英文、亮暗主题均可切换。', 'Switch between Chinese and English, light and dark themes.') + '</li></ul></div>'); return true; }
    return false;
  }
  function handleChange(bind, value, el, event) {
    if (!String(bind).startsWith('s:')) return false;
    const a = String(bind).slice(2).split(':'), key = a[0], s = d(), f = ui.flow;
    if (key === 'language') { s.language = value; state.lang = value === 'system' ? (/^zh/i.test(navigator.language || '') ? 'zh' : 'en') : value === 'en' ? 'en' : 'zh'; commit(); return true; }
    if (key === 'pref') { s[a[1]] = value; save(); if (a[1] === 'terminal' && value === 'custom' && !s.customTerminal) { flow('terminal', 'start', { command: s.customTerminal || '' }); return true; } if (el && (el.tagName === 'SELECT' || el.type === 'checkbox' || el.getAttribute('role') === 'switch')) render(); return true; }
    if (key === 'flow') { if (f) { f.v[a[1]] = value; f.error = ''; if (['auth', 'source', 'provider', 'runtime'].includes(a[1])) { if (a[1] === 'runtime' && f.type === 'machine' && ['work', 'ready'].includes(f.stage)) { f.v.loginDone = false; f.stage = 'work'; } render(); } } return true; }
    if (key === 'concurrency') { const n = Number(value); if (!Number.isInteger(n) || n < 1) { if (el) { el.setAttribute('aria-invalid', 'true'); el.title = T('请输入正整数。', 'Enter a positive integer.'); } } else { s.concurrency = n; if (el) el.removeAttribute('aria-invalid'); save(); } return true; }
    if (key === 'petSize') { s.petSize = Math.max(65, Math.min(160, Number(value))); save(); const o = document.querySelector('[data-s-value="petSize"]'); if (o) o.textContent = s.petSize + '%'; const pet = document.querySelector('.s-pet-stage'); if (pet) pet.style.setProperty('--pet-scale', s.petSize / 100); return true; }
    if (key === 'workTool') { s.ai.workTool = value; commit(); toast('新任务工作工具已更新，已有任务不受影响。', 'Tool for new tasks updated. Existing tasks are unchanged.'); return true; }
    if (key === 'pluginQuery') { ui.pluginQuery = String(value); render(); focusAfter('s-plugin-search'); return true; }
    if (key === 'modelQuery') { ui.modelQuery = String(value); render(); focusAfter('s-model-search'); return true; }
    if (key === 'packFile') { const file = el && el.files && el.files[0]; if (file) { if (!file.name.toLowerCase().endsWith('.caelispack')) { toast('请选择 .caelispack 文件。', 'Choose a .caelispack file.', 'danger'); return true; } s.packs.push({ name: file.name.replace(/\.caelispack$/i, '') }); commit(); toast('已添加形象包示例记录，未解析或执行文件。', 'Character pack demo entry added. The file was not parsed or executed.'); } return true; }
    if (key === 'approvalMode') { if (value === 'full-access') { render(); confirmAction(['使用完全访问？', 'Use full access?'], ['实际应用将关闭命令沙箱并不再询问执行权限。此原型仅展示该选项。', 'The app would disable its command sandbox and execution prompts. This prototype only demonstrates the option.'], ['使用完全访问', 'Use full access'], () => { s.approvalMode = value; closeModal(); commit(); }); } else { s.approvalMode = value; commit(); } return true; }
    return false;
  }
  window.Settings = {
    render(page) { d(); const pages = { general: generalPage, appearance: appearancePage, ai: aiPage, channels: channelPage, plugins: pluginsPage, machines: machinesPage, tools: toolsPage, privacy: privacyPage, about: aboutPage }; return '<div class="s-settings-page">' + (pages[page] || aiPage)() + '</div>'; },
    handleAction,
    handleChange,
    clearTransient
  };
})();
