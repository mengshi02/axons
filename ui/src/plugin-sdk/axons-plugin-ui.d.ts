/**
 * axons-plugin-ui type declarations
 *
 * HOST MAINTAINERS: This is the single source of truth for plugin UI types.
 * Keep it in sync with index.tsx — every new component/prop must be reflected here.
 *
 * PLUGIN DEVELOPERS: You do NOT need to maintain this file.
 * - Compile-time only: never ships in your plugin bundle.
 * - Inside axons repo: use tsconfig paths to reference this file.
 * - Outside axons repo: copy this file from the axons repository into your plugin project, re-copy when host updates.
 * - At runtime: axons-plugin-ui is provided by the host iframe (UMD + ESM shim).
 */
declare module 'axons-plugin-ui' {
    import { ButtonHTMLAttributes, InputHTMLAttributes, SelectHTMLAttributes, TextareaHTMLAttributes, ReactNode } from 'react';

    // ═══ Button ═══
    export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
        variant?: 'primary' | 'secondary' | 'ghost';
        size?: 'default' | 'sm';
    }
    export const Button: React.ForwardRefExoticComponent<ButtonProps & React.RefAttributes<HTMLButtonElement>>;

    // ═══ Card ═══
    export interface CardProps { children: ReactNode; className?: string; }
    export function Card(props: CardProps): JSX.Element;
    export function CardHeader(props: { children: ReactNode; className?: string }): JSX.Element;
    export function CardBody(props: { children: ReactNode; className?: string }): JSX.Element;

    // ═══ Input ═══
    export interface InputProps extends InputHTMLAttributes<HTMLInputElement> { }
    export function Input(props: InputProps): JSX.Element;

    // ═══ Select ═══
    export interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> { }
    export function Select(props: SelectProps): JSX.Element;

    // ═══ Textarea ═══
    export interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> { }
    export function Textarea(props: TextareaProps): JSX.Element;

    // ═══ Badge ═══
    export interface BadgeProps { variant?: 'default' | 'success' | 'warning' | 'error' | 'info'; children: ReactNode; className?: string; }
    export function Badge(props: BadgeProps): JSX.Element;

    // ═══ Divider ═══
    export interface DividerProps { spacing?: 'default' | 'lg'; className?: string; }
    export function Divider(props: DividerProps): JSX.Element;

    // ═══ EmptyState ═══
    export interface EmptyStateProps { icon?: ReactNode; title?: string; description?: string; children?: ReactNode; className?: string; }
    export function EmptyState(props: EmptyStateProps): JSX.Element;

    // ═══ Spinner ═══
    export interface SpinnerProps { size?: 'sm' | 'md' | 'lg'; className?: string; }
    export function Spinner(props: SpinnerProps): JSX.Element;

    // ═══ ProgressBar ═══
    export interface ProgressBarProps { value: number; variant?: 'default' | 'success' | 'warning' | 'error'; className?: string; }
    export function ProgressBar(props: ProgressBarProps): JSX.Element;

    // ═══ List ═══
    export interface ListProps { children: ReactNode; className?: string; }
    export function List(props: ListProps): JSX.Element;
    export interface ListItemProps { icon?: ReactNode; active?: boolean; children: ReactNode; className?: string; onClick?: () => void; }
    export function ListItem(props: ListItemProps): JSX.Element;

    // ═══ Tabs ═══
    export interface TabsProps { tabs: Array<{ id: string; label: string }>; activeTab: string; onChange: (id: string) => void; className?: string; }
    export function Tabs(props: TabsProps): JSX.Element;

    // ═══ ConfirmDialog ═══
    export interface ConfirmDialogProps { isOpen: boolean; title: string; message: string; confirmLabel?: string; cancelLabel?: string; variant?: 'default' | 'danger' | 'warning'; onConfirm: () => void; onCancel: () => void; }
    export function ConfirmDialog(props: ConfirmDialogProps): JSX.Element;

    // ═══ Modal ═══
    export interface ModalProps { isOpen: boolean; onClose: () => void; children: ReactNode; closeOnOverlayClick?: boolean; closeOnEscape?: boolean; size?: 'sm' | 'md' | 'lg' | 'xl' | 'full'; overlayOpacity?: 'none' | 'light' | 'medium' | 'dark' | 'darker'; backdropBlur?: boolean; className?: string; }
    export function Modal(props: ModalProps): JSX.Element;
}