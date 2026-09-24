import en_common from '../../../internal/i18n/locales/en/common.json' with { type: 'json' };
import en_settings from '../../../internal/i18n/locales/en/settings.json' with { type: 'json' };
import en_chat from '../../../internal/i18n/locales/en/chat.json' with { type: 'json' };
import en_runtime from '../../../internal/i18n/locales/en/runtime.json' with { type: 'json' };
import en_connections from '../../../internal/i18n/locales/en/connections.json' with { type: 'json' };
import en_native from '../../../internal/i18n/locales/en/native.json' with { type: 'json' };
import en_host from '../../../internal/i18n/locales/en/host.json' with { type: 'json' };
import zh_common from '../../../internal/i18n/locales/zh-CN/common.json' with { type: 'json' };
import zh_settings from '../../../internal/i18n/locales/zh-CN/settings.json' with { type: 'json' };
import zh_chat from '../../../internal/i18n/locales/zh-CN/chat.json' with { type: 'json' };
import zh_runtime from '../../../internal/i18n/locales/zh-CN/runtime.json' with { type: 'json' };
import zh_connections from '../../../internal/i18n/locales/zh-CN/connections.json' with { type: 'json' };
import zh_native from '../../../internal/i18n/locales/zh-CN/native.json' with { type: 'json' };
import zh_host from '../../../internal/i18n/locales/zh-CN/host.json' with { type: 'json' };

export const english = {
 common: en_common,
 settings: en_settings,
 chat: en_chat,
 runtime: en_runtime,
 connections: en_connections,
 native: en_native,
 host: en_host,
} as const;
export const chinese = {
 common: zh_common,
 settings: zh_settings,
 chat: zh_chat,
 runtime: zh_runtime,
 connections: zh_connections,
 native: zh_native,
 host: zh_host,
} as const;
export type Message = string | { other: string; one?: string };
type Catalog = typeof english;
export type MessageKey = { [N in keyof Catalog]: `${N}.${keyof Catalog[N] & string}` }[keyof Catalog];
export type Messages = Record<string, Record<string, Message>>;
