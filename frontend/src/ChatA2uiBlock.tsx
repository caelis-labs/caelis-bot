import {Component, createContext, lazy, Suspense, useContext, type ReactNode} from 'react';

import {canRenderChatA2ui} from './chat-a2ui-capabilities';

// A hidden native history window retains its React tree. Only its temporary
// surface models are released, leaving history, composer and copy intact.
export const ChatA2uiMount = createContext<number | null>(0);

const ExpressSurface = lazy(() => import('./ExpressChatSurface'));

export function A2uiSource({source}: {source: string}) {
 return <pre className="chat-a2ui-source"><code>{source}</code></pre>;
}

class BlockBoundary extends Component<{fallback: ReactNode; children: ReactNode}, {failed: boolean}> {
 state = {failed: false};
 static getDerivedStateFromError() { return {failed: true}; }
 render() { return this.state.failed ? this.props.fallback : this.props.children; }
}

export function ChatA2uiBlock({source, body, closed, receiving, permitted}: {source: string; body: string; closed: boolean; receiving: boolean; permitted: boolean}) {
 const epoch = useContext(ChatA2uiMount);
 if (epoch === null || !closed && receiving) return null;
 const fallback = <A2uiSource source={source}/>;
 if (!closed || !body || !permitted || !canRenderChatA2ui(globalThis)) return fallback;
 return <BlockBoundary key={`${epoch}:${source}`} fallback={fallback}>
  <Suspense fallback={null}><ExpressSurface body={body} fallback={fallback}/></Suspense>
 </BlockBoundary>;
}
