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
