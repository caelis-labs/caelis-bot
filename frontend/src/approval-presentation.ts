import type { Approval, Choice } from './backend/contract';
import { english } from './i18n/catalogs.ts';
import { formatMessage, type Locale } from './i18n/core.ts';

// Only adapter-owned presentation keys are translated. Native labels, reasons,
// commands, questions and decision IDs remain verbatim, including known key names.
export function approvalText(locale: Locale, key: string | undefined, fallback: string, parameters = {}) {
 const [namespace, name] = key?.split('.') ?? [];
 if (!namespace || !Object.hasOwn(english, namespace) || !Object.hasOwn(english[namespace as keyof typeof english], name)) return fallback;
 return formatMessage(locale, key!, parameters);
}
export function approvalTitle(value: Approval, locale: Locale) {
 return approvalText(locale, value.titleKey, value.title, {name: value.title});
}
export function approvalChoice(value: Choice, locale: Locale) {
 return approvalText(locale, value.labelKey, value.label);
}
export function approvalResult(value: Approval, locale: Locale) {
 if(value.status!=='resolved')return approvalText(locale,value.status==='sent'||value.status==='sending'?'chat.approvalSent':'chat.approvalUnknown','');
 const result=value.resolution;
 if(result?.outcome==='declined')return approvalText(locale,'chat.approvalDeclined','');
 if(result?.outcome==='cancelled')return approvalText(locale,'chat.approvalCancelled','');
 if(result?.outcome==='allowed'){
  const scopeKey:Record<string,string>={once:'chat.approvalScopeOnce',session:'chat.approvalScopeSession',always:'chat.approvalScopeAlways',conversation:'chat.approvalScopeConversation',turn:'chat.approvalScopeTurn',rule:'chat.approvalScopeRule'};
  const scope=approvalText(locale,scopeKey[result.scope??''],'');
  return scope?approvalText(locale,'chat.approvalAllowedScope','',{scope}):approvalText(locale,'chat.approvalAllowed','');
 }
 return approvalText(locale,'chat.approvalResolved','');
}
