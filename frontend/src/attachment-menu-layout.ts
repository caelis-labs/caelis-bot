export type MenuLayout = { left: number; top: number; width: number; height: number };

// Anchor the attachment menu to the composer edge with a readable compact width,
// never stretching full width across wide composers.
export function attachmentMenuLayout(
  anchor: { left: number; top: number; bottom: number; width: number },
  viewport: { width: number; height: number },
  desiredHeight: number
): MenuLayout {
  const margin = 12;
  const gap = 8;
  const maxWidth = 344;
  const width = Math.min(anchor.width, maxWidth, Math.max(0, viewport.width - margin * 2));
  const above = Math.max(0, Math.min(anchor.top, viewport.height) - gap - margin);
  const height = Math.min(desiredHeight, above);
  return {
    left: Math.max(margin, Math.min(anchor.left, viewport.width - width - margin)),
    top: Math.max(margin, anchor.top - gap - height),
    width,
    height,
  };
}
