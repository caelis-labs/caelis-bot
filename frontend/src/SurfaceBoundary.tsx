import React, { useState } from 'react';
import { desktop } from './desktop';
import { useI18n } from './i18n';
import { presentationWarning } from './presentation-warning';
import { ArrowClockwiseIcon } from '@phosphor-icons/react/dist/csr/ArrowClockwise';
import { CopyIcon } from '@phosphor-icons/react/dist/csr/Copy';
import { CheckIcon } from '@phosphor-icons/react/dist/csr/Check';

// Contextual errors do not reload a surface or manufacture a retry operation.
// Callers supply the actual failure and, only when safe, a scoped retry.
export function Recovery({message,onRetry}:{message:string;onRetry?:()=>void}) {
  const {t}=useI18n();
  return <p className="operation-error" role="status"><span>{message}</span>{onRetry&&<button type="button" className="text-action" onClick={onRetry}>{t('settings.runtimeTryAgain')}</button>}</p>;
}

function SurfaceRecovery() {
  const { t } = useI18n();
  const [copied,setCopied]=useState(false);
  const copy=async()=>{try{await desktop('CopyDiagnostics');setCopied(true);}catch{presentationWarning('background_read_failed');}};
  return <section className="surface-recovery is-fullscreen" role="alert">
    <span className="surface-recovery-icon" aria-hidden="true"><ArrowClockwiseIcon size={20}/></span>
    <div className="surface-recovery-content">
      <strong>{t('chat.surfaceRestartTitle')}</strong>
      <p>{t('chat.surfaceRestartNeeded')}</p>
      <div className="surface-recovery-actions">
        <button type="button" className="surface-recovery-primary" onClick={()=>window.location.reload()}>{t('chat.restartSurface')}</button>
        <button type="button" className="surface-recovery-feedback" onClick={()=>void copy()}>{copied?<CheckIcon size={14} aria-hidden="true"/>:<CopyIcon size={14} aria-hidden="true"/>}{t(copied?'chat.feedbackCopied':'chat.copyFeedback')}</button>
      </div>
    </div>
  </section>;
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
  render() { return this.state.failed ? this.state.fatal?<SurfaceRecovery/>:null : this.props.children; }
}
