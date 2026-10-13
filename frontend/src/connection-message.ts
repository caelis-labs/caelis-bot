import type { MessageKey } from './i18n/catalogs';

// Only native messages with a known, fixed meaning are presentation aliases.
// Unknown Host details must remain intact so address, auth and model errors are not hidden.
const knownMessages: Record<string, MessageKey> = {
 'Caelis 审查配置与当前对话不匹配': 'chat.reviewConfigurationMismatch',
 'Caelis Guardian 配置与当前对话不匹配': 'chat.reviewerConfigurationMismatch',
 'Caelis Guardian 模型不可用，请在运行时连接设置中恢复该模型；不会转为免审批或人工放行': 'chat.reviewerModelUnavailable',
 'Caelis 审查状态无法确认，请更新并检查运行时': 'chat.reviewStateUnknown',
};

export function displayConnectionMessage(message: string | undefined, t: (key: MessageKey) => string): string {
 if (!message) return '';
 const key = knownMessages[message];
 return key ? t(key) : message;
}
