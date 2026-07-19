import React from "react";

interface InterfaceMarkdownErrorBoundaryProps {
  fallback: React.ReactNode;
  children: React.ReactNode;
  resetKey: string;
}

interface InterfaceMarkdownErrorBoundaryState {
  failed: boolean;
}

export class InterfaceMarkdownErrorBoundary extends React.Component<
  InterfaceMarkdownErrorBoundaryProps,
  InterfaceMarkdownErrorBoundaryState
> {
  state: InterfaceMarkdownErrorBoundaryState = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidUpdate(previousProps: InterfaceMarkdownErrorBoundaryProps) {
    if (previousProps.resetKey !== this.props.resetKey && this.state.failed) {
      this.setState({ failed: false });
    }
  }

  componentDidCatch(error: unknown) {
    console.warn("[codex] native markdown renderer failed", error);
  }

  render() {
    if (this.state.failed) {
      return this.props.fallback;
    }
    return this.props.children;
  }
}
