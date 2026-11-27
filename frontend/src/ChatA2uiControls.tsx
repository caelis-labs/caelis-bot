import {Fragment, useContext, useId, useState, type CSSProperties, type ReactNode} from 'react';
import {createComponentImplementation} from '@a2ui/react';
import {type ResolvedChildRef} from '@a2ui/web_core';
import {CardApi, CheckBoxApi, ChoicePickerApi, ColumnApi, DividerApi, ListApi, RowApi, SliderApi, TabsApi, TextFieldApi} from '@a2ui/web_core/catalogs/basic/v1';
import {chatButton, chatForm, chatShow, chatTable, chatText} from './chat-a2ui-catalog';
import {ChatA2uiSubmitEnabled} from './ChatA2uiActions';
import {chatEvent} from './chat-a2ui-actions';
import {fieldText} from './chat-a2ui-functions';
import {ChatA2uiForm, useAnswer} from './ChatA2uiForm';
import {useI18n} from './i18n';

type BuildChild = (id: string, basePath?: string) => ReactNode;
function childrenOf(children: (string | ResolvedChildRef)[], buildChild: BuildChild, list = false) {
 return children.map((child, index) => {
  const node = typeof child === 'string' ? buildChild(child) : buildChild(child.id, child.basePath);
  const key = typeof child === 'string' ? `${child}:${index}` : `${child.id}:${child.basePath}:${index}`;
  return list ? <div key={key} role="listitem">{node}</div> : <Fragment key={key}>{node}</Fragment>;
 });
}

const justifyContent = {start: 'flex-start', end: 'flex-end', center: 'center', spaceAround: 'space-around', spaceBetween: 'space-between', spaceEvenly: 'space-evenly', stretch: 'stretch'};
const alignItems = {start: 'flex-start', end: 'flex-end', center: 'center', stretch: 'stretch'};

const text = createComponentImplementation(chatText, ({props}) => {
 const style = {flex: props.weight};
 if (props.variant === 'title') return <strong className="chat-a2ui-title" style={style}>{props.text}</strong>;
 if (props.variant === 'caption') return <small className="chat-a2ui-caption" style={style}>{props.text}</small>;
 return <span style={style}>{props.text}</span>;
});
const row = createComponentImplementation(RowApi, ({props, buildChild}) =>
 <div className="chat-a2ui-row" style={{flex: props.weight, justifyContent: justifyContent[props.justify ?? 'start'], alignItems: alignItems[props.align ?? 'stretch']}}>
  {childrenOf(props.children, buildChild)}
 </div>);
const column = createComponentImplementation(ColumnApi, ({props, buildChild}) =>
 <div className="chat-a2ui-column" style={{flex: props.weight, justifyContent: justifyContent[props.justify ?? 'start'], alignItems: alignItems[props.align ?? 'stretch']}}>
  {childrenOf(props.children, buildChild)}
 </div>);
const list = createComponentImplementation(ListApi, ({props, buildChild}) =>
 <div role="list" className={props.direction === 'horizontal' ? 'chat-a2ui-row' : 'chat-a2ui-column'} style={{flex: props.weight, alignItems: alignItems[props.align ?? 'stretch']}}>
  {childrenOf(props.children, buildChild, true)}
 </div>);
const card = createComponentImplementation(CardApi, ({props, buildChild}) => <div className="chat-a2ui-card" style={{flex: props.weight}}>{buildChild(props.child)}</div>);
const divider = createComponentImplementation(DividerApi, ({props}) =>
 <hr className="chat-a2ui-divider" aria-orientation={props.axis ?? 'horizontal'} style={{flex: props.weight}}/>);
const form = createComponentImplementation(chatForm, ({props, buildChild}) => <ChatA2uiForm title={props.title}>{buildChild(props.child)}</ChatA2uiForm>);

const button = createComponentImplementation(chatButton, ({props, context, buildChild, accessibilityRef}) => {
 const id = useId();
 const [checked, setChecked] = useState(false);
 const canSubmit = useContext(ChatA2uiSubmitEnabled);
 const submits = chatEvent(context.componentModel.properties.action) === 'submit';
 const invalid = checked && props.isValid === false;
 return <div className="chat-a2ui-action" style={{flex: props.weight}}>
  <button ref={accessibilityRef} className="chat-a2ui-button" data-variant={props.variant}
   type="button" disabled={props.enabled === false || submits && !canSubmit}
   aria-describedby={invalid ? `${id}-error` : undefined} onClick={() => {
    setChecked(true);
    if (props.isValid !== false) void props.action();
   }}>{buildChild(props.child)}</button>
  {invalid && <FieldError id={id} messages={props.validationErrors}/>}
 </div>;
});

const show = createComponentImplementation(chatShow, ({props, buildChild}) =>
 props.when ? <>{buildChild(props.child)}</> : null);

const table = createComponentImplementation(chatTable, ({props}) =>
 <div className="message-table chat-a2ui-table" tabIndex={0}><table>
  <thead><tr>{props.headers.map((header, index) => <th key={index} scope="col">{header}</th>)}</tr></thead>
  <tbody>{(Array.isArray(props.rows) ? props.rows : []).map((row, index) => Array.isArray(row) &&
   <tr key={index}>{props.headers.map((_, column) => <td key={column}>{fieldText(row[column])}</td>)}</tr>)}</tbody>
 </table></div>);

const tabs = createComponentImplementation(TabsApi, ({props, buildChild}) => {
 const id = useId();
 const [active, setActive] = useState(0);
 const selected = Math.min(active, props.tabs.length - 1);
 return <div className="chat-a2ui-tabs" style={{flex: props.weight}}>
  <div role="tablist" onKeyDown={event => {
   if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
   event.preventDefault();
   const buttons = Array.from(event.currentTarget.querySelectorAll('button'));
   const index = buttons.indexOf(event.target as HTMLButtonElement);
   const backwards = (event.key === 'ArrowLeft') !== (getComputedStyle(event.currentTarget).direction === 'rtl');
   const next = event.key === 'Home' ? 0 : event.key === 'End' ? buttons.length - 1 :
    (index + (backwards ? -1 : 1) + buttons.length) % buttons.length;
   buttons[next]?.focus();
   setActive(next);
  }}>
   {props.tabs.map(({title}, index) => <button key={index} type="button" role="tab" id={`${id}-tab-${index}`}
    aria-selected={selected === index} tabIndex={selected === index ? 0 : -1} aria-controls={`${id}-panel-${index}`} onClick={() => setActive(index)}>{title}</button>)}
  </div>
  {props.tabs.map(({child}, index) => <div key={index} role="tabpanel" id={`${id}-panel-${index}`}
   aria-labelledby={`${id}-tab-${index}`} hidden={selected !== index} tabIndex={selected === index ? 0 : undefined}>
   {selected === index && buildChild(child)}
  </div>)}
 </div>;
});

function useField(label: string, value: string, invalid: boolean) {
 const id = useId();
 const answer = useAnswer(label, value, invalid);
 const [touched, setTouched] = useState(false);
 const showInvalid = invalid && (!answer.inForm || answer.checked || touched);
 return {
  id, disabled: answer.frozen, showInvalid,
  field: {ref: answer.root, 'data-a2ui-answer': '', className: 'chat-a2ui-field', onBlur: (event: React.FocusEvent<HTMLDivElement>) => {
   if (!event.currentTarget.contains(event.relatedTarget)) setTouched(true);
  }},
  aria: {'aria-invalid': showInvalid || undefined, 'aria-describedby': showInvalid ? `${id}-error` : undefined},
 };
}

function FieldError({id, messages}: {id: string; messages?: string[]}) {
 const {t} = useI18n();
 return <div className="a2ui-error-message" id={`${id}-error`}>
  <span aria-hidden="true">!</span><div>{(messages?.length ? messages : [t('chat.a2uiInvalid')]).map((message, index) => <div key={index}>{message}</div>)}</div>
 </div>;
}

const textField = createComponentImplementation(TextFieldApi, ({props}) => {
 const value = props.value ?? '';
 const field = useField(props.label, value, props.isValid === false);
 const shared = {id: field.id, className: 'chat-a2ui-input', placeholder: props.placeholder, value, readOnly: !props.setValue,
  disabled: field.disabled, maxLength: 4096, ...field.aria};
 return <div {...field.field} style={{flex: props.weight}}>
  <label className="chat-a2ui-label" htmlFor={field.id}>{props.label}</label>
  {props.variant === 'longText'
   ? <textarea {...shared} rows={3} onChange={event => props.setValue?.(event.currentTarget.value)}/>
   : <input {...shared} type={props.variant === 'obscured' ? 'password' : 'text'}
    inputMode={props.variant === 'number' ? 'decimal' : undefined} onChange={event => props.setValue?.(event.currentTarget.value)}/>}
  {field.showInvalid && <FieldError id={field.id} messages={props.validationErrors}/>}
 </div>;
});

const checkBox = createComponentImplementation(CheckBoxApi, ({props}) => {
 const {t} = useI18n();
 const field = useField(props.label, t(props.value ? 'chat.a2uiYes' : 'chat.a2uiNo'), props.isValid === false);
 return <div {...field.field} style={{flex: props.weight}}>
  <label className="chat-a2ui-option">
   <input type="checkbox" checked={!!props.value} disabled={field.disabled || !props.setValue} {...field.aria}
    onChange={event => props.setValue?.(event.currentTarget.checked)}/>
   <span>{props.label}</span>
  </label>
  {field.showInvalid && <FieldError id={field.id} messages={props.validationErrors}/>}
 </div>;
});

const choice = createComponentImplementation(ChoicePickerApi, ({props}) => {
 const {t} = useI18n();
 const [filter, setFilter] = useState('');
 const values = Array.isArray(props.value) ? props.value : typeof props.value === 'string' ? [props.value] : [];
 const multi = props.variant === 'multipleSelection';
 const selected = multi ? values : values.slice(0, 1);
 const label = props.label || t('chat.a2uiOptions');
 const answer = props.options.filter(option => selected.includes(option.value)).map(option => option.label).join('\n');
 const field = useField(label, answer, props.isValid === false);
 const options = props.options.filter(option => !props.filterable || option.label.toLocaleLowerCase().includes(filter.toLocaleLowerCase()));
 return <div {...field.field} style={{flex: props.weight}}>
  <span className="chat-a2ui-label" id={field.id}>{label}</span>
  {props.filterable && <input className="chat-a2ui-input" type="search" aria-label={t('chat.a2uiFilter')} placeholder={t('chat.a2uiFilter')}
   value={filter} disabled={field.disabled} onChange={event => setFilter(event.currentTarget.value)}/>}
  <div role={multi ? 'group' : 'radiogroup'} aria-labelledby={field.id} {...field.aria}
   className={`chat-a2ui-options${props.displayStyle === 'chips' ? ' inline' : ''}`}>
   {options.map(({label, value}, index) => <label className="chat-a2ui-option" key={`${value}:${index}`}>
    <input type={multi ? 'checkbox' : 'radio'} name={multi ? undefined : field.id} checked={selected.includes(value)}
     disabled={field.disabled || !props.setValue} onChange={event => props.setValue?.(multi
      ? event.currentTarget.checked ? [...selected, value] : selected.filter(item => item !== value)
      : [value])}/>
    <span>{label}</span>
   </label>)}
  </div>
  {field.showInvalid && <FieldError id={field.id} messages={props.validationErrors}/>}
 </div>;
});

const slider = createComponentImplementation(SliderApi, ({props}) => {
 const {t} = useI18n();
 const label = props.label || t('chat.a2uiValue');
 const min = props.min ?? 0;
 const max = Math.max(props.max, min);
 const raw = Math.min(max, Math.max(min, props.value ?? min));
 const step = props.steps && max > min ? (max - min) / props.steps : undefined;
 const value = step ? Number((min + Math.round((raw - min) / step) * step).toPrecision(12)) : raw;
 const field = useField(label, String(value), props.isValid === false);
 const fill = max > min ? ((value - min) / (max - min)) * 100 : 0;
 return <div {...field.field} style={{flex: props.weight}}>
  <label className="chat-a2ui-label" htmlFor={field.id}>{label}</label>
  <div className="chat-a2ui-range">
   <input type="range" id={field.id} min={min} max={max} step={step ?? 'any'} value={value}
    disabled={field.disabled || !props.setValue || max === min} {...field.aria} style={{'--range-fill': `${fill}%`} as CSSProperties}
    onChange={event => props.setValue?.(event.currentTarget.valueAsNumber)}/>
   <span className="a2ui-slider-value" aria-hidden="true">{value}</span>
  </div>
  {field.showInvalid && <FieldError id={field.id} messages={props.validationErrors}/>}
 </div>;
});

export const chatControls = [text, row, column, list, card, divider, tabs, textField, checkBox, choice, slider, form, button, show, table];
