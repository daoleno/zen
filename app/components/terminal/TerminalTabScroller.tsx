import React from "react";
import {
  ScrollView,
  StyleSheet,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TerminalTabPill } from "./TerminalTabPill";
import type { TerminalTabDescriptor } from "./TerminalTopBar";

interface TerminalTabScrollerProps {
  tabs: TerminalTabDescriptor[];
  chrome: TerminalThemeChrome;
  tabScrollRef: React.RefObject<ScrollView | null>;
  menuAnchorRef: React.RefObject<View | null>;
  onOpenTab(id: string): void;
  onOpenMenu(): void;
  onTabLayout(id: string, layout: { x: number; width: number }): void;
}

export function TerminalTabScroller({
  tabs,
  chrome,
  tabScrollRef,
  menuAnchorRef,
  onOpenTab,
  onOpenMenu,
  onTabLayout,
}: TerminalTabScrollerProps) {
  return (
    <ScrollView
      ref={tabScrollRef}
      horizontal
      showsHorizontalScrollIndicator={false}
      style={styles.tabScroller}
      contentContainerStyle={styles.tabScrollerContent}
    >
      {tabs.map((tab) => (
        <TerminalTabPill
          key={tab.id}
          tab={tab}
          chrome={chrome}
          menuAnchorRef={menuAnchorRef}
          onOpenTab={onOpenTab}
          onOpenMenu={onOpenMenu}
          onLayout={(event) => {
            const { x, width } = event.nativeEvent.layout;
            onTabLayout(tab.id, { x, width });
          }}
        />
      ))}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  tabScroller: {
    flex: 1,
    marginHorizontal: 4,
  },
  tabScrollerContent: {
    paddingRight: 2,
  },
});
