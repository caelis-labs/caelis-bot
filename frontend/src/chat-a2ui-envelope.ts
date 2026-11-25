// A close belongs to the preceding opener, up to the next opener. With no
// close, a blank line followed by explicit Markdown gives the prose back.
export const chatA2uiOpen = /^<a2ui>[ \t]*$/;
const chatA2uiClose = /^<\/a2ui>[ \t]*$/;
const markdownAfterBlank = /^(?:#{1,6}[ \t]|[-+*][ \t]|\d+[.)][ \t]|\[[^\]]+\](?:\(|:)|>|\x60{3}|~{3})/;
export const partialChatA2uiOpen = /^<a2(?:u(?:i)?)?$/;

export function chatA2uiEnvelope(source: string, start: number) {
 const lines = /[^\r\n]*(?:\r\n|[\r\n]|$)/g;
 lines.lastIndex = start;
 lines.exec(source); // Opening line.
 let blank = false, recovery = source.length;
 for (let part; (part = lines.exec(source))?.[0];) {
  const line = part[0].replace(/[\r\n]+$/, '');
  const index = part.index;
  if (chatA2uiOpen.test(line)) return {end: Math.min(recovery, index), close: undefined};
  if (chatA2uiClose.test(line)) return {end: index + line.length, close: index};
  if (blank && markdownAfterBlank.test(line)) recovery = Math.min(recovery, index);
  blank = /^[ \t]*$/.test(line);
 }
 return {end: recovery, close: undefined};
}
