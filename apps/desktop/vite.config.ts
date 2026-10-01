import { resolve } from 'node:path';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';
import { electrobunViteAliases } from './.hutch/devkit/api/config/electrobun-vite';

export default defineConfig({
  plugins: [tailwindcss(), svelte()],
  resolve: {
    alias: electrobunViteAliases(resolve(__dirname, '.hutch/devkit')),
  },
  root: 'src/mainview',
  build: {
    outDir: '../../dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    strictPort: true,
  },
});
