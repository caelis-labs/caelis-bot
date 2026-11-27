import {readFileSync, writeFileSync} from 'node:fs';
import {ExpressFormat, SkillGenerator} from '@a2ui/agent';
import {chatCatalog} from '../frontend/src/chat-a2ui-catalog.ts';
import {comparison, collection, draft, budget, calculator, weather, practice} from './a2ui-chat-examples.mjs';

const target = new URL('../internal/botskills/skills/bot-core/references/a2ui-chat.md', import.meta.url);
// Keep the historical Form renderer out of the card authoring reference.
const authoringCatalog = chatCatalog.derive({
 catalogId: chatCatalog.id,
 components: [...chatCatalog.components.keys()].filter(name => name !== 'Form'),
});
const format = new ExpressFormat([authoringCatalog], {version: 'v1.0', allowedMessages: ['createSurface']});
const generated = new SkillGenerator(format).generateSkill().content;
// Replace the general event and surface rules with the desktop action contract.
const sections = generated.split(/\n(?=\d+\. |## )/);
const unsupported = /^(8\. Action events|12\. Required actions|13\. Surface targeting)/;
if (sections.filter(section => unsupported.test(section)).length !== 3) throw Error('Review upstream Express rules before regenerating');
const rules = sections.filter(section => !unsupported.test(section)).join('\n')
 .replace(/^You must output the user interface using A2UI Express\.\n\n/m, '')
 .replace(/^   root = ComponentB.*Event.*\n/m, '')
 .replace(/^   - Strings:[^\n]*\n     Raw Strings:[^\n]*\n/m, '   - Strings: Use double-quoted single-line strings.\n')
 .replace(/^   - Dates & Times:[^\n]*\n/m, '')
 .replace(/^7\. Logic and validation:[^\n]*/m, '7. Prefix supported client checks with ?. For example, ?email("Enter an email address").')
 .replace('itemTemplate = Image($url)', 'itemTemplate = Text($name)');

const examples = [['Compare details in tabs', comparison], ['Repeat supplied records', collection], ['Edit and validate a local draft', draft], ['Compare and send a budget', budget], ['Calculate locally with a keypad', calculator], ['Send a weather query', weather], ['Practice across tabs, score locally or ask Bot', practice]];
const local = `# Present results in desktop chat

Give the important facts, recommendation and next step in Markdown. Add one compact block when tabs, grouped records, a local draft or a short questionnaire makes the reply easier to use. Put each sentinel at column one on its own line, with blank lines separating the block from prose. Keep each block independent.

Use the controls for a task the user can do in the card. Give every input a meaningful label and bind it to an initialized local path. TextField produces text, including its number variant. Use \`?required("…")\` for required answers and \`?numberInRange(0,"…")\` for numeric text. Use vertical ChoicePicker rows for long labels, chips for short choices, and Slider steps for bounded numeric choices. Use Table for a small set of records with column headers.

Use \`Button(Text("…"), action=Event("set", {"/path": value}))\` for local actions. Give each button a single Text label that names its effect. You can bind the label text to local data, including the current item in a repeated template. A set event updates one or more existing absolute paths on the same card. Each value is evaluated against the pre-click data, then all changes publish together. Declare initial values before binding the inputs. Use \`Show(condition, child)\` and a button's \`enabled\` to follow the same data. Tab changes preserve bindings. For calculated output, use \`display(value,"…")\` with a recovery message for invalid input such as division by zero.

To hand something to Bot, use a button with a specific call to action and \`Event("submit", {city: $/city, period: $/period}, userMessage="Check the weather in {city} for {period}.")\`. Each named placeholder must match a declared field. Field values can come from any tab and can be derived with a catalog function. Use \`enabled=and([present($/city),present($/period)])\` or relevant checks so all required answers are valid before sending. Declare only the values needed for this request, including when multiple tabs have other inputs or private notes. The user can keep editing while the card is open. Card inputs reset when the user reopens chat. The submitted text appears in history as a user message. Do not add a sending disclaimer or a copy of the outgoing text to the card.

Use a plain chat reply path on channels without desktop cards. Use the existing approval or question workflow for explicit Runtime decisions. Do not request secrets or hidden values.

Use the signatures below. Assign initial data with \`$/path = value\`. Use double-quoted, single-line strings, and up to 12 items per list or group. The host accepts one \`root\` tree of up to 40 components per block. Keep repeated templates shallow. Use only the set and submit button events. Omit surface directives, explicit updates, media and standalone calls.

## Desktop chat examples

${examples.map(([title, body]) => '### '+title+'\n\n```text\n<a2ui>\n'+body+'\n</a2ui>\n```').join('\n\n')}

## Protocol and catalog reference

<!-- Generated by @a2ui/agent SkillGenerator from the desktop catalog derived from @a2ui/web_core Basic v1.0. Apache-2.0. Bot narrows the output contract and examples to the supported subset. As installed, the license is in ../assets/A2UI-LICENSE. -->

`;
const result = local + rules.trimEnd() + '\n';
if (process.argv.includes('--check')) {
 // Git can check text files out with CRLF on Windows.
 if (readFileSync(target, 'utf8').replaceAll('\r\n', '\n') !== result) throw Error('Stale desktop chat A2UI reference. Run node script/generate-chat-a2ui-reference.mjs');
} else {
 writeFileSync(target, result);
}
