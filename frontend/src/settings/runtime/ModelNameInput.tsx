import { useId, useState } from 'react';
import type { ConnectionModelOption } from './types';
import { modelNameSuggestions } from './connection-models';

export function ModelNameInput({ id, value, onChange, options, placeholder, disabled }: {
 id: string;
 value: string;
 onChange: (value: string) => void;
 options: readonly ConnectionModelOption[];
 placeholder: string;
 disabled: boolean;
}) {
 const listID = useId();
 const [open, setOpen] = useState(false);
 const [active, setActive] = useState(0);
 const suggestions = modelNameSuggestions(options, value);
 const visible = open && !disabled && suggestions.length > 0;
 const activeIndex = Math.min(active, suggestions.length - 1);
 const choose = (model: ConnectionModelOption) => { onChange(model.value); setOpen(false); setActive(0); };

 return <div className="connection-model-combobox">
  <input id={id} role="combobox" aria-autocomplete="list" aria-expanded={visible} aria-controls={visible ? listID : undefined} aria-activedescendant={visible ? `${listID}-${activeIndex}` : undefined} data-dialog-escape-first={visible ? '' : undefined}
   autoComplete="off" spellCheck={false} disabled={disabled} value={value} placeholder={placeholder}
   onFocus={() => setOpen(true)} onBlur={() => setOpen(false)}
   onChange={event => { onChange(event.target.value); setActive(0); setOpen(true); }}
   onKeyDown={event => {
    if (event.key === 'Escape' && visible) { event.preventDefault(); event.stopPropagation(); setOpen(false); return; }
    if (event.key === 'ArrowDown' && suggestions.length) { event.preventDefault(); setOpen(true); setActive(index => Math.min(index + 1, suggestions.length - 1)); }
    if (event.key === 'ArrowUp' && suggestions.length) { event.preventDefault(); setOpen(true); setActive(index => Math.max(index - 1, 0)); }
    if (event.key === 'Enter' && visible) { event.preventDefault(); choose(suggestions[activeIndex]); }
   }}/>
  {visible && <div id={listID} role="listbox" className="connection-model-suggestions">
   {suggestions.map((model, index) => <button key={model.value} id={`${listID}-${index}`} type="button" role="option" tabIndex={-1} aria-selected={index === activeIndex}
    onMouseDown={event => event.preventDefault()} onClick={() => choose(model)}>
    <span>{model.name}</span>{model.name !== model.value && <small>{model.value}</small>}
   </button>)}
  </div>}
  {value.length > 32 && <small className="connection-model-full-name">{value}</small>}
 </div>;
}
