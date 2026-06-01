/**
 * React UMD entry — bundles React + ReactDOM into a single IIFE file
 * that sets window.React and window.ReactDOM globals.
 * Used by plugin iframes to provide React runtime for ESM shims.
 */
import * as React from 'react';
import { createRoot } from 'react-dom/client';

// Expose as globals for ESM shims and plugin code
(window as any).React = React;
// Note: react-dom main export does NOT include createRoot (it's in react-dom/client).
// We expose a minimal ReactDOM global with createRoot for the bootstrap script.
(window as any).ReactDOM = { createRoot };