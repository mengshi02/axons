import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Vite config for building React + ReactDOM as IIFE bundles for plugin iframes.
// These set window.React and window.ReactDOM globals, which the ESM shims read from.
export default defineConfig({
  plugins: [react()],
  define: {
    'process.env.NODE_ENV': JSON.stringify('production'),
  },
  build: {
    lib: {
      entry: 'src/plugin-sdk/react-umd-entry.ts',
      name: 'ReactUMD',
      formats: ['iife'],
      fileName: () => 'react.umd.js',
    },
    outDir: '../internal/api/static/dist/plugin-sdk/vendor',
    emptyOutDir: false,
    rollupOptions: {
      output: {
        globals: {
          react: 'React',
          'react-dom': 'ReactDOM',
        },
      },
    },
  },
})