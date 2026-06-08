import { createRoot } from 'react-dom/client';
import { AppProvider } from './hooks/useAppState';
import { ThemeProvider } from './hooks/useTheme';
import App from './App';
import { initConfig } from './lib/config';
import './i18n'; // Initialize i18next (must be imported before React renders)
import './index.css';
import './plugin-sdk/theme.css';
import 'devicon/devicon.min.css';

// Plugin SDK: expose React as global so axons-plugin-ui.umd.js can reference it,
// then dynamically load the UMD bundle which mounts window.AxonsPluginUI.
// Plugins can then `import { Button } from 'axons-plugin-ui'` without bundling it.
import * as React from 'react';
import * as ReactDOM from 'react-dom';
(window as any).React = React;
(window as any).ReactDOM = ReactDOM;

const pluginSdkScript = document.createElement('script');
pluginSdkScript.src = '/plugin-sdk/axons-plugin-ui.umd.js';
pluginSdkScript.async = false;
document.head.appendChild(pluginSdkScript);

// Note: StrictMode is disabled to prevent double-rendering issues with Sigma.js
// Sigma.js manipulates DOM directly and conflicts with React's StrictMode behavior

// Initialize runtime config BEFORE rendering the React app.
// In desktop mode, this fetches the daemon's localhost address so that
// API requests go to the Go daemon. Without this await, React renders and fires
// API calls before baseURL is configured, causing all /v1/ and /api/ requests to
// fail — resulting in an empty project list and the DropZone being shown.
initConfig().then(() => {
  // Fetch locale→plugin mapping for i18next http-backend.
  // This must happen before React renders so that changeLanguage()
  // can resolve plugin IDs for non-English locales.
  // Timeout: 2s — on failure, i18next falls back to embedded English.
  const localeMappingPromise = fetch('/v1/plugins/locales')
    .then(r => r.ok ? r.json() : { locales: {} })
    .then(data => {
      (window as any).__localePluginMap = Object.fromEntries(
        Object.entries(data.locales || {}).map(([code, info]: [string, any]) => [code, info.pluginId])
      );

      // i18next initializes before this mapping is available (import './i18n' runs
      // synchronously at module load). If the language detector restored a non-English
      // locale from localStorage, i18next's http-backend would have returned '' for
      // loadPath (no mapping), causing it to store an empty bundle (callback(null, {})).
      // Even after the mapping is set, changeLanguage won't reload because
      // hasResourceBundle returns true for the empty bundle. Use switchLocale
      // to remove stale empty bundles and re-trigger changeLanguage.
      import('./i18n').then(({ switchLocale, default: i18n }) => {
        const lng = typeof i18n.language === 'string' ? i18n.language : i18n.language?.[0];
        if (lng && lng !== 'en' && !lng.startsWith('en')) {
          switchLocale(lng);
        }
      });
    })
    .catch(() => {
      // Network error — i18next loadPath returns '' for missing mapping, fallback to en
    });

  // Race with 2s timeout
  const timeout = new Promise<void>(resolve => setTimeout(resolve, 2000));
  Promise.race([localeMappingPromise, timeout]).then(() => {
    createRoot(document.getElementById('root')!).render(
      <ThemeProvider>
        <AppProvider>
          <App />
        </AppProvider>
      </ThemeProvider>
    );
  });
});