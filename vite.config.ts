import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { cpSync, mkdirSync } from 'node:fs';
import { resolve } from 'node:path';

import pack from './resources/character-pack.json' with { type: 'json' };
const runtimeFiles = pack.files.filter(f => f.path.startsWith('frontend/public/models/')||f.path.startsWith('frontend/public/portraits/')).map(f => f.path.replace('frontend/public/',''));

export default defineConfig({
  root: 'frontend',
  plugins: [react(), {
    name: 'current-character-resources',
    apply: 'build',
    closeBundle() {
      const output = resolve('frontend/dist');
      mkdirSync(resolve(output, 'models'), { recursive: true });
      cpSync(resolve('frontend/public/icons'), resolve(output, 'icons'), { recursive: true });
      for (const file of runtimeFiles) {
        mkdirSync(resolve(output,file,'..'),{recursive:true});
        cpSync(resolve('frontend/public', file), resolve(output, file));
      }
    },
  }],
  base: './',
  server: { host: '127.0.0.1', port: 9246, strictPort: true },
  preview: { host: '127.0.0.1', port: 9246, strictPort: true },
  build: { outDir: 'dist', emptyOutDir: true, copyPublicDir: false },
});
