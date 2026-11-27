import {basicCatalog, ButtonApi, CardApi, TextApi} from '@a2ui/web_core/catalogs/basic/v1';
import {ChildSchema, DynamicBooleanSchema, DynamicValueSchema} from '@a2ui/web_core/v1_0';
import {z} from 'zod';
import {chatFunctions} from './chat-a2ui-functions.ts';

// Keep the first POC's title variant so saved replies still render.
export const chatText = {
 ...TextApi,
 schema: TextApi.schema.extend({variant: z.enum([...TextApi.schema.shape.variant.unwrap().removeDefault().options, 'title']).optional()}),
};

// Keep root Form for replies saved before buttons supported submission.
export const chatForm = {
 name: 'Form',
 schema: CardApi.schema.omit({weight: true}).extend({
  title: z.string().min(1).max(160).describe('The title of the answer form.'),
 }).describe('Legacy root-only answer form. Use a Button with a submit event for new cards.'),
};

export const chatButton = {
 ...ButtonApi,
 allowedChildren: [chatText.name],
 schema: ButtonApi.schema.extend({
  child: ChildSchema.describe('REF:#/$defs/Child|Use a single Text component as the button label.'),
  enabled: DynamicBooleanSchema.describe('REF:#/$defs/DynamicBoolean|Allow this action only while this condition is true.').optional(),
 }),
};
export const chatShow = {
 name: 'Show',
 schema: z.object({
  when: DynamicBooleanSchema.describe('REF:#/$defs/DynamicBoolean|Show the child only while this condition is true.'),
  child: ChildSchema.describe('REF:#/$defs/Child'),
 }).strict(),
};
export const chatTable = {
 name: 'Table',
 schema: z.object({
  headers: z.array(z.string().min(1)).min(1).max(12),
  rows: DynamicValueSchema.describe('REF:#/$defs/DynamicValue|Rows of cell values in header order.'),
 }).strict(),
};

export const formComponents = ['TextField', 'CheckBox', 'ChoicePicker', 'Slider'];
export const chatCatalog = basicCatalog.derive({
 catalogId: 'https://caelis.dev/catalogs/chat-preview',
 components: [chatText, 'Row', 'Column', 'List', 'Card', 'Tabs', 'Divider', ...formComponents, chatForm, chatButton, chatShow, chatTable],
 functions: ['required', 'length', 'numeric', 'email', 'formatNumber', 'and', 'or', 'not', ...chatFunctions],
});
