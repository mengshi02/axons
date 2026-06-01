import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
// Vite config for building axons-plugin-ui as a UMD library.
// This produces axons-plugin-ui.umd.js that mounts window.AxonsPluginUI,
// so plugins can `import { Button } from 'axons-plugin-ui'` without bundling it.
export default defineConfig({
  plugins: [react()],
  define: {
    'process.env.NODE_ENV': JSON.stringify('production'),
  },
  build: {
    lib: {
      entry: 'src/plugin-sdk/index.tsx',
      name: 'AxonsPluginUI',
      formats: ['umd'],
      fileName: () => 'axons-plugin-ui.umd.js',
    },
    outDir: '../internal/api/static/dist/plugin-sdk',
    emptyOutDir: false,
    rollupOptions: {
      external: ['react', 'react-dom'],
      output: {
        globals: {
          react: 'React',
          'react-dom': 'ReactDOM',
        },
      },
    },
  },
})