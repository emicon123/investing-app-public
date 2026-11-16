import { Component, type ReactNode, type ErrorInfo } from "react";

type Props = { children: ReactNode; fallback?: ReactNode };
type State = { hasError: boolean; error?: Error };

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Uncaught error in boundary:", error, info);
  }

  render() {
    if (this.state.hasError) {
      return (
        this.props.fallback ?? (
          <div className="p-6 space-y-2">
            <p role="alert" className="text-destructive text-sm font-medium">
              Wystąpił nieoczekiwany błąd. Odśwież stronę lub wróć do strony głównej.
            </p>
            {this.state.error && (
              <p className="text-xs text-muted-foreground font-mono">
                {this.state.error.message}
              </p>
            )}
          </div>
        )
      );
    }
    return this.props.children;
  }
}
