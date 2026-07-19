interface TerminalExitRouter {
  dismissTo(href: "/list"): void;
}

export function dismissTerminalToSessions(router: TerminalExitRouter): void {
  router.dismissTo("/list");
}
