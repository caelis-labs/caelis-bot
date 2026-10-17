import React, { useState } from 'react';
import { desktop } from './desktop';
import { useI18n } from './i18n';
import { presentationWarning } from './presentation-warning';

function Recovery() {
  const { t } = useI18n();
  const [copied,setCopied]=useState(false);
  const copy=async()=>{try{await desktop('CopyDiagnostics');setCopied(true);}catch{presentationWarning('background_read_failed');}};
  return <main className="surface-recovery" role="status">
    <p>{t('chat.surfaceRestartNeeded')}</p>
    <div><button onClick={()=>void copy()}>{t(copied?'chat.feedbackCopied':'chat.copyFeedback')}</button>
    <button className="primary" onClick={()=>window.location.reload()}>{t('chat.restartSurface')}</button></div>
  </main>;
}

// A failed renderer belongs to this window. Native Runtime observation and the
// other surfaces stay alive; remounting presentation never replays an action.
export class SurfaceBoundary extends React.Component<React.PropsWithChildren, { failed: boolean; fatal:boolean }> {
  state = { failed: false, fatal:false };
  private timer?: ReturnType<typeof setTimeout>;
  private recovered = false;
  static getDerivedStateFromError() { return { failed: true }; }
  componentDidCatch() {
    presentationWarning('surface_render_failed');
    if(this.recovered){this.setState({fatal:true});return;}
    this.recovered=true;
    this.timer=setTimeout(()=>this.setState({failed:false}),300);
  }
  componentWillUnmount() { clearTimeout(this.timer); }
  render() { return this.state.failed ? this.state.fatal?<Recovery/>:null : this.props.children; }
}
