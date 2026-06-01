// ESM shim: re-export react/jsx-runtime from the host app's React global.
// React 19 exposes jsx/jsxs on the main react object.
const R = window.React;
export const jsx = R.jsx;
export const jsxs = R.jsxs;
export const Fragment = R.Fragment;