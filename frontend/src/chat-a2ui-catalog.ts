import {basicCatalog, CardApi, TextApi} from '@a2ui/web_core/catalogs/basic/v1';
import {z} from 'zod';

// Keep the first POC's title variant so saved replies still render.
export const chatText = {
 ...TextApi,
 schema: TextApi.schema.extend({variant: z.enum([...TextApi.schema.shape.variant.unwrap().removeDefault().options, 'title']).optional()}),
};

// Only this host-owned root has a send affordance. It carries no A2UI actions.
export const chatForm = {
 name: 'Form',
 schema: CardApi.schema.omit({weight: true}).extend({
  title: z.string().min(1).max(160).describe('The title of the answer form.'),
 }).describe('A root-only chat form. The desktop offers one button to send the visible answers as a chat message.'),
};

export const formComponents = ['TextField', 'CheckBox', 'ChoicePicker', 'Slider'];
export const chatCatalog = basicCatalog.derive({
 catalogId: 'https://caelis.dev/catalogs/chat-preview',
 components: [chatText, 'Row', 'Column', 'List', 'Card', 'Tabs', 'Divider', ...formComponents, chatForm],
 functions: ['required', 'length', 'numeric', 'email', 'formatNumber'],
});
