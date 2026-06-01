import { defineConfig } from 'vite'

// Vite config for building iframe-adapter as an ES module.
// This is used by the <script type="module"> bootstrap in iframe-host.
export default defineConfig({
  define: {
    'process.env.NODE_ENV': JSON.stringify('production'),
  },
  build: {
    lib: {
      entry: 'src/plugin-sdk/iframe-adapter-esm.ts',
      formats: ['es'],
      fileName: () => 'iframe-adapter.esm.js',
    },
    outDir: '../internal/api/static/dist/plugin-sdk',
    emptyOutDir: false,
  },
})