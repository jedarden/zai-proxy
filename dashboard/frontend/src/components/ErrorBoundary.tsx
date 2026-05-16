import { Component, type ReactNode, type ErrorInfo } from 'react';
import { useTheme } from '../contexts/ThemeContext';

interface ErrorBoundaryProps {
  children: ReactNode;
  fallback?: ReactNode;
  onRetry?: () => void;
  panelName?: string;
}

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    console.error(`Error in ${this.props.panelName || 'component'}:`, error, errorInfo);
  }

  handleRetry = () => {
    this.setState({ hasError: false, error: null });
    this.props.onRetry?.();
  };

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback;
      }

      return (
        <ErrorFallback
          error={this.state.error}
          panelName={this.props.panelName}
          onRetry={this.handleRetry}
        />
      );
    }

    return this.props.children;
  }
}

interface ErrorFallbackProps {
  error: Error | null;
  panelName?: string;
  onRetry: () => void;
}

function ErrorFallback({ error, panelName, onRetry }: ErrorFallbackProps) {
  // We can't use hooks inside class component render, so we use a functional subcomponent
  return <ErrorFallbackContent error={error} panelName={panelName} onRetry={onRetry} />;
}

function ErrorFallbackContent({ error, panelName, onRetry }: ErrorFallbackProps) {
  const { theme } = useTheme();

  return (
    <div className={`error-boundary-fallback ${theme === 'dark' ? 'bg-slate-800 border-red-500/30' : 'bg-white border-red-200'}`}>
      <div className="error-boundary-title">
        {panelName ? `${panelName} Error` : 'Something went wrong'}
      </div>
      <div className="error-boundary-message">
        {error?.message || 'An unexpected error occurred in this panel.'}
      </div>
      <button
        onClick={onRetry}
        className="error-boundary-retry"
      >
        Retry
      </button>
    </div>
  );
}

// Functional wrapper for using error boundary with hooks
interface PanelErrorBoundaryProps {
  children: ReactNode;
  panelName: string;
}

export function PanelErrorBoundary({ children, panelName }: PanelErrorBoundaryProps) {
  return (
    <ErrorBoundary panelName={panelName}>
      {children}
    </ErrorBoundary>
  );
}
