import {Component, lazy, Suspense, type ReactNode} from 'react';

const ExpressSurface = lazy(() => import('./ExpressChatSurface'));

export function A2uiSource({source}: {source: string}) {
 return <pre className="chat-a2ui-source"><code>{source}</code></pre>;
}

class BlockBoundary extends Component<{fallback: ReactNode; children: ReactNode}, {failed: boolean}> {
 state = {failed: false};
 static getDerivedStateFromError() { return {failed: true}; }
 render() { return this.state.failed ? this.props.fallback : this.props.children; }
}

export function ChatA2uiBlock({source, body, closed, receiving}: {source: string; body: string; closed: boolean; receiving: boolean}) {
 if (!closed && receiving) return null;
 const fallback = <A2uiSource source={source}/>;
 if (!closed || !body) return fallback;
 return <BlockBoundary key={source} fallback={fallback}>
  <Suspense fallback={null}><ExpressSurface body={body} fallback={fallback}/></Suspense>
 </BlockBoundary>;
}
