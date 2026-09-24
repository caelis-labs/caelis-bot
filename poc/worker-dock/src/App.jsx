import { useEffect, useRef, useState } from 'react';
import { Pet } from './Pet';

// Display the original task input verbatim. No generated title, category or icon.
const tasks = [
  { id: 'one', prompt: '修复登录后的跳转问题，检查登录成功后仍然停留在登录页面的原因，完成修复并运行相关测试。' },
  { id: 'two', prompt: '帮我整理项目目录中的文档，先看看哪些资料重复、哪些需要归档，给我一个整理方案。移动文件之前先跟我确认。' },
  { id: 'three', prompt: '总结这周 Caelis 的开发进展，列出已经完成的工作、还没有解决的问题，以及下周建议优先处理的事情。' },
];

export function App() {
  const [expanded, setExpanded] = useState(false);
  const [selected, setSelected] = useState(null);
  const [gesture, setGesture] = useState(null);
  const [feedback, setFeedback] = useState('悬停查看原始任务，点击打开终端');
  const zone = useRef(), trigger = useRef();
  const opening = useRef(), closing = useRef();
  const isOpen = useRef(false), lastGesture = useRef(0);
  const task = tasks.find(t => t.id === selected);

  function pose(name) {
    if (performance.now() - lastGesture.current < 1200) return;
    lastGesture.current = performance.now();
    setGesture({ name, at: performance.now() });
  }
  function open(immediate = false) {
    clearTimeout(opening.current); clearTimeout(closing.current);
    const reveal = () => {
      if (!isOpen.current) pose('attention');
      isOpen.current = true; setExpanded(true);
    };
    if (immediate) reveal(); else opening.current = setTimeout(reveal, 180);
  }
  function close() {
    clearTimeout(opening.current); clearTimeout(closing.current);
    isOpen.current = false; setExpanded(false); setSelected(null);
  }
  function leave() {
    clearTimeout(opening.current);
    closing.current = setTimeout(() => {
      if (!zone.current?.contains(document.activeElement)) close();
    }, 400);
  }
  function attach(t) {
    pose('nod');
    // Native OpenTask(handle) will replace this mock; never execute prompt text.
    setFeedback(`演示：打开任务 ${tasks.indexOf(t) + 1} 对应的终端`);
  }
  useEffect(() => () => { clearTimeout(opening.current); clearTimeout(closing.current); }, []);

  return <main>
    <header><span>Caelis</span><span>任务气泡 · 交互示意</span></header>
    <div className="companion">
      <Pet gesture={gesture}/>
      <div className={`task-bubbles ${expanded ? 'expanded' : ''}`} ref={zone}
        onPointerEnter={() => open()} onPointerLeave={leave}
        onFocus={() => open(true)}
        onBlur={e => { if (!e.currentTarget.contains(e.relatedTarget)) leave(); }}
        onKeyDown={e => {
          if (e.key === 'Escape') { e.preventDefault(); trigger.current?.focus(); close(); }
          if (['ArrowRight', 'ArrowLeft'].includes(e.key) && expanded) {
            e.preventDefault();
            const buttons = [...zone.current.querySelectorAll('.task-orb')];
            const i = buttons.indexOf(document.activeElement);
            const next = i < 0 ? (e.key === 'ArrowRight' ? 0 : buttons.length - 1) : (i + (e.key === 'ArrowRight' ? 1 : buttons.length - 1)) % buttons.length;
            buttons[next]?.focus();
          }
        }}>
        <button className="bubble-handle" ref={trigger} aria-label={`展开 ${tasks.length} 个任务`} aria-expanded={expanded} aria-controls="task-orbs" onClick={() => open(true)}>
          <span/><span/><span/>
        </button>
        {expanded && <div className="orb-row" id="task-orbs" aria-label="任务">
          {tasks.map((t, i) => <button key={t.id} className={`task-orb ${selected === t.id ? 'selected' : ''}`}
            aria-label={`任务 ${i + 1}：${t.prompt} 点击打开终端`} aria-describedby={selected === t.id ? 'original-prompt' : undefined}
            onPointerEnter={() => setSelected(t.id)} onFocus={() => setSelected(t.id)} onClick={() => attach(t)}>
            <span aria-hidden="true">{i + 1}</span>
          </button>)}
        </div>}
        {expanded && task && <div className="prompt-bubble" id="original-prompt" role="tooltip"><p>{task.prompt}</p></div>}
      </div>
    </div>
    <footer><p role="status">{feedback}</p><small>示例任务；本页不会启动真实终端</small></footer>
  </main>;
}
