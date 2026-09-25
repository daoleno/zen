import React, { useCallback, type RefObject } from "react";
import {
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
  type View as ViewInstance,
} from "react-native";
import { useRouter } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  ContinuousCorners,
  Radii,
  Typography,
  useAppTheme,
} from "../../constants/tokens";
import { appVersion } from "../../constants/appVersion";
import { useWorkerServerSummary } from "../../store/workers";
import { useCurrentServer } from "../../store/currentServer";
import { GlassSurface } from "../ui/GlassSurface";
import { StatusPill, type StatusTone } from "../ui/StatusPill";
import { ZenLogoMark } from "../ui/ZenLogoMark";
import {
  NavChevronIcon,
  NavCloseIcon,
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
  return <Ionicons name="settings-outline" color={color} size={19} />;
}

function DrawerRow({ drawerVisible, icon, label, onPress }: DrawerRowProps) {
  const { colors, theme } = useAppTheme();
  return (
    <Pressable
      onPress={onPress}
      accessibilityRole="button"
      accessibilityLabel={label}
      tabIndex={drawerVisible ? 0 : -1}
      android_ripple={{ color: colors.surfacePressed }}
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
          { backgroundColor: theme.materials.tint },
        ]}
      >
        <DrawerRowIconView color={colors.accentStrong} icon={icon} />
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
  const { colors, theme } = useAppTheme();
  const { serverConnections, serverConnectionIssues } = useWorkerServerSummary();
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
  const connectionTone: StatusTone = currentIssue
    ? "danger"
    : currentConnection === "connected"
      ? "success"
      : currentConnection === "connecting"
        ? "warning"
        : "neutral";

  const openRoute = useCallback(
    (pathname: "/skills" | "/stats" | "/settings" | "/remote-desktop") => {
      onNavigateAway();
      router.push(pathname);
    },
    [onNavigateAway, router],
  );

  return (
    <SafeAreaView style={styles.drawerContent} edges={["top", "bottom"]}>
      <ScrollView
        style={styles.drawerScroll}
        showsVerticalScrollIndicator={false}
        keyboardShouldPersistTaps="handled"
      >
        <View style={styles.drawerIdentity}>
          <ZenLogoMark size={34} accessible={false} />
          <Text
            style={[
              styles.drawerTitle,
              {
                color: colors.textPrimary,
                fontFamily: Typography.uiFontMedium,
              },
            ]}
            accessibilityRole="header"
          >
            Zen
          </Text>
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
            <NavCloseIcon color={colors.textSecondary} size={18} />
          </Pressable>
        </View>

        <Pressable
          onPress={() => openRoute("/settings")}
          accessibilityRole="button"
          accessibilityLabel={`${connectionSummary}, ${connectionDetail}`}
          accessibilityHint="Opens server settings"
          tabIndex={drawerVisible ? 0 : -1}
          style={({ pressed }) => (pressed ? styles.pressedCard : null)}
        >
          <GlassSurface
            material="thin"
            radius={Radii.card}
            elevation="card"
            style={styles.connectionCard}
          >
            <View style={[styles.serverGlyph, { backgroundColor: theme.materials.tint }]}>
              <Ionicons name="desktop-outline" size={20} color={colors.accentStrong} />
            </View>
            <View style={styles.connectionCopy}>
              <Text
                numberOfLines={1}
                style={[styles.connectionTitle, { color: colors.textPrimary }]}
              >
                {connectionSummary}
              </Text>
              <StatusPill
                label={connectionDetail}
                tone={connectionTone}
                live={currentConnection === "connecting"}
              />
            </View>
            <NavChevronIcon color={colors.textTertiary} size={17} />
          </GlassSurface>
        </Pressable>

        <View
          style={[
            styles.drawerGroup,
            {
              backgroundColor: colors.bgElevated,
              borderColor: theme.materials.stroke,
            },
          ]}
        >
          <DrawerRow
            drawerVisible={drawerVisible}
            icon="skills"
            label="Skills"
            onPress={() => openRoute("/skills")}
          />
          <View style={[styles.groupSeparator, { backgroundColor: theme.materials.separator }]} />
          <DrawerRow
            drawerVisible={drawerVisible}
            icon="stats"
            label="Stats"
            onPress={() => openRoute("/stats")}
          />
        </View>
      </ScrollView>

      <View
        style={[
          styles.drawerFooter,
          { borderTopColor: colors.borderSubtle },
        ]}
      >
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
      </View>
    </SafeAreaView>
  );
}

const DRAWER_ICON_TILE = 32;

const styles = StyleSheet.create({
  drawerContent: {
    flex: 1,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  drawerIdentity: {
    minHeight: 56,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 4,
  },
  drawerScroll: {
    flex: 1,
    minHeight: 0,
  },
  drawerFooter: {
    flexShrink: 0,
    borderTopWidth: StyleSheet.hairlineWidth,
    marginTop: 12,
    paddingTop: 8,
  },
  drawerTitle: {
    flex: 1,
    fontSize: 22,
    lineHeight: 30,
  },
  closeButton: {
    width: 44,
    height: 44,
    borderRadius: 22,
    alignItems: "center",
    justifyContent: "center",
  },
  pressedCard: {
    opacity: 0.8,
    transform: [{ scale: 0.99 }],
  },
  connectionCard: {
    marginTop: 14,
    minHeight: 76,
    paddingHorizontal: 14,
    paddingVertical: 14,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  serverGlyph: {
    width: 40,
    height: 40,
    borderRadius: 12,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
  connectionCopy: {
    flex: 1,
    minWidth: 0,
    gap: 6,
  },
  connectionTitle: {
    fontSize: 16,
    lineHeight: 22,
    fontFamily: Typography.uiFontMedium,
  },
  drawerGroup: {
    marginTop: 22,
    borderRadius: Radii.card,
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  groupSeparator: {
    height: StyleSheet.hairlineWidth,
    marginLeft: 14 + DRAWER_ICON_TILE + 12,
  },
  drawerRow: {
    minHeight: 58,
    paddingHorizontal: 14,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  drawerRowIcon: {
    width: DRAWER_ICON_TILE,
    height: DRAWER_ICON_TILE,
    borderRadius: 9,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
  drawerRowLabel: {
    flex: 1,
    fontSize: 15,
    lineHeight: 22,
  },
  drawerVersion: {
    paddingVertical: 8,
    textAlign: "center",
    fontSize: 11,
    lineHeight: 15,
  },
});
