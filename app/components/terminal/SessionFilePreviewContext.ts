import React from "react";

export interface SessionFilePreviewContextValue {
  open(reference: string): boolean;
}

export const SessionFilePreviewContext =
  React.createContext<SessionFilePreviewContextValue | null>(null);

export function useSessionFilePreviewContext() {
  return React.useContext(SessionFilePreviewContext);
}
