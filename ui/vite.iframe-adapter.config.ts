import { defineConfig } from 'vite'

// Vite config for building iframe-adapter as a UMD library.
// This produces iframe-adapter.umd.js that mounts window.AxonsPluginIframe,
// so plugins can use IframePluginApiAdapter without bundling it.
export default defineConfig({
  define: {
    'process.env.NODE_ENV': JSON.stringify('production'),
  },
  build: {
    lib: {
      entry: 'src/plugin-sdk/iframe-adapter.ts',
      name: 'AxonsPluginIframe',
      formats: ['umd'],
      fileName: () => 'iframe-adapter.umd.js',
    },
    outDir: '../internal/api/static/dist/plugin-sdk',
    emptyOutDir: false,
  },
})