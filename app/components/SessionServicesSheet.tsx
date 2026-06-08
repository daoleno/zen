import React, { useMemo } from "react";
import {
  ActivityIndicator,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography, useAppColors } from "../constants/tokens";
import { BottomSheetFrame } from "./ui/BottomSheetFrame";
import {
  groupSessionServices,
  presentSessionServiceURL,
  serviceAgentLabel,
  serviceBindLabel,
  serviceCommandDetail,
  serviceProcessLabel,
  type DiscoveredSessionService,
  type SessionServiceGroup,
} from "../services/sessionServicesPresentation";

interface SessionServicesSheetProps {
  visible: boolean;
  services: DiscoveredSessionService[];
  loading: boolean;
  error: string | null;
  showServerSections: boolean;
  onClose(): void;
  onRefresh(): void;
  onOpenTerminal(service: DiscoveredSessionService): void;
  onOpenURL(url: string): void;
}

export function SessionServicesSheet({
  visible,
  services,
  loading,
  error,
  showServerSections,
  onClose,
  onRefresh,
  onOpenTerminal,
  onOpenURL,
}: SessionServicesSheetProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const sections = useMemo(
    () => groupSessionServices(services, { showServerSections }),
    [services, showServerSections],
  );
  const serviceCount = services.length;

  return (
    <BottomSheetFrame
      visible={visible}
      maxHeight="78%"
      contentStyle={styles.sheetContent}
      onClose={onClose}
    >
      <View style={styles.header}>
        <View style={styles.headerMain}>
          <Text style={styles.title}>Services</Text>
          {serviceCount > 0 ? (
            <Text style={styles.count}>
              {serviceCount} port{serviceCount === 1 ? "" : "s"}
            </Text>
          ) : null}
        </View>
        <TouchableOpacity
          style={styles.iconButton}
          onPress={onRefresh}
          disabled={loading}
          activeOpacity={0.82}
        >
          {loading ? (
            <ActivityIndicator size="small" color={colors.accent} />
          ) : (
            <Ionicons name="refresh" size={17} color={colors.textSecondary} />
          )}
        </TouchableOpacity>
        <TouchableOpacity
          style={styles.iconButton}
          onPress={onClose}
          activeOpacity={0.82}
        >
          <Ionicons name="close" size={19} color={colors.textSecondary} />
        </TouchableOpacity>
      </View>

      {error ? <Text style={styles.error}>{error}</Text> : null}

      {loading && services.length === 0 ? (
        <View style={styles.loading}>
          <ActivityIndicator color={colors.accent} />
        </View>
      ) : services.length === 0 ? (
        <View style={styles.empty}>
          <Ionicons name="radio-outline" size={22} color={colors.textSecondary} />
          <Text style={styles.emptyText}>No listening services found.</Text>
        </View>
      ) : (
        <ScrollView
          style={styles.scroll}
          contentContainerStyle={styles.list}
          showsVerticalScrollIndicator={false}
        >
          {sections.map((section) => (
            <View key={section.key} style={styles.section}>
              {section.title ? (
                <View style={styles.sectionHeader}>
                  <Text style={styles.sectionTitle} numberOfLines={1}>
                    {section.title}
                  </Text>
                  <Text style={styles.sectionMeta} numberOfLines={1}>
                    {section.groups.length} project
                    {section.groups.length === 1 ? "" : "s"}
                  </Text>
                </View>
              ) : null}

              {section.groups.map((group) => (
                <ServiceProjectCard
                  key={group.key}
                  group={group}
                  colors={colors}
                  styles={styles}
                  onOpenTerminal={onOpenTerminal}
                  onOpenURL={onOpenURL}
                />
              ))}
            </View>
          ))}
        </ScrollView>
      )}
    </BottomSheetFrame>
  );
}

function ServiceProjectCard({
  group,
  colors,
  styles,
  onOpenTerminal,
  onOpenURL,
}: {
  group: SessionServiceGroup;
  colors: ReturnType<typeof useAppColors>;
  styles: ReturnType<typeof createStyles>;
  onOpenTerminal(service: DiscoveredSessionService): void;
  onOpenURL(url: string): void;
}) {
  return (
    <View style={styles.projectCard}>
      <View style={styles.projectHeader}>
        <View style={styles.projectHeaderIcon}>
          <Ionicons name="folder-open-outline" size={15} color={colors.promptYellow} />
        </View>
        <View style={styles.projectHeaderCopy}>
          <Text style={styles.projectTitle} numberOfLines={1}>
            {group.project}
          </Text>
          <Text style={styles.projectMeta} numberOfLines={1}>
            {group.headerMeta}
          </Text>
        </View>
      </View>

      {group.services.map((service, index) => (
        <ServicePortRow
          key={`${service.serverId}:${service.id}`}
          service={service}
          last={index >= group.services.length - 1}
          colors={colors}
          styles={styles}
          onOpenTerminal={onOpenTerminal}
          onOpenURL={onOpenURL}
        />
      ))}
    </View>
  );
}

function ServicePortRow({
  service,
  last,
  colors,
  styles,
  onOpenTerminal,
  onOpenURL,
}: {
  service: DiscoveredSessionService;
  last: boolean;
  colors: ReturnType<typeof useAppColors>;
  styles: ReturnType<typeof createStyles>;
  onOpenTerminal(service: DiscoveredSessionService): void;
  onOpenURL(url: string): void;
}) {
  const urls = (service.urls ?? []).map(presentSessionServiceURL);
  const commandDetail = serviceCommandDetail(service);

  return (
    <View style={[styles.portRow, !last ? styles.portRowDivider : null]}>
      <View style={styles.portPill}>
        <Text style={styles.portNumber}>:{service.port}</Text>
      </View>
      <View style={styles.portMain}>
        <View style={styles.portTopRow}>
          <Text style={styles.portProcess} numberOfLines={1}>
            {serviceProcessLabel(service)}
          </Text>
          <TouchableOpacity
            style={styles.terminalButton}
            onPress={() => onOpenTerminal(service)}
            activeOpacity={0.82}
            accessibilityLabel={`Open terminal for port ${service.port}`}
          >
            <Ionicons
              name="terminal-outline"
              size={15}
              color={colors.textSecondary}
            />
          </TouchableOpacity>
        </View>

        {commandDetail ? (
          <Text style={styles.commandDetail} numberOfLines={1}>
            {commandDetail}
          </Text>
        ) : null}

        <View style={styles.agentRow}>
          <Ionicons
            name="person-circle-outline"
            size={13}
            color={colors.textSecondary}
          />
          <Text style={styles.portAgent} numberOfLines={1}>
            {serviceAgentLabel(service)}
          </Text>
        </View>

        <View style={styles.linkRow}>
          {urls.length > 0 ? (
            urls.map((item) => (
              <TouchableOpacity
                key={item.key}
                style={styles.linkChip}
                onPress={() => onOpenURL(item.url)}
                activeOpacity={0.82}
                accessibilityLabel={`Open ${item.label} URL for port ${service.port}`}
              >
                <Text style={styles.linkLabel}>{item.label}</Text>
                <Text style={styles.linkHost} numberOfLines={1}>
                  {item.address}
                </Text>
                <Ionicons name="open-outline" size={12} color={colors.textSecondary} />
              </TouchableOpacity>
            ))
          ) : (
            <View style={styles.localChip}>
              <Text style={styles.localLabel}>Bind</Text>
              <Text style={styles.localText} numberOfLines={1}>
                {serviceBindLabel(service)}
              </Text>
            </View>
          )}
        </View>
      </View>
    </View>
  );
}

function createStyles(colors: ReturnType<typeof useAppColors>) {
  return StyleSheet.create({
    sheetContent: {
      paddingHorizontal: 0,
      paddingBottom: 8,
      minWidth: 0,
    },
    header: {
      minHeight: 44,
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      paddingHorizontal: 18,
      marginBottom: 4,
    },
    headerMain: {
      flex: 1,
      minWidth: 0,
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
    },
    title: {
      color: colors.textPrimary,
      fontSize: 17,
      lineHeight: 22,
      fontFamily: Typography.uiFontMedium,
      letterSpacing: -0.2,
    },
    count: {
      color: colors.textSecondary,
      fontSize: 12,
      lineHeight: 16,
      fontFamily: Typography.uiFontMedium,
      opacity: 0.72,
    },
    iconButton: {
      width: 34,
      height: 34,
      borderRadius: 10,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfaceSubtle,
    },
    error: {
      paddingHorizontal: 18,
      paddingBottom: 8,
      color: colors.dangerText,
      fontSize: 12,
      lineHeight: 16,
      fontFamily: Typography.uiFont,
    },
    loading: {
      minHeight: 160,
      alignItems: "center",
      justifyContent: "center",
    },
    empty: {
      minHeight: 180,
      alignItems: "center",
      justifyContent: "center",
      gap: 10,
    },
    emptyText: {
      color: colors.textSecondary,
      fontSize: 13,
      lineHeight: 18,
      fontFamily: Typography.uiFont,
    },
    scroll: {
      flexGrow: 0,
      maxHeight: 520,
    },
    list: {
      paddingHorizontal: 14,
      paddingBottom: 12,
      gap: 10,
    },
    section: {
      gap: 8,
      minWidth: 0,
    },
    sectionHeader: {
      minHeight: 18,
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      paddingHorizontal: 4,
      paddingTop: 2,
    },
    sectionTitle: {
      flex: 1,
      minWidth: 0,
      color: colors.textSecondary,
      fontSize: 11,
      lineHeight: 14,
      fontFamily: Typography.uiFontMedium,
      letterSpacing: 0.5,
      textTransform: "uppercase",
      opacity: 0.62,
    },
    sectionMeta: {
      color: colors.textSecondary,
      fontSize: 10,
      lineHeight: 13,
      fontFamily: Typography.uiFont,
      opacity: 0.52,
    },
    projectCard: {
      borderRadius: 8,
      backgroundColor: colors.surfaceSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      overflow: "hidden",
      minWidth: 0,
    },
    projectHeader: {
      flexDirection: "row",
      alignItems: "center",
      gap: 9,
      paddingHorizontal: 12,
      paddingTop: 10,
      paddingBottom: 9,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    projectHeaderIcon: {
      width: 26,
      height: 26,
      borderRadius: 8,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfacePressed,
    },
    projectHeaderCopy: {
      flex: 1,
      minWidth: 0,
      gap: 1,
    },
    projectTitle: {
      color: colors.textPrimary,
      fontSize: 14,
      lineHeight: 18,
      fontFamily: Typography.uiFontMedium,
    },
    projectMeta: {
      color: colors.textSecondary,
      fontSize: 11,
      lineHeight: 14,
      fontFamily: Typography.uiFont,
      opacity: 0.72,
    },
    portRow: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 10,
      paddingHorizontal: 12,
      paddingVertical: 9,
      minWidth: 0,
    },
    portRowDivider: {
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    portMain: {
      flex: 1,
      minWidth: 0,
      gap: 4,
    },
    portTopRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      minHeight: 24,
      minWidth: 0,
    },
    portPill: {
      minWidth: 48,
      minHeight: 24,
      borderRadius: 8,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: 7,
      backgroundColor: colors.surfacePressed,
    },
    portNumber: {
      color: colors.promptYellow,
      fontSize: 13,
      lineHeight: 18,
      fontFamily: Typography.terminalFontBold,
    },
    portProcess: {
      flex: 1,
      minWidth: 0,
      color: colors.textPrimary,
      fontSize: 12,
      lineHeight: 16,
      fontFamily: Typography.terminalFontBold,
    },
    terminalButton: {
      width: 28,
      height: 28,
      borderRadius: 8,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfaceActive,
    },
    commandDetail: {
      color: colors.textSecondary,
      fontSize: 11,
      lineHeight: 14,
      fontFamily: Typography.terminalFont,
      opacity: 0.64,
    },
    agentRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 4,
      minWidth: 0,
    },
    portAgent: {
      flex: 1,
      minWidth: 0,
      color: colors.textSecondary,
      fontSize: 11,
      lineHeight: 14,
      fontFamily: Typography.uiFont,
      opacity: 0.74,
    },
    linkRow: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 6,
      marginTop: 1,
    },
    linkChip: {
      flexDirection: "row",
      alignItems: "center",
      gap: 5,
      maxWidth: "100%",
      minHeight: 26,
      borderRadius: 8,
      paddingHorizontal: 8,
      paddingVertical: 4,
      backgroundColor: colors.surfaceActive,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderStrong,
    },
    linkLabel: {
      color: colors.accent,
      fontSize: 10,
      lineHeight: 12,
      fontFamily: Typography.uiFontMedium,
      textTransform: "uppercase",
    },
    linkHost: {
      flexShrink: 1,
      color: colors.textPrimary,
      fontSize: 11,
      lineHeight: 14,
      fontFamily: Typography.terminalFont,
      opacity: 0.88,
    },
    localChip: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      minHeight: 26,
      borderRadius: 8,
      paddingHorizontal: 8,
      paddingVertical: 5,
      backgroundColor: colors.surfacePressed,
      maxWidth: "100%",
    },
    localLabel: {
      color: colors.textSecondary,
      fontSize: 10,
      lineHeight: 12,
      fontFamily: Typography.uiFontMedium,
      textTransform: "uppercase",
      opacity: 0.7,
    },
    localText: {
      flexShrink: 1,
      color: colors.textSecondary,
      fontSize: 11,
      lineHeight: 14,
      fontFamily: Typography.terminalFont,
      opacity: 0.72,
    },
  });
}
