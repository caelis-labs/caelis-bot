import { desktop } from './desktop';

export type PresentationWarning = 'surface_render_failed' | 'bubble_action_failed' | 'background_read_failed' | 'setup_activation_failed';
const reported = new Set<PresentationWarning>();
// Deliberately accept a classification, not the original exception or payload.
export function presentationWarning(code:PresentationWarning) {
 if(reported.has(code))return;
 reported.add(code);
 void desktop('ReportPresentationWarning',code).catch(()=>{});
}
