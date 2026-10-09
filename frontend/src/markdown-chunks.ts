// Bound the work done when a long reply grows. A completed chunk never changes
// on an append, so React can keep its parsed Markdown and DOM in place.
// Split only at top-level blank lines; constructs that can span those lines
// stay together. Reference definitions can affect earlier blocks, so they use
// the original whole-message parser.
const targetSize = 3072;
const referenceDefinition = /^ {0,3}\[[^\]\n]+\]:/m;
const htmlBlock = /^ {0,3}(?:<!--|<\/?[A-Za-z][\w-]*(?:[ \t/>]|$))/m;
const listItem = /^ {0,3}(?:[-+*]|\d+[.)])[ \t]+/;
const heading = /^#{1,6}(?:[ \t]|$)/;
const fenceMarker = /^ {0,3}(`{3,}|~{3,})/;

function independent(previous: string, next: string): boolean {
 if (heading.test(next)) return true;
 if (/^[ \t>]/.test(previous) || /^[ \t>]/.test(next)) return false;
 if (listItem.test(previous) || listItem.test(next)) return false;
 return true;
}

export function markdownChunks(text: string): string[] {
 if (text.length <= targetSize || referenceDefinition.test(text) || htmlBlock.test(text)) return [text];
 const chunks: string[] = [];
 let start = 0, lineStart = 0, previous = '', boundary = 0;
 let fence = '', fenceLength = 0;
 while (lineStart < text.length) {
  const end = text.indexOf('\n', lineStart);
  const lineEnd = end < 0 ? text.length : end;
  const line = text.slice(lineStart, lineEnd).replace(/\r$/, '');
  const blank = line.trim() === '';
  if (!fence && !blank && boundary && boundary - start >= targetSize && independent(previous, line)) {
   chunks.push(text.slice(start, boundary));
   start = boundary;
  }
  if (!blank) boundary = 0;
  const marker = line.match(fenceMarker)?.[1];
  if (marker) {
   if (!fence) { fence = marker[0]; fenceLength = marker.length; }
   else if (marker[0] === fence && marker.length >= fenceLength && line.slice(line.indexOf(marker) + marker.length).trim() === '') { fence = ''; fenceLength = 0; }
  }
  if (!fence && blank && end >= 0) boundary = end + 1;
  if (!blank) previous = line;
  lineStart = lineEnd + 1;
 }
 chunks.push(text.slice(start));
 return chunks;
}
