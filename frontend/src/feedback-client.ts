import {backend,desktop} from './desktop';
import {isWebSurface} from './transport';
import {decodeDiagnostic,feedbackSummary,type FeedbackContext} from './feedback';

export async function copyFeedback(context:FeedbackContext={},locale?:string){
 const [report,version,overview,initialization]=await Promise.all([
  backend('DiagnosticReport').catch(()=>undefined),
  desktop('AppVersion').catch(()=>undefined),
  backend<{onboarding:boolean}>('SetupOverview').catch(()=>undefined),
  backend<{required:boolean}>('BotInitialization').catch(()=>undefined),
 ]);
 const page=context.page??(location.hash==='#/setup'?'setup':document.body.dataset.surface);
 const text=feedbackSummary({context:{...context,page},locale,web:isWebSurface(),version,report:decodeDiagnostic(report),setup:overview?.onboarding||initialization?.required});
 await desktop('CopyText',text);
}
