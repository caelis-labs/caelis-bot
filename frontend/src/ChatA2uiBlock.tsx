import {Component, createContext, lazy, Suspense, useContext, useEffect, useState, type ReactNode} from 'react';

import {canRenderChatA2ui} from './chat-a2ui-capabilities';

// A hidden native history window retains its React tree. The Express surface
// releases previews and keeps action cards available for continued editing.
export const ChatA2uiMount = createContext(true);

const ExpressSurface = lazy(() => import('./ExpressChatSurface'));

export function A2uiSource({source}: {source: string}) {
 return <pre className="chat-a2ui-source"><code>{source}</code></pre>;
}

class BlockBoundary extends Component<{fallback: ReactNode; children: ReactNode}, {failed: boolean}> {
 state = {failed: false};
 static getDerivedStateFromError() { return {failed: true}; }
 render() { return this.state.failed ? this.props.fallback : this.props.children; }
}

export function ChatA2uiBlock({source, body, closed, receiving, permitted, replyKey}: {
 source: string; body: string; closed: boolean; receiving: boolean; permitted: boolean; replyKey?: string;
}) {
 const visible = useContext(ChatA2uiMount);
 const [seen, setSeen] = useState(visible);
 useEffect(() => { if (visible) setSeen(true); }, [visible]);
 if ((!seen && !visible) || !closed && receiving) return null;
 const fallback = <A2uiSource source={source}/>;
 if (!closed || !body || !permitted || !canRenderChatA2ui(globalThis)) return visible ? fallback : null;
 return <BlockBoundary key={source} fallback={visible ? fallback : null}>
  <Suspense fallback={null}><ExpressSurface body={body} fallback={fallback} replyKey={replyKey} receiving={receiving} visible={visible}/></Suspense>
 </BlockBoundary>;
}
