// ESM shim: re-export ReactDOM from the host app's window global.
const D = window.ReactDOM;
export default D;
export const createRoot = D.createRoot;
export const hydrateRoot = D.hydrateRoot;