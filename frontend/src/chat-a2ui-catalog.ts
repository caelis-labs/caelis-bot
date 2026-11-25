import {basicCatalog, TextApi} from '@a2ui/web_core/catalogs/basic/v1';
import {z} from 'zod';

// Keep the first POC's title variant so saved replies still render.
export const chatText = {
 ...TextApi,
 schema: TextApi.schema.extend({variant: z.enum([...TextApi.schema.shape.variant.unwrap().removeDefault().options, 'title']).optional()}),
};

export const formComponents = ['TextField', 'CheckBox', 'ChoicePicker', 'Slider'];
export const chatCatalog = basicCatalog.derive({
 catalogId: 'https://caelis.dev/catalogs/chat-preview',
 components: [chatText, 'Row', 'Column', 'List', 'Card', 'Tabs', 'Divider', ...formComponents],
 functions: ['required', 'length', 'numeric', 'email', 'formatNumber'],
});
