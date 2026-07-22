import React, { useCallback, type RefObject } from "react";
import {
  Pressable,
  StyleSheet,
  Text,
  View,
  type View as ViewInstance,
} from "react-native";
import { useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { Typography, useAppColors } from "../../constants/tokens";
import { appVersion } from "../../constants/appVersion";
import { useAgentServerSummary } from "../../store/agents";
import { useCurrentServer } from "../../store/currentServer";
import { ZenLogoMark } from "../ui/ZenLogoMark";
import {
  NavChevronIcon,
  NavCloseIcon,
  NavSettingsIcon,
  NavSkillsIcon,
  NavStatsIcon,
} from "./PrimaryNavIcons";

interface PrimaryDrawerPanelProps {
  closeButtonRef: RefObject<ViewInstance | null>;
  drawerVisible: boolean;
  onClose(): void;
  onClosePressIn(): void;
  onNavigateAway(): void;
}

type DrawerRowIcon = "settings" | "skills" | "stats";

interface DrawerRowProps {
  drawerVisible: boolean;
  icon: DrawerRowIcon;
  label: string;
  onPress(): void;
}

function DrawerRowIconView({
  color,
  icon,
}: {
  color: string;
  icon: DrawerRowIcon;
}) {
  if (icon === "stats") {
    return <NavStatsIcon color={color} size={19} />;
  }
  if (icon === "skills") {
    return <NavSkillsIcon color={color} size={19} />;
  }
  return <NavSettingsIcon color={color} size={19} />;
}

function DrawerRow({ drawerVisible, icon, label, onPress }: DrawerRowProps) {
  const colors = useAppColors();
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
      tabIndex={drawerVisible ? 0 : -1}
      style={({ pressed }) => [
        styles.drawerRow,
        {
          backgroundColor: pressed ? colors.surfacePressed : "transparent",
        },
      ]}
    >
      <View
        style={[
          styles.drawerRowIcon,
          { backgroundColor: colors.surfaceSubtle },
        ]}
      >
        <DrawerRowIconView color={colors.textSecondary} icon={icon} />
      </View>
      <Text
        numberOfLines={1}
        style={[
          styles.drawerRowLabel,
          {
            color: colors.textPrimary,
            fontFamily: Typography.uiFontMedium,
          },
        ]}
      >
        {label}
      </Text>
      <NavChevronIcon color={colors.textTertiary} size={17} />
    </Pressable>
  );
}

export function PrimaryDrawerPanel({
  closeButtonRef,
  drawerVisible,
  onClose,
  onClosePressIn,
  onNavigateAway,
}: PrimaryDrawerPanelProps) {
  const router = useRouter();
  const colors = useAppColors();
  const { serverConnections, serverConnectionIssues } = useAgentServerSummary();
  const { currentServer } = useCurrentServer();
  const currentConnection = currentServer
    ? serverConnections[currentServer.id] || "offline"
    : "offline";
  const currentIssue = currentServer
    ? serverConnectionIssues[currentServer.id] || null
    : null;
  const connectionSummary = currentServer?.name || "No current server";
  const connectionDetail =
    currentIssue?.title ??
    (currentConnection === "connected"
      ? "Connected"
      : currentConnection === "connecting"
        ? "Connecting"
        : "Offline");

  const openRoute = useCallback(
    (pathname: "/skills" | "/stats" | "/settings") => {
      onNavigateAway();
      router.push(pathname);
    },
    [onNavigateAway, router],
  );

  return (
    <SafeAreaView style={styles.drawerContent} edges={["top", "bottom"]}>
      <View style={styles.drawerIdentity}>
        <ZenLogoMark size={42} accessible={false} />
        <View style={styles.drawerIdentityCopy}>
          <Text
            style={[
              styles.drawerTitle,
              {
                color: colors.textPrimary,
                fontFamily: Typography.uiFontMedium,
              },
            ]}
          >
            Zen
          </Text>
        </View>
        <Pressable
          ref={closeButtonRef}
          onPress={onClose}
          onPressIn={onClosePressIn}
          accessibilityRole="button"
          accessibilityLabel="Close navigation drawer"
          tabIndex={drawerVisible ? 0 : -1}
          hitSlop={6}
          style={({ pressed }) => [
            styles.closeButton,
            {
              backgroundColor: pressed
                ? colors.surfacePressed
                : colors.surfaceSubtle,
            },
          ]}
        >
          <NavCloseIcon color={colors.textPrimary} size={20} />
        </Pressable>
      </View>

      <View
        style={[
          styles.connectionCard,
          {
            backgroundColor: colors.surfaceSubtle,
            borderColor: colors.borderSubtle,
          },
        ]}
      >
        <View
          style={[
            styles.connectionDot,
            {
              backgroundColor:
                currentConnection === "connected"
                  ? colors.statusRunning
                  : colors.statusUnknown,
            },
          ]}
        />
        <View style={styles.connectionCopy}>
          <Text style={[styles.connectionTitle, { color: colors.textPrimary }]}>
            {connectionSummary}
          </Text>
          <Text
            numberOfLines={2}
            style={[styles.connectionDetail, { color: colors.textTertiary }]}
          >
            {connectionDetail}
          </Text>
        </View>
      </View>

      <View
        style={[styles.drawerDivider, { backgroundColor: colors.borderSubtle }]}
      />

      <DrawerRow
        drawerVisible={drawerVisible}
        icon="skills"
        label="Skills"
        onPress={() => openRoute("/skills")}
      />
      <DrawerRow
        drawerVisible={drawerVisible}
        icon="stats"
        label="Stats"
        onPress={() => openRoute("/stats")}
      />
      <DrawerRow
        drawerVisible={drawerVisible}
        icon="settings"
        label="Settings"
        onPress={() => openRoute("/settings")}
      />

      <Text
        style={[
          styles.drawerVersion,
          {
            color: colors.textTertiary,
            fontFamily: Typography.terminalFont,
          },
        ]}
      >
        Zen v{appVersion}
      </Text>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  drawerContent: {
    flex: 1,
    paddingHorizontal: 18,
    paddingVertical: 14,
  },
  drawerIdentity: {
    minHeight: 64,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  drawerIdentityCopy: {
    flex: 1,
  },
  drawerTitle: {
    fontSize: 17,
    lineHeight: 22,
  },
  closeButton: {
    width: 44,
    height: 44,
    borderRadius: 12,
    alignItems: "center",
    justifyContent: "center",
  },
  connectionCard: {
    marginTop: 16,
    minHeight: 68,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 14,
    paddingHorizontal: 13,
    paddingVertical: 12,
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 10,
  },
  connectionDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
    marginTop: 5,
  },
  connectionCopy: {
    flex: 1,
  },
  connectionTitle: {
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  connectionDetail: {
    marginTop: 3,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
  },
  drawerDivider: {
    height: StyleSheet.hairlineWidth,
    marginVertical: 18,
  },
  drawerRow: {
    minHeight: 58,
    borderRadius: 12,
    paddingHorizontal: 10,
    flexDirection: "row",
    alignItems: "center",
    gap: 11,
  },
  drawerRowIcon: {
    width: 34,
    height: 34,
    borderRadius: 11,
    alignItems: "center",
    justifyContent: "center",
  },
  drawerRowLabel: {
    flex: 1,
    fontSize: 14,
    lineHeight: 21,
  },
  drawerVersion: {
    marginTop: "auto",
    paddingVertical: 8,
    textAlign: "center",
    fontSize: 11,
    lineHeight: 15,
  },
});
