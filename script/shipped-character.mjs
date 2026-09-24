import assert from 'node:assert/strict';
import {readManifest} from './asset-pack.mjs';

const pack = readManifest();
const character = pack.characters.find(value => value.id === pack.defaultCharacter);
const variant = character?.variants.find(value => value.id === pack.defaultVariant);
assert.ok(variant, 'the default character and outfit must resolve in the shipped manifest');
export const defaultModel = variant.model;
