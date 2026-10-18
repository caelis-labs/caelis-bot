// Hover freezes presentation order, never authoritative task facts. Removed
// cards disappear and new cards append until the pointer leaves the dock.
export function preserveTaskOrder<T extends {id: string}>(previous: readonly T[], incoming: readonly T[], freeze: boolean): T[] {
  if (!freeze) return [...incoming];
  const current = new Map(incoming.map(task => [task.id, task]));
  const ordered: T[] = [];
  for (const task of previous) {
    const fresh = current.get(task.id);
    if (fresh) {
      ordered.push(fresh);
      current.delete(task.id);
    }
  }
  return [...ordered, ...current.values()];
}
