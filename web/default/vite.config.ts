import { readFileSync } from 'node:fs';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const pkg = JSON.parse(
  readFileSync(new URL('./package.json', import.meta.url), 'utf-8')
) as { version: string };

export default defineConfig({
    plugins: [react()],
    envPrefix: ['VITE_', 'REACT_APP_'],
    define: {
        // REACT_APP_VERSION is only inlined when the build environment
        // exports it, which never happens here: the version lives in
        // package.json. Expose it with the v-prefix the server version and
        // release tags use so the frontends' comparisons are meaningful.
        'import.meta.env.REACT_APP_VERSION': JSON.stringify(`v${pkg.version}`),
    },
    build: { outDir: '../build/default', emptyOutDir: true, cssMinify: false },
    server: { proxy: { '/api': 'http://localhost:3000', '/v1': 'http://localhost:3000' } },
});
