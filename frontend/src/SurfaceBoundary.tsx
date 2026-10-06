import React from 'react';
import { desktop } from './desktop';
import { useI18n } from './i18n';

function Recovery({ retry }: { retry: () => void }) {
  const { t } = useI18n();
  return <main className="surface-recovery" role="status">
    <p>{t('chat.surfaceRecovering')}</p>
    <button onClick={retry}>{t('chat.recheckSetup')}</button>
    <button onClick={() => void desktop('OpenRuntimeSettings').catch(() => {})}>{t('chat.connectionSettings')}</button>
  </main>;
}

// A failed renderer belongs to this window. Native Runtime observation and the
// other surfaces stay alive; remounting presentation never replays an action.
export class SurfaceBoundary extends React.Component<React.PropsWithChildren, { failed: boolean }> {
  state = { failed: false };
  private timer?: ReturnType<typeof setTimeout>;
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch() { this.timer = setTimeout(this.retry, 3000); }
  componentWillUnmount() { clearTimeout(this.timer); }
  private retry = () => { clearTimeout(this.timer); this.setState({ failed: false }); };
  render() { return this.state.failed ? <Recovery retry={this.retry} /> : this.props.children; }
}
