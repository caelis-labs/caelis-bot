import type {Extension as MarkdownExtension} from 'mdast-util-from-markdown';
import type {Code, Construct, Extension, State} from 'micromark-util-types';

declare module 'micromark-util-types' {
 interface TokenTypeMap {
  chatA2ui: 'chatA2ui';
  chatA2uiLine: 'chatA2uiLine';
 }
}

const openTag = '<a2ui>';
const closeTag = '</a2ui>';
const lineEnd = (code: Code) => code === -5 || code === -4 || code === -3;

const closingLine: Construct = {
 partial: true,
 tokenize(effects, ok, nok) {
  let index = 0;
  const tag: State = code => {
   if (index === closeTag.length) {
    if (code !== null && !lineEnd(code)) return nok(code);
    effects.exit('chatA2uiLine');
    return ok(code);
   }
   if (code !== closeTag.charCodeAt(index++)) return nok(code);
   effects.consume(code);
   return tag;
  };
  return code => {
   effects.enter('chatA2uiLine');
   return tag(code);
  };
 },
};

const block: Construct = {
 name: 'chatA2ui',
 concrete: true,
 tokenize(effects, ok, nok) {
  // Require a top-level block boundary. CommonMark owns code spans, code blocks,
  // quoted/list content, escapes, and any existing HTML construct.
  if (this.interrupt || this.now().column !== 1) return nok;
  let index = 0;
  const finish: State = code => {
   effects.exit('chatA2ui');
   return ok(code);
  };
  const nextLine: State = code => {
   if (code === null) return finish(code);
   if (lineEnd(code)) return consumeLineEnd(code);
   if (code === 60) return effects.attempt(closingLine, finish, beginBodyLine)(code);
   return beginBodyLine(code);
  };
  const consumeLineEnd = (code: Code) => {
   effects.enter('lineEnding');
   effects.consume(code);
   effects.exit('lineEnding');
   return nextLine;
  };
  const body: State = code => {
   if (code === null || lineEnd(code)) {
    effects.exit('chatA2uiLine');
    return code === null ? finish(code) : consumeLineEnd(code);
   }
   effects.consume(code);
   return body;
  };
  const beginBodyLine: State = code => {
   effects.enter('chatA2uiLine');
   return body(code);
  };
  const tag: State = code => {
   if (index === openTag.length) {
    if (code === null) return finish(code);
    if (!lineEnd(code)) return nok(code);
    return consumeLineEnd(code);
   }
   if (code === null && index >= 3) return finish(code);
   if (code !== openTag.charCodeAt(index++)) return nok(code);
   effects.consume(code);
   return tag;
  };
  return code => {
   effects.enter('chatA2ui');
   return tag(code);
  };
 },
};

const syntax: Extension = {flow: {60: block}};

type ParserData = {micromarkExtensions?: Extension[]; fromMarkdownExtensions?: MarkdownExtension[]};

export function remarkChatA2ui(this: {data(): object}, {receiving = false} = {}) {
 const data = this.data() as ParserData;
 const markdown: MarkdownExtension = {
  enter: {
   chatA2ui(token) {
    const source = this.sliceSerialize(token);
    const closed = /(?:\r\n|\n|\r)<\/a2ui>$/.test(source);
    const body = closed ? source.slice(openTag.length, -closeTag.length).trim() : '';
    this.enter({type: 'thematicBreak', data: {hName: 'chat-a2ui-block', hProperties: {source, body, closed, receiving}}}, token);
   },
  },
  exit: {chatA2ui(token) { this.exit(token); }},
 };
 (data.micromarkExtensions ??= []).push(syntax);
 (data.fromMarkdownExtensions ??= []).push(markdown);
}
