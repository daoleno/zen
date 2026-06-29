import React, { createContext, useContext, type ReactNode } from "react";

const TabScreenInsetContext = createContext<number | null>(null);

export function TabScreenInsetProvider({
  inset,
  children,
}: {
  inset: number;
  children: ReactNode;
}) {
  return (
    <TabScreenInsetContext.Provider value={inset}>
      {children}
    </TabScreenInsetContext.Provider>
  );
}

export function useTabScreenBottomInset(): number | null {
  return useContext(TabScreenInsetContext);
}