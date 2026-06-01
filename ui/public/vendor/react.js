// ESM shim: re-export React from the host app's window global.
// The host app (main.tsx) sets window.React before any plugin is loaded.
// This ensures plugins share the exact same React instance as the host.
const R = window.React;
export default R;
export const useState = R.useState;
export const useEffect = R.useEffect;
export const useCallback = R.useCallback;
export const useMemo = R.useMemo;
export const useRef = R.useRef;
export const useReducer = R.useReducer;
export const useContext = R.useContext;
export const useLayoutEffect = R.useLayoutEffect;
export const useInsertionEffect = R.useInsertionEffect;
export const useImperativeHandle = R.useImperativeHandle;
export const useDebugValue = R.useDebugValue;
export const useDeferredValue = R.useDeferredValue;
export const useTransition = R.useTransition;
export const useId = R.useId;
export const useSyncExternalStore = R.useSyncExternalStore;
export const useOptimistic = R.useOptimistic;
export const useActionState = R.useActionState;
export const use = R.use;
export const createElement = R.createElement;
export const Fragment = R.Fragment;
export const Component = R.Component;
export const PureComponent = R.PureComponent;
export const memo = R.memo;
export const forwardRef = R.forwardRef;
export const createContext = R.createContext;
export const StrictMode = R.StrictMode;
export const Suspense = R.Suspense;
export const Children = R.Children;
export const cloneElement = R.cloneElement;
export const isValidElement = R.isValidElement;
export const lazy = R.lazy;
export const startTransition = R.startTransition;
export const jsx = R.jsx;
export const jsxs = R.jsxs;