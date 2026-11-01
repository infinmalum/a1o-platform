import { readFileSync } from 'node:fs';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';
import path from 'node:path';

const pkg = JSON.parse(
  readFileSync(new URL('./package.json', import.meta.url), 'utf-8')
) as { version: string };

export default defineConfig({
    plugins: [react(), tailwindcss()],
    envPrefix: ['VITE_', 'REACT_APP_'],
    define: {
        // Same convention as the admin console: expose the package version
        // with the v-prefix so the server version comparison is meaningful.
        'import.meta.env.REACT_APP_VERSION': JSON.stringify(`v${pkg.version}`),
    },
    resolve: {
        alias: { '@': path.resolve(__dirname, './src') },
    },
    build: { outDir: '../build/studio', emptyOutDir: true, cssMinify: false },
    // Dev layout (docs/studio-transformation-proposal.md): studio on 3000,
    // admin console on 3001, Go backend on 3002.
    server: { port: 3000, proxy: { '/api': 'http://localhost:3002', '/v1': 'http://localhost:3002' } },
});
