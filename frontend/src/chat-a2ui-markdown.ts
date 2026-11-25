import {fromMarkdown, type Extension as MarkdownExtension} from 'mdast-util-from-markdown';
import type {Construct, Extension, State} from 'micromark-util-types';
import {chatA2uiEnvelope, chatA2uiOpen, partialChatA2uiOpen} from './chat-a2ui-envelope.ts';

declare module 'micromark-util-types' {
 interface TokenTypeMap {
  chatA2ui: 'chatA2ui';
  chatA2uiLine: 'chatA2uiLine';
 }
}

function chatA2uiSyntax(source: string): Extension {
 const block: Construct = {
  name: 'chatA2ui',
  concrete: true,
  tokenize(effects, ok, nok) {
   // CommonMark owns code spans, code blocks, containers and other HTML.
   const {column, offset} = this.now();
   if (this.interrupt || column !== 1) return nok;
   const rest = source.slice(offset);
   const line = rest.split(/[\r\n]/, 1)[0];
   const open = chatA2uiOpen.test(line);
   if (!open && !(line.length === rest.length && partialChatA2uiOpen.test(line))) return nok;
   const range = open ? chatA2uiEnvelope(source, offset) : {end: source.length, close: undefined};
   const end = range.close === undefined && range.end < source.length
    ? range.end - (source.slice(range.end - 2, range.end) === '\r\n' ? 2 : 1)
    : range.end;
   const finished = (code: Parameters<State>[0]) => code === null || this.now().offset >= end;
   const lineEnd = (code: Parameters<State>[0]) => code === -5 || code === -4 || code === -3;
   const finish: State = code => {
    effects.exit('chatA2ui');
    return ok(code);
   };
   const next: State = code => {
    if (finished(code)) return finish(code);
    if (lineEnd(code)) {
     effects.enter('lineEnding');
     effects.consume(code);
     effects.exit('lineEnding');
     return next;
    }
    effects.enter('chatA2uiLine');
    return consume(code);
   };
   const consume: State = code => {
    if (finished(code) || lineEnd(code)) {
     effects.exit('chatA2uiLine');
     return next(code);
    }
    effects.consume(code);
    return consume;
   };
   return code => {
    effects.enter('chatA2ui');
    return next(code);
   };
  },
 };
 return {flow: {60: block}};
}

function markdownExtension(source: string, receiving: boolean, blockOffset: number): MarkdownExtension {
 let count = blockOffset;
 return {
  enter: {
   chatA2ui(token) {
    const start = token.start.offset;
    const raw = source.slice(start, token.end.offset);
    const close = chatA2uiEnvelope(raw, 0).close;
    const body = close === undefined ? '' : raw.slice('<a2ui>'.length, close).trim();
    this.enter({type: 'thematicBreak', data: {hName: 'chat-a2ui-block',
     hProperties: {source: raw, body, closed: close !== undefined, receiving, permitted: count++ < 6}}}, token);
   },
  },
  exit: {chatA2ui(token) { this.exit(token); }},
 };
}

type ParserData = {micromarkExtensions?: Extension[]; fromMarkdownExtensions?: MarkdownExtension[]};

export function remarkChatA2ui(this: {data(): object}, {source, receiving = false, blockOffset = 0}: {source: string; receiving?: boolean; blockOffset?: number}) {
 const data = this.data() as ParserData;
 (data.micromarkExtensions ??= []).push(chatA2uiSyntax(source));
 (data.fromMarkdownExtensions ??= []).push(markdownExtension(source, receiving, blockOffset));
}

// Used by the pet bubble before text reveal, so animation never exposes DSL.
// Slice around the Markdown nodes to preserve literal examples and definitions.
export function readableChatReply(source: string, receiving: boolean, placeholder: string): string {
 if (!/^<a2/m.test(source)) return source;
 const tree = fromMarkdown(source, {extensions: [chatA2uiSyntax(source)], mdastExtensions: [markdownExtension(source, receiving, 0)]});
 let offset = 0, prose = '';
 for (const node of tree.children) {
  if (node.data?.hName !== 'chat-a2ui-block' || !node.position) continue;
  prose += source.slice(offset, node.position.start.offset);
  offset = node.position.end.offset ?? offset;
 }
 if (!offset) return source;
 prose = (prose + source.slice(offset)).replace(/^[\r\n]+|[\r\n]+$/g, '');
 return prose.trim() ? prose : (receiving ? '' : placeholder);
}
