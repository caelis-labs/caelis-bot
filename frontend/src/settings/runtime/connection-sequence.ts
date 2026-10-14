import type { ConnectInput, ConnectionFlow, RuntimeSettingsClient } from './types';

// The Host accepts one model per flow. Sequence confirmed commits and retain
// each native flow identity. A failed or unknown result stops the sequence.
export async function connectSelectedModels(
 client: RuntimeSettingsClient,
 input: Omit<ConnectInput, 'model'>,
 models: readonly string[],
 signal: AbortSignal,
 progress: (flow: ConnectionFlow) => void,
 completed: (count: number) => void,
): Promise<ConnectionFlow> {
 if (!models.length) throw new Error('No model selected');
 let last: ConnectionFlow | undefined;
 for (const [index, model] of models.entries()) {
  signal.throwIfAborted();
  const terminal = await new Promise<ConnectionFlow>((resolve, reject) => {
   let settled = false;
   const onAbort = () => {
    if (!settled) { settled = true; reject(new DOMException('Aborted', 'AbortError')); }
   };
   const update = (flow: ConnectionFlow) => {
    progress(flow);
    if (!settled && ['complete', 'failed', 'unknown'].includes(flow.stage)) {
     settled = true;
     signal.removeEventListener('abort', onAbort);
     resolve(flow);
    }
   };
   signal.addEventListener('abort', onAbort, { once: true });
   void client.startConnection({ ...input, model }, signal, update).then(update, error => {
    signal.removeEventListener('abort', onAbort);
    if (!settled) { settled = true; reject(error); }
   });
  });
  last = terminal;
  if (terminal.stage !== 'complete') return terminal;
  completed(index + 1);
 }
 return last!;
}
