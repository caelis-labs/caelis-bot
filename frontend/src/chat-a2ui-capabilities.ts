// The compiler and the Basic Catalog initialize these before React can render.
type WebView = Pick<typeof globalThis, 'Object' | 'CSSStyleSheet' | 'Document' | 'ShadowRoot'>;

export function canRenderChatA2ui(webview: Partial<WebView>): boolean {
 return typeof webview.Object?.hasOwn === 'function'
  && typeof webview.CSSStyleSheet?.prototype.replaceSync === 'function'
  && !!webview.Document && 'adoptedStyleSheets' in webview.Document.prototype
  && !!webview.ShadowRoot && 'adoptedStyleSheets' in webview.ShadowRoot.prototype;
}
