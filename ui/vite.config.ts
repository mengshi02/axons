import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { resolve } from 'path'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: '../internal/api/static/dist',
    emptyOutDir: true,
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
      },
      output: {
        manualChunks(id) {
          // Sigma graph visualization — only needed when graph is displayed
          if (id.includes('node_modules/sigma/') || id.includes('node_modules/@sigma/')) {
            return 'vendor-sigma';
          }
          if (id.includes('node_modules/graphology')) {
            return 'vendor-sigma';
          }
          // CodeMirror editor — only needed when code panel is open
          if (id.includes('node_modules/@uiw/react-codemirror') ||
            id.includes('node_modules/@codemirror/') ||
            id.includes('node_modules/@replit/codemirror-vim') ||
            id.includes('node_modules/codemirror') ||
            id.includes('node_modules/@lezer/')) {
            return 'vendor-codemirror';
          }
          // xterm terminal — only needed when terminal panel is open
          if (id.includes('node_modules/@xterm/')) {
            return 'vendor-xterm';
          }
          // Markdown rendering — only needed in chat/references
          // NOTE: prismjs is intentionally excluded from this chunk because its
          // side-effect grammar imports (prismjs/components/prism-*) depend on
          // the Prism global being evaluated first. Splitting them breaks load order.
          if (id.includes('node_modules/react-markdown/') ||
            id.includes('node_modules/react-syntax-highlighter/') ||
            id.includes('node_modules/remark-gfm/') ||
            id.includes('node_modules/unified/') ||
            id.includes('node_modules/rehype-') ||
            id.includes('node_modules/mdast-') ||
            id.includes('node_modules/micromark') ||
            id.includes('node_modules/vfile')) {
            return 'vendor-markdown';
          }
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/v1': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/plugins': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})