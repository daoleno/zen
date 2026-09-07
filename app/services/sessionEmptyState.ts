export function sessionEmptyState(hasServer: boolean, connection: string | undefined, filtered = false) {
  if (!hasServer) return { title: "Connect your computer", icon: "server-outline", action: "pair", label: "Pair a server", busy: false } as const;
  if (connection === "connecting") return { title: "Connecting", icon: "server-outline", action: null, label: "", busy: true } as const;
  if (connection !== "connected") return { title: "Server offline", icon: "cloud-offline-outline", action: "retry", label: "Retry connection", busy: false } as const;
  if (filtered) return { title: "No matches", icon: "search-outline", action: "clear", label: "Clear filters", busy: false } as const;
  return { title: "No sessions yet", icon: "terminal-outline", action: "terminal", label: "New terminal", busy: false } as const;
}
