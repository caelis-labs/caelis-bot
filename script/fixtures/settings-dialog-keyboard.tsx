import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { SettingsDialog } from '../../frontend/src/settings/runtime/SettingsDialog';
import { ModelNameInput } from '../../frontend/src/settings/runtime/ModelNameInput';
import type { ConnectionModelOption } from '../../frontend/src/settings/runtime/types';

const options: ConnectionModelOption[] = [
 { value: 'fixture/long-chat-model-alpha', name: 'Long chat model alpha', detail: '', chatEligibility: 'chat' },
 { value: 'fixture/long-chat-model-beta', name: 'Long chat model beta', detail: '', chatEligibility: 'chat' },
];

function KeyboardFixture() {
 const [open, setOpen] = useState(true);
 const [key, setKey] = useState('unsent-test-key');
 const [model, setModel] = useState('');
 const [closes, setCloses] = useState(0);
 const [submits, setSubmits] = useState(0);
 return <>
  <button id="reopen" type="button" onClick={() => setOpen(true)}>Reopen</button>
  <output id="close-count">{closes}</output>
  <output id="submit-count">{submits}</output>
  {open && <SettingsDialog title="Connection" portalTarget={document.getElementById('dialog-portal')!} onClose={() => { setCloses(count => count + 1); setOpen(false); }}>
   <form onSubmit={event => { event.preventDefault(); setSubmits(count => count + 1); }}>
    <label htmlFor="api-key">API Key</label><input id="api-key" type="password" value={key} onChange={event => setKey(event.target.value)}/>
    <label htmlFor="model-name">Model</label><ModelNameInput id="model-name" value={model} onChange={setModel} options={options} placeholder="Model" disabled={false}/>
    <button id="after-model" type="button">After model</button>
    <button id="submit" type="submit">Submit</button>
   </form>
  </SettingsDialog>}
 </>;
}

createRoot(document.getElementById('root')!).render(<KeyboardFixture/>);
