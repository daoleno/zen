import React from "react";

interface CodexMarkdownErrorBoundaryProps {
  fallback: React.ReactNode;
  children: React.ReactNode;
  resetKey: string;
}

interface CodexMarkdownErrorBoundaryState {
  failed: boolean;
}

export class CodexMarkdownErrorBoundary extends React.Component<
  CodexMarkdownErrorBoundaryProps,
  CodexMarkdownErrorBoundaryState
> {
  state: CodexMarkdownErrorBoundaryState = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidUpdate(previousProps: CodexMarkdownErrorBoundaryProps) {
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
