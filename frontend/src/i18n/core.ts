import { english, chinese } from './catalogs.ts';
import type { Messages, MessageKey } from './catalogs.ts';

export type Locale = 'en' | 'zh-CN';
export type LanguagePreference = 'system' | Locale;
export type LanguageState = { preference: LanguagePreference; locale: Locale; revision: number };
export type Parameters = Record<string, string | number>;
export const initialLanguage: LanguageState = { preference: 'system', locale: 'en', revision: 0 };

// OS languages are resolved by the host. Renderer navigator.language is never
// a second preference authority; standalone previews provide an explicit state.
export function newerLanguage(current: LanguageState, next: LanguageState): LanguageState {
 return next.revision >= current.revision ? next : current;
}
export function formatMessage(locale: Locale, key: string, parameters: Parameters = {}, catalogs: Record<Locale, Messages> = {en: english, 'zh-CN': chinese}): string {
 const dot=key.indexOf('.'), ns=key.slice(0,dot), name=key.slice(dot+1);
 const message=catalogs[locale]?.[ns]?.[name] ?? catalogs.en[ns]?.[name];
 if(message===undefined)return key;
 let template: string;
 if(typeof message==='string')template=message;
 else {
  const count=parameters.count;
  if(typeof count!=='number'||!Number.isFinite(count))throw new Error(`Numeric count required: ${key}`);
  const category=new Intl.PluralRules(locale).select(count);
  template=category==='one'&&message.one!==undefined?message.one:message.other;
 }
 return template.replace(/\{([a-zA-Z][a-zA-Z0-9_]*)\}/g,(token,name:string)=>Object.hasOwn(parameters,name)?String(parameters[name]):token);
}
export function translator(locale: Locale) {
 return {
  t:(key:MessageKey,parameters?:Parameters)=>formatMessage(locale,key,parameters),
  number:(value:number,options?:Intl.NumberFormatOptions)=>new Intl.NumberFormat(locale,options).format(value),
  date:(value:Date|number,options?:Intl.DateTimeFormatOptions)=>new Intl.DateTimeFormat(locale,options).format(value),
 };
}
