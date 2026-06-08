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
  shortAgentLabel,
  shortProcessLabel,
  type DiscoveredSessionService,
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
            <Text style={styles.count}>{serviceCount}</Text>
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
                <Text style={styles.sectionTitle} numberOfLines={1}>
                  {section.title}
                </Text>
              ) : null}

              {section.groups.map((group) => (
                <View key={group.key} style={styles.projectCard}>
                  <View style={styles.projectHeader}>
                    <Text style={styles.projectTitle} numberOfLines={1}>
                      {group.project}
                    </Text>
                    <Text style={styles.projectMeta}>
                      {group.services.length} port
                      {group.services.length === 1 ? "" : "s"}
                    </Text>
                  </View>

                  {group.services.map((service, index) => (
                    <View
                      key={`${service.serverId}:${service.id}`}
                      style={[
                        styles.portRow,
                        index < group.services.length - 1
                          ? styles.portRowDivider
                          : null,
                      ]}
                    >
                      <View style={styles.portMain}>
                        <View style={styles.portTopRow}>
                          <Text style={styles.portNumber}>:{service.port}</Text>
                          <Text style={styles.portProcess} numberOfLines={1}>
                            {shortProcessLabel(
                              service.process || service.command || "",
                            )}
                          </Text>
                          <TouchableOpacity
                            style={styles.terminalButton}
                            onPress={() => onOpenTerminal(service)}
                            activeOpacity={0.82}
                            accessibilityLabel="Open terminal"
                          >
                            <Ionicons
                              name="terminal-outline"
                              size={15}
                              color={colors.textSecondary}
                            />
                          </TouchableOpacity>
                        </View>

                        <Text style={styles.portAgent} numberOfLines={1}>
                          {shortAgentLabel(service.agent_name)}
                        </Text>

                        <View style={styles.linkRow}>
                          {(service.urls ?? []).length > 0 ? (
                            (service.urls ?? []).map((item) => (
                              <TouchableOpacity
                                key={item.url}
                                style={styles.linkChip}
                                onPress={() => onOpenURL(item.url)}
                                activeOpacity={0.82}
                              >
                                <Text style={styles.linkLabel}>{item.label}</Text>
                                <Text style={styles.linkHost} numberOfLines={1}>
                                  {item.address}
                                </Text>
                              </TouchableOpacity>
                            ))
                          ) : (
                            <View style={styles.localChip}>
                              <Text style={styles.localText} numberOfLines={1}>
                                {(service.binds ?? []).length > 0
                                  ? (service.binds ?? []).join(", ")
                                  : "localhost"}
                                :{service.port}
                              </Text>
                            </View>
                          )}
                        </View>
                      </View>
                    </View>
                  ))}
                </View>
              ))}
            </View>
          ))}
        </ScrollView>
      )}
    </BottomSheetFrame>
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
    sectionTitle: {
      paddingHorizontal: 4,
      paddingTop: 2,
      color: colors.textSecondary,
      fontSize: 11,
      lineHeight: 14,
      fontFamily: Typography.uiFontMedium,
      letterSpacing: 0.5,
      textTransform: "uppercase",
      opacity: 0.62,
    },
    projectCard: {
      borderRadius: 14,
      backgroundColor: colors.surfaceSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      overflow: "hidden",
      minWidth: 0,
    },
    projectHeader: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      paddingHorizontal: 12,
      paddingTop: 11,
      paddingBottom: 8,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    projectTitle: {
      flex: 1,
      minWidth: 0,
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
      opacity: 0.68,
    },
    portRow: {
      paddingHorizontal: 12,
      paddingVertical: 10,
      minWidth: 0,
    },
    portRowDivider: {
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    portMain: {
      minWidth: 0,
      gap: 4,
    },
    portTopRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      minHeight: 22,
      minWidth: 0,
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
      fontFamily: Typography.terminalFont,
    },
    terminalButton: {
      width: 28,
      height: 28,
      borderRadius: 8,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: colors.surfaceActive,
    },
    portAgent: {
      color: colors.textSecondary,
      fontSize: 11,
      lineHeight: 14,
      fontFamily: Typography.uiFont,
      opacity: 0.66,
    },
    linkRow: {
      flexDirection: "row",
      flexWrap: "wrap",
      gap: 6,
      marginTop: 2,
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
      minHeight: 26,
      borderRadius: 8,
      paddingHorizontal: 8,
      paddingVertical: 5,
      backgroundColor: colors.surfacePressed,
      maxWidth: "100%",
    },
    localText: {
      color: colors.textSecondary,
      fontSize: 11,
      lineHeight: 14,
      fontFamily: Typography.terminalFont,
      opacity: 0.72,
    },
  });
}