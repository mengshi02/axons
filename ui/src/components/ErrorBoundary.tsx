import { Component, type ErrorInfo, type ReactNode } from 'react';

interface Props {
    children: ReactNode;
    fallback?: ReactNode;
}

interface State {
    hasError: boolean;
    error: Error | null;
}

/**
 * Lightweight Error Boundary — prevents a lazy-loaded panel from
 * crashing the entire React tree.  Wrap each Suspense boundary with
 * this so that chunk-load failures or render errors are caught
 * gracefully instead of producing a white screen.
 */
export class ErrorBoundary extends Component<Props, State> {
    constructor(props: Props) {
        super(props);
        this.state = { hasError: false, error: null };
    }

    static getDerivedStateFromError(error: Error): State {
        return { hasError: true, error };
    }

    componentDidCatch(error: Error, info: ErrorInfo) {
        console.error('[ErrorBoundary] Caught error:', error, info.componentStack);
    }

    render() {
        if (this.state.hasError) {
            if (this.props.fallback) return this.props.fallback;
            return (
                <div className="flex items-center justify-center h-full text-text-muted text-sm p-4">
                    <div className="text-center">
                        <p>Component failed to load</p>
                        <button
                            className="mt-2 text-accent hover:underline text-xs"
                            onClick={() => this.setState({ hasError: false, error: null })}
                        >
                            Retry
                        </button>
                    </div>
                </div>
            );
        }
        return this.props.children;
    }
}