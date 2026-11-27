import {createFunctionImplementation} from '@a2ui/web_core';
import {DynamicValueSchema} from '@a2ui/web_core/v1_0';
import {z} from 'zod';

// A temporarily empty or invalid input is data the user can correct.
function number(value: unknown): number | null {
 if (typeof value === 'string' && /^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:e[+-]?\d+)?$/i.test(value.trim())) value = Number(value);
 return typeof value === 'number' && Number.isFinite(value) ? value : null;
}

const value = DynamicValueSchema.nullish().describe('REF:#/$defs/DynamicValue');
const arithmetic = createFunctionImplementation({
 name: 'calculate', returnType: 'any',
 description: 'Perform arithmetic on numbers or decimal text. An invalid operand, overflow, or division by zero produces null.',
 schema: z.object({op: z.enum(['+', '-', '*', '/']).or(value), left: value, right: value}).strict(),
}, ({op, left, right}) => {
 const a = number(left), b = number(right);
 if (a === null || b === null) return null;
 switch (op) {
  case '+': return number(a + b);
  case '-': return number(a - b);
  case '*': return number(a * b);
  case '/': return b === 0 ? null : number(a / b);
  default: return null;
 }
});

const round = createFunctionImplementation({
 name: 'round', returnType: 'any',
 description: 'Round to an integer, floor or ceil. Returns null for invalid input.',
 schema: z.object({value, mode: z.enum(['nearest', 'floor', 'ceil']).optional()}).strict(),
}, ({value, mode}) => {
 const n = number(value);
 return n === null ? null : (mode === 'floor' ? Math.floor : mode === 'ceil' ? Math.ceil : Math.round)(n);
});

const compare = createFunctionImplementation({
 name: 'compare', returnType: 'boolean',
 description: 'Compare scalars with eq/ne or numbers (including decimal text) with lt/le/gt/ge. Null equals null.',
 schema: z.object({op: z.enum(['eq', 'ne', 'lt', 'le', 'gt', 'ge']), left: value, right: value}).strict(),
}, ({op, left, right}) => {
 if (op === 'eq' || op === 'ne') return (left === right) === (op === 'eq');
 const a = number(left), b = number(right);
 if (a === null || b === null) return false;
 switch (op) {
  case 'lt': return a < b;
  case 'le': return a <= b;
  case 'gt': return a > b;
  case 'ge': return a >= b;
 }
});

const sameSet = createFunctionImplementation({
 name: 'sameSet', returnType: 'boolean',
 description: 'Compare two lists of scalar values, ignoring order and duplicates.',
 schema: z.object({left: value, right: value}).strict(),
}, ({left, right}) => {
 if (!Array.isArray(left) || !Array.isArray(right) || !left.every(scalar) || !right.every(scalar)) return false;
 const a = new Set(left), b = new Set(right);
 return a.size === b.size && [...a].every(item => b.has(item));
});

function scalar(value: unknown): value is string | number | boolean {
 return typeof value === 'string' || typeof value === 'boolean' || typeof value === 'number' && Number.isFinite(value);
}

const count = createFunctionImplementation({
 name: 'count', returnType: 'number',
 description: 'Count the items in a list. A missing list counts as zero.',
 schema: z.object({values: value}).strict(),
}, ({values}) => Array.isArray(values) ? values.length : 0);

const sum = createFunctionImplementation({
 name: 'sum', returnType: 'any',
 description: 'Add the numbers, decimal text, or booleans (true=1) in a list. Invalid items produce null.',
 schema: z.object({values: value}).strict(),
}, ({values}) => {
 if (!Array.isArray(values)) return null;
 let total = 0;
 for (const item of values) {
  const n = typeof item === 'boolean' ? Number(item) : number(item);
  if (n === null) return null;
  total += n;
 }
 return number(total);
});

const choose = createFunctionImplementation({
 name: 'choose', returnType: 'any',
 description: 'Return yes when the condition is true, otherwise no.',
 schema: z.object({when: value, yes: value, no: value}).strict(),
}, ({when, yes, no}) => when === true ? yes : no);

const display = createFunctionImplementation({
 name: 'display', returnType: 'string',
 description: 'Display a scalar or list. For an invalid calculation, display the supplied fallback. Optional fixed decimal precision applies to numbers.',
 schema: z.object({value, fallback: z.string(), decimals: z.number().int().min(0).max(8).optional()}).strict(),
}, ({value, fallback, decimals}) => {
 if (typeof value === 'number' && Number.isFinite(value) && decimals !== undefined) return value.toFixed(decimals);
 return fieldText(value) ?? fallback;
});

const numberInRange = createFunctionImplementation({
 name: 'numberInRange', returnType: 'boolean',
 description: 'Check numeric text or a number against inclusive bounds. Use for TextField number checks.',
 schema: z.object({value, min: z.number(), max: z.number().optional()}).strict(),
}, ({value, min, max}) => {
 const n = number(value);
 return n !== null && n >= min && (max === undefined || n <= max);
});

const present = createFunctionImplementation({
 name: 'present', returnType: 'boolean',
 description: 'Check for nonblank text, a finite number, a boolean, or a nonempty list of these values.',
 schema: z.object({value}).strict(),
}, ({value}) => !!fieldText(value));

export function fieldText(value: unknown): string | undefined {
 if (scalar(value)) return String(value).trim();
 if (Array.isArray(value) && value.length <= 12 && value.every(scalar)) return value.map(item => String(item).trim()).join('、');
}

export const chatFunctions = [arithmetic, round, compare, sameSet, count, sum, choose, display, present, numberInRange];
