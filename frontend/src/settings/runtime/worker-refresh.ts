// Keep one native Worker operation in flight. Repeated refresh requests replace
// the queued read, then run after the current operation settles.
export function serialLatest<T>(run: (request: T) => Promise<void>, onBusy: (busy: boolean) => void, onError: (error: unknown) => void = () => {}) {
 let pending = false;
 let queued: T | undefined;
 const drain = async () => {
  if (pending) return;
  pending = true;
  onBusy(true);
  try {
   while (queued !== undefined) {
    const request = queued;
    queued = undefined;
    try { await run(request); } catch (error) { onError(error); }
   }
  } finally {
   pending = false;
   onBusy(false);
   if (queued !== undefined) void drain();
  }
 };
 return (request: T) => { queued = request; void drain(); };
}
