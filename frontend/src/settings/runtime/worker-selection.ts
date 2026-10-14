import type {LocalWorkerSettings} from '../../backend/contract';
import type {backend} from '../../desktop';

// Availability is not authority. A saved Worker default must come from the
// native read, including after a successful selection command.
export async function readSelectedWorker(call: typeof backend, requested?: string): Promise<LocalWorkerSettings> {
 if (requested) await call<LocalWorkerSettings>('SelectLocalWorker', requested);
 const selected = await call<string>('SelectedLocalWorker');
 if (selected !== 'caelis' && selected !== 'codex') throw new Error('Worker selection is unavailable');
 if (requested && selected !== requested) throw new Error('Worker selection did not commit');
 const view = await call<LocalWorkerSettings>('InspectWorkerRuntime', selected);
 if (view.runtime !== selected) throw new Error('Worker inspection does not match the saved selection');
 return view;
}
