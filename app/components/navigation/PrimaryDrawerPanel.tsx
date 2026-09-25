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
    return <NavStatsIcon color={color} size={20} />;
  }
  if (icon === "skills") {
    return <NavSkillsIcon color={color} size={20} />;
  }
  return <Ionicons name="settings-outline" color={color} size={20} />;
}

/**
 * One navigation destination. Every row pushes a screen, so there is no
 * selected state: the glyph is quiet ink and the label carries the meaning.
 */
function DrawerRow({ drawerVisible, icon, label, onPress }: DrawerRowProps) {
  const { colors } = useAppTheme();
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
      <View style={styles.drawerRowIcon}>
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
      <NavChevronIcon color={colors.textTertiary} size={16} />
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
  const connectionDetail = !currentServer
    ? "Pair a server in Settings"
    : currentIssue?.title ??
      (currentConnection === "connected"
        ? "Connected"
        : currentConnection === "connecting"
          ? "Connecting"
          : "Offline");
  // Healthy is the quiet default; only a state the user can act on is colored.
  const connectionInk = currentIssue
    ? colors.dangerText
    : currentServer && currentConnection === "offline"
      ? colors.warning
      : colors.textTertiary;

  const openRoute = useCallback(
    (pathname: "/skills" | "/stats" | "/settings" | "/remote-desktop") => {
      onNavigateAway();
      router.push(pathname);
    },
    [onNavigateAway, router],
  );

  return (
    <SafeAreaView style={styles.drawerContent} edges={["top", "bottom"]}>
      <View style={styles.drawerIdentity}>
        <ZenLogoMark size={30} accessible={false} />
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
                : "transparent",
            },
          ]}
        >
          <NavCloseIcon color={colors.textSecondary} size={18} />
        </Pressable>
      </View>

      <ScrollView
        style={styles.drawerScroll}
        showsVerticalScrollIndicator={false}
        keyboardShouldPersistTaps="handled"
      >
        {/* Where you are. Read-only: switching servers lives in Settings. */}
        <View
          accessible
          accessibilityLabel={`Current server, ${connectionSummary}, ${connectionDetail}`}
          style={styles.serverHeader}
        >
          <View style={[styles.serverGlyph, { backgroundColor: theme.materials.tint }]}>
            <Ionicons name="desktop-outline" size={17} color={colors.accentStrong} />
          </View>
          <View style={styles.serverCopy}>
            <Text
              numberOfLines={1}
              style={[styles.serverTitle, { color: colors.textPrimary }]}
            >
              {connectionSummary}
            </Text>
            <Text
              numberOfLines={1}
              style={[styles.serverDetail, { color: connectionInk }]}
            >
              {connectionDetail}
            </Text>
          </View>
        </View>

        <View
          style={[
            styles.drawerGroup,
            {
              backgroundColor: colors.bgElevated,
              borderColor: theme.isLight ? "transparent" : theme.materials.stroke,
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
          <View style={[styles.groupSeparator, { backgroundColor: theme.materials.separator }]} />
          <DrawerRow
            drawerVisible={drawerVisible}
            icon="settings"
            label="Settings"
            onPress={() => openRoute("/settings")}
          />
        </View>
      </ScrollView>

      <View style={styles.drawerFooter}>
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

const DRAWER_ICON_SLOT = 28;

const styles = StyleSheet.create({
  drawerContent: {
    flex: 1,
    paddingHorizontal: 12,
    paddingVertical: 8,
  },
  drawerIdentity: {
    minHeight: 56,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingLeft: 8,
  },
  drawerScroll: {
    flex: 1,
    minHeight: 0,
  },
  drawerFooter: {
    flexShrink: 0,
    paddingTop: 8,
  },
  drawerTitle: {
    flex: 1,
    fontSize: 20,
    lineHeight: 28,
  },
  closeButton: {
    width: 44,
    height: 44,
    borderRadius: 22,
    alignItems: "center",
    justifyContent: "center",
  },
  serverHeader: {
    marginTop: 12,
    minHeight: 56,
    paddingHorizontal: 8,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  serverGlyph: {
    width: 36,
    height: 36,
    borderRadius: 11,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
  serverCopy: {
    flex: 1,
    minWidth: 0,
    gap: 1,
  },
  serverTitle: {
    fontSize: 15,
    lineHeight: 21,
    fontFamily: Typography.uiFontMedium,
  },
  serverDetail: {
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
  },
  drawerGroup: {
    marginTop: 16,
    borderRadius: Radii.card,
    ...ContinuousCorners,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
  },
  groupSeparator: {
    height: StyleSheet.hairlineWidth,
    marginLeft: 16 + DRAWER_ICON_SLOT + 12,
  },
  drawerRow: {
    minHeight: 52,
    paddingHorizontal: 16,
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
  },
  drawerRowIcon: {
    width: DRAWER_ICON_SLOT,
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
