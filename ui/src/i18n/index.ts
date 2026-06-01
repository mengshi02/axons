import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import HttpBackend from 'i18next-http-backend';
import LanguageDetector from 'i18next-browser-languagedetector';

// Embedded English resources (offline-capable, zero-latency)
import common from './en/common.json';
import settings from './en/settings.json';
import panels from './en/panels.json';
import chat from './en/chat.json';
import activitybar from './en/activitybar.json';
import dropzone from './en/dropzone.json';
import extensions from './en/extensions.json';
import notifications from './en/notifications.json';

const enResources = {
  common, settings, panels, chat, activitybar, dropzone, extensions, notifications,
};

i18n
  .use(HttpBackend)
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: {
      en: enResources,
    },
    partialBundledLanguages: true, // en is embedded; other locales loaded via http-backend
    fallbackLng: 'en',
    ns: ['common', 'settings', 'panels', 'chat', 'activitybar', 'dropzone', 'extensions', 'notifications'],
    defaultNS: 'common',
    interpolation: { escapeValue: false }, // React already escapes
    backend: {
      // Language plugin resource path (dynamic function)
      // en is embedded, no network request; other languages served by daemon static route
      loadPath: (lngs: string[], _namespaces: string[]) => {
        const lng = lngs[0];
        if (lng === 'en') return ''; // en embedded, skip loading

        // Look up plugin ID for this locale
        // Mapping source: GET /v1/plugins/locales (provided by locale plugin scheme)
        const localePluginMap = (window as any).__localePluginMap as Record<string, string> || {};
        const pluginId = localePluginMap[lng];
        if (!pluginId) {
          console.warn(`[i18n/loadPath] No pluginId mapping for "${lng}" in __localePluginMap:`, JSON.stringify(localePluginMap));
          return ''; // locale not installed
        }

        // Daemon serves static files for non-running plugins via HandlePluginStaticFiles
        return `/plugins/${pluginId}/locales/frontend/{{ns}}.json`;
      },
    },
    detection: {
      order: ['localStorage', 'navigator'],
      lookupLocalStorage: 'axons-locale',
      caches: ['localStorage'],
    },
  });

/** Switch to a locale, removing any stale/empty resource bundles first.
 *
 * When `loadPath` returns '' (missing locale→plugin mapping), i18next-http-backend
 * calls `callback(null, {})`, storing an empty object as the bundle for that
 * namespace.  After the mapping becomes available, `changeLanguage` won't
 * re-fetch because `hasResourceBundle` returns true for the empty bundle.
 * This function removes those empty bundles before calling `changeLanguage`,
 * forcing the http-backend to fetch the real translations.
 *
 * CRITICAL: i18next's changeLanguage() checks hasLanguageSomeTranslations()
 * BEFORE loading resources. If the target language has no non-empty bundles
 * (e.g., all were empty from a previous failed loadPath), it falls back to
 * fallbackLng ('en') and never loads the target language at all.
 *
 * To break this cycle, we must load resources BEFORE calling changeLanguage,
 * using reloadResources() which bypasses the bundle-exists check.
 */
export async function switchLocale(locale: string): Promise<void> {
  console.log(`[i18n/switchLocale] CALLED with locale="${locale}", current i18n.language="${i18n.language}"`);
  // IMPORTANT: snapshot i18n.options.ns into a NEW array.
  // i18n.removeResourceBundle() internally calls store.removeNamespaces(ns),
  // which does `this.options.ns.splice(index, 1)` — mutating the same array
  // we'd be iterating. Sharing the reference caused for-of to skip every other
  // element, leaving most namespaces uncleaned and excluded from the subsequent
  // reloadResources() call. Always work on a stable copy.
  const namespaces = [...((i18n.options.ns as string[] | undefined) || ['common'])];

  // Remove stale empty bundles so reloadResources is forced to re-fetch.
  // Re-add the namespace to i18n.options.ns immediately, because
  // removeResourceBundle() also unregisters it from the namespace list and
  // subsequent reloadResources would otherwise ignore it.
  for (const ns of namespaces) {
    const bundle = i18n.getResourceBundle(locale, ns);
    if (bundle && Object.keys(bundle).length === 0) {
      i18n.removeResourceBundle(locale, ns);
      // removeResourceBundle -> store.removeNamespaces splices ns out of
      // i18n.options.ns. Restore it so reloadResources/loadResources keeps
      // treating it as a known namespace.
      if (!(i18n.options.ns as string[]).includes(ns)) {
        (i18n.options.ns as string[]).push(ns);
      }
    }
  }

  // For non-English locales, ensure the locale→plugin mapping is available
  // before attempting to load resources. If the mapping is missing (e.g.,
  // SSE event hasn't arrived yet), refresh it from the API.
  if (locale !== 'en') {
    const localePluginMap = (window as any).__localePluginMap as Record<string, string> || {};
    console.log(`[i18n/switchLocale] __localePluginMap =`, JSON.stringify(localePluginMap));
    if (!localePluginMap[locale]) {
      console.info(`[i18n/switchLocale] No pluginId for "${locale}" — refreshing mapping from API`);
      try {
        const resp = await fetch('/v1/plugins/locales');
        if (resp.ok) {
          const data = await resp.json();
          (window as any).__localePluginMap = Object.fromEntries(
            Object.entries(data.locales || {}).map(([code, info]: [string, any]) => [code, info.pluginId])
          );
        }
      } catch {
        // Ignore — proceed with current mapping
      }
    }

    // Reset failed load states for this locale's namespaces.
    // i18next's backendConnector tracks each namespace's load state:
    //   -1 = failed, 0 = pending, 1 = loading, 2 = loaded
    // When a previous load attempt failed (e.g., loadPath returned empty
    // or the HTTP request got 404), state is set to -1. Subsequent calls
    // to reloadResources() skip namespaces with state < 0, so they are
    // never retried. Resetting to 0 allows reloadResources to retry them.
    const backendState = (i18n as any).services?.backendConnector?.state;
    if (backendState) {
      console.log(`[i18n/switchLocale] backendConnector.state for "${locale}":`, namespaces.map(ns => `${ns}=${backendState[locale + '|' + ns]}`).join(', '));
      for (const ns of namespaces) {
        const key = `${locale}|${ns}`;
        if (backendState[key] === -1) {
          console.log(`[i18n/switchLocale] Resetting failed state: ${key}`);
          backendState[key] = 0;
        }
      }
    } else {
      console.warn(`[i18n/switchLocale] backendConnector.state not found — cannot reset failed states`);
    }

    // Pre-load resources for the target language BEFORE calling changeLanguage.
    // This ensures hasLanguageSomeTranslations() returns true, preventing
    // changeLanguage from falling back to the fallback language.
    try {
      console.log(`[i18n/switchLocale] Calling reloadResources(["${locale}"], [${namespaces.join(', ')}])`);
      await i18n.reloadResources([locale], namespaces);
      console.log(`[i18n/switchLocale] reloadResources completed. Checking bundles...`);
      for (const ns of namespaces) {
        const bundle = i18n.getResourceBundle(locale, ns);
        console.log(`  ${locale}|${ns}: ${bundle ? (Object.keys(bundle).length > 0 ? 'LOADED (' + Object.keys(bundle).length + ' keys)' : 'EMPTY') : 'NULL'}`);
      }
    } catch (e) {
      console.warn(`[i18n/switchLocale] reloadResources failed for "${locale}":`, e);
    }
  }

  // Now safe to call changeLanguage — bundles are loaded, no fallback
  console.log(`[i18n/switchLocale] Calling changeLanguage("${locale}")`);
  i18n.changeLanguage(locale);
}

export default i18n;