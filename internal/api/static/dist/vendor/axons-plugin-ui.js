// ESM shim: re-export axons-plugin-ui from the host app's window global.
// The host app (main.tsx) loads axons-plugin-ui.umd.js which mounts window.AxonsPluginUI.
const UI = window.AxonsPluginUI;
export const Button = UI.Button;
export const Card = UI.Card;
export const CardHeader = UI.CardHeader;
export const CardBody = UI.CardBody;
export const Input = UI.Input;
export const Select = UI.Select;
export const Badge = UI.Badge;
export const Spinner = UI.Spinner;
export const ProgressBar = UI.ProgressBar;
export const Tabs = UI.Tabs;