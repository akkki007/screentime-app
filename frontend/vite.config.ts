import { writeFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

// Built into dist/, which cmd/screentime embeds (frontend/embed.go). Vite empties
// dist/ first, so put back the tracked placeholder that keeps `go vet` working
// on a checkout that has not built the UI.
const keepPlaceholder = {
  name: 'keep-dist-placeholder',
  closeBundle() {
    writeFileSync(resolve(__dirname, 'dist', '.gitkeep'), '');
  },
};

export default defineConfig({
  plugins: [tailwindcss(), svelte(), keepPlaceholder],
  build: { outDir: 'dist', emptyOutDir: true },
  server: { port: 5173, strictPort: true },
});
