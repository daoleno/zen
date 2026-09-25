import React, { useEffect, useMemo, useRef, useState } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import * as Clipboard from "expo-clipboard";
import * as Haptics from "expo-haptics";
import {
  ContinuousCorners,
  Typography,
  useAppTheme,
} from "../constants/tokens";
import { AppText } from "./ui/AppText";
import { BottomSheetFrame } from "./ui/BottomSheetFrame";
import { EmptyState } from "./ui/EmptyState";
import { IconButton } from "./ui/IconButton";
import { InlineNotice } from "./ui/InlineNotice";
import { ListRow, ListSection } from "./ui/ListSection";
import { StatusPill } from "./ui/StatusPill";
import { useServiceTunnel } from "./services/useServiceTunnel";
import {
  groupSessionServices,
  hasServiceTerminal,
  isDSHWebService,
  presentSessionServiceURL,
  serviceSourceLabel,
  serviceWorkerLabel,
  serviceBindLabel,
  serviceCommandDetail,
  serviceProcessLabel,
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

/**
 * Listening services on the current server. The list is one row per port,
 * grouped by project; everything you can do with a port lives on its detail
 * page inside the same sheet, so the list itself stays calm.
 */
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
  const { colors } = useAppTheme();
  const sections = useMemo(
    () => groupSessionServices(services, { showServerSections }),
    [services, showServerSections],
  );
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const selected = selectedKey
    ? services.find((service) => serviceKey(service) === selectedKey) ?? null
    : null;

  useEffect(() => {
    if (!visible) setSelectedKey(null);
  }, [visible]);

  const closeDetail = () => {
    setSelectedKey(null);
    // Tunnel state may have changed on the detail page.
    onRefresh();
  };

  const serviceCount = services.length;
  const subtitle = loading && serviceCount === 0
    ? "Looking for listening ports"
    : serviceCount === 0
      ? null
      : `${serviceCount} ${serviceCount === 1 ? "port" : "ports"}`;

  return (
    <BottomSheetFrame
      visible={visible}
      maxHeight="82%"
      rootStyle={styles.sheetRoot}
      contentStyle={styles.sheetContent}
      onClose={onClose}
    >
      {selected ? (
        <ServiceDetail
          key={serviceKey(selected)}
          service={selected}
          onBack={closeDetail}
          onOpenTerminal={onOpenTerminal}
          onOpenURL={onOpenURL}
        />
      ) : (
        <>
          <View style={styles.header}>
            <View style={styles.headerCopy}>
              <AppText variant="title" accessibilityRole="header">
                Services
              </AppText>
              {subtitle ? (
                <AppText variant="caption" tone="tertiary">
                  {subtitle}
                </AppText>
              ) : null}
            </View>
            <IconButton
              icon="refresh"
              accessibilityLabel="Refresh services"
              disabled={loading}
              onPress={onRefresh}
            />
            <IconButton
              icon="close"
              accessibilityLabel="Close services"
              onPress={onClose}
            />
          </View>

          {error ? (
            <InlineNotice
              tone="danger"
              title="Services could not be loaded"
              detail={error}
              action={{ label: "Retry", onPress: onRefresh, disabled: loading }}
              style={styles.notice}
            />
          ) : null}

          {serviceCount === 0 ? (
            error ? null : (
              <EmptyState
                size="inline"
                busy={loading}
                icon="radio-outline"
                title={loading ? "Loading services" : "No listening services"}
                detail={loading ? null : "Ports opened by Sessions and persistent services appear here."}
                style={styles.empty}
              />
            )
          ) : (
            <ScrollView
              style={styles.scroll}
              contentContainerStyle={styles.list}
              showsVerticalScrollIndicator={false}
            >
              {sections.flatMap((section) =>
                section.groups.map((group) => (
                  <ListSection
                    key={group.key}
                    title={section.title ? `${section.title} · ${group.project}` : group.project}
                    style={styles.section}
                  >
                    {group.services.map((service) => (
                      <ServiceRow
                        key={serviceKey(service)}
                        service={service}
                        onPress={() => setSelectedKey(serviceKey(service))}
                      />
                    ))}
                  </ListSection>
                )),
              )}
              {loading ? (
                <AppText variant="caption" tone="tertiary" style={[styles.refreshing, { color: colors.textTertiary }]}>
                  Refreshing
                </AppText>
              ) : null}
            </ScrollView>
          )}
        </>
      )}
    </BottomSheetFrame>
  );
}

function serviceKey(service: DiscoveredSessionService): string {
  return `${service.serverId}:${service.id}`;
}

function PortTile({ port }: { port: number }) {
  const { colors, theme } = useAppTheme();
  return (
    <View style={[styles.portTile, { backgroundColor: theme.materials.tint }]}>
      <AppText
        numberOfLines={1}
        adjustsFontSizeToFit
        style={[styles.portText, { color: colors.accentStrong }]}
      >
        {port}
      </AppText>
    </View>
  );
}

function ServiceRow({
  service,
  onPress,
}: {
  service: DiscoveredSessionService;
  onPress(): void;
}) {
  const persistent = !hasServiceTerminal(service) && service.source === "persistent";
  const publicTunnel = service.tunnel?.status === "running";
  const subtitle = persistent
    ? serviceSourceLabel(service)
    : serviceWorkerLabel(service);
  return (
    <ListRow
      title={serviceProcessLabel(service)}
      subtitle={subtitle}
      leading={<PortTile port={service.port} />}
      trailing={publicTunnel ? <StatusPill label="Public" tone="accent" /> : null}
      accessory="chevron"
      accessibilityLabel={`Port ${service.port}, ${serviceProcessLabel(service)}, ${subtitle}${publicTunnel ? ", public tunnel running" : ""}`}
      accessibilityHint="Shows links and actions for this service"
      onPress={onPress}
    />
  );
}

function ServiceDetail({
  service,
  onBack,
  onOpenTerminal,
  onOpenURL,
}: {
  service: DiscoveredSessionService;
  onBack(): void;
  onOpenTerminal(service: DiscoveredSessionService): void;
  onOpenURL(url: string): void;
}) {
  const tunnel = useServiceTunnel(service);
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => {
    if (copiedTimer.current) clearTimeout(copiedTimer.current);
  }, []);
  const urls = (service.urls ?? []).map(presentSessionServiceURL);
  const dshWeb = isDSHWebService(service);
  const commandDetail = serviceCommandDetail(service);
  const persistent = !hasServiceTerminal(service) && service.source === "persistent";
  const statusDetail = (service.status_detail || "").trim();

  const copyPublicURL = async (url: string) => {
    await Clipboard.setStringAsync(url);
    void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    setCopied(true);
    if (copiedTimer.current) clearTimeout(copiedTimer.current);
    copiedTimer.current = setTimeout(() => setCopied(false), 1600);
  };

  return (
    <>
      <View style={styles.header}>
        <IconButton
          icon="chevron-back"
          accessibilityLabel="Back to services"
          onPress={onBack}
        />
        <View style={styles.headerCopy}>
          <AppText variant="title" numberOfLines={1} accessibilityRole="header">
            {serviceProcessLabel(service)}
          </AppText>
          <AppText variant="caption" tone="tertiary" numberOfLines={1}>
            {`Port ${service.port} · ${persistent ? serviceSourceLabel(service) : serviceWorkerLabel(service)}`}
          </AppText>
        </View>
      </View>

      <ScrollView
        style={styles.scroll}
        contentContainerStyle={styles.list}
        showsVerticalScrollIndicator={false}
      >
        <ListSection title="Open">
          {dshWeb ? (
            urls.length > 0 ? (
              <ListRow
                title="Open Web"
                subtitle={urls[0]!.address}
                icon="globe-outline"
                accessory="chevron"
                accessibilityLabel={`Open DSH Web for port ${service.port}`}
                onPress={() => onOpenURL(urls[0]!.url)}
              />
            ) : (
              <ListRow title="Web unavailable" icon="globe-outline" disabled accessibilityLabel="DSH Web unavailable" />
            )
          ) : urls.length > 0 ? (
            urls.map((item) => (
              <ListRow
                key={item.key}
                title={item.label}
                subtitle={item.address}
                icon="open-outline"
                accessory="chevron"
                accessibilityLabel={`Open ${item.label} URL for port ${service.port}`}
                onPress={() => onOpenURL(item.url)}
              />
            ))
          ) : (
            <ListRow title="Bound locally" value={serviceBindLabel(service)} icon="link-outline" />
          )}
          {hasServiceTerminal(service) ? (
            <ListRow
              title="Open Session terminal"
              icon="terminal-outline"
              accessory="chevron"
              accessibilityLabel={`Open terminal for port ${service.port}`}
              onPress={() => onOpenTerminal(service)}
            />
          ) : null}
        </ListSection>

        {tunnel.available ? (
          <ListSection
            title="Public access"
            footer={tunnel.active ? null : "A temporary Quick Tunnel URL that anyone with the link can open."}
          >
            {tunnel.publicURL ? (
              <ListRow
                title="Open public URL"
                subtitle={tunnel.publicURL}
                icon="globe-outline"
                accessory="chevron"
                accessibilityLabel="Open public URL"
                onPress={() => onOpenURL(tunnel.publicURL!)}
              />
            ) : null}
            {tunnel.publicURL ? (
              <ListRow
                title={copied ? "Copied" : "Copy public URL"}
                icon={copied ? "checkmark" : "copy-outline"}
                accessibilityLabel="Copy public URL"
                onPress={() => void copyPublicURL(tunnel.publicURL!)}
              />
            ) : null}
            <ListRow
              title={tunnel.busy ? "Working…" : tunnel.active ? "Stop public tunnel" : "Share with Quick Tunnel"}
              subtitle={tunnel.active && !tunnel.publicURL ? capitalize(tunnel.tunnel?.status) : null}
              icon={tunnel.active ? "stop-circle-outline" : "share-outline"}
              destructive={tunnel.active}
              loading={tunnel.busy}
              accessibilityLabel={tunnel.active ? "Stop public tunnel" : "Start temporary public tunnel"}
              onPress={tunnel.active ? tunnel.stop : tunnel.start}
            />
          </ListSection>
        ) : null}
        {tunnel.error ? (
          <InlineNotice tone="danger" title="Tunnel unavailable" detail={tunnel.error} style={styles.notice} />
        ) : null}

        <ListSection title="Details">
          <ListRow title="Process" value={serviceProcessLabel(service)} />
          {commandDetail ? (
            <ListRow title="Command" subtitle={commandDetail} numberOfLines={1} />
          ) : null}
          <ListRow title="Bind" value={serviceBindLabel(service)} />
          {persistent && statusDetail ? (
            <ListRow title="Status" subtitle={statusDetail} />
          ) : null}
        </ListSection>
      </ScrollView>
    </>
  );
}

function capitalize(value?: string): string | null {
  if (!value) return null;
  return value.charAt(0).toUpperCase() + value.slice(1);
}

const styles = StyleSheet.create({
  sheetRoot: {
    position: "absolute",
    top: 0,
    right: 0,
    bottom: 0,
    left: 0,
    width: "100%",
    minWidth: "100%",
  },
  sheetContent: {
    width: "100%",
    paddingBottom: 8,
    minWidth: 0,
  },
  header: {
    minHeight: 52,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 4,
    marginBottom: 12,
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
  },
  notice: {
    marginBottom: 16,
  },
  empty: {
    paddingVertical: 32,
  },
  scroll: {
    flexGrow: 0,
  },
  list: {
    paddingBottom: 8,
  },
  section: {
    marginBottom: 18,
  },
  refreshing: {
    textAlign: "center",
    paddingVertical: 4,
  },
  portTile: {
    minWidth: 46,
    height: 30,
    paddingHorizontal: 6,
    borderRadius: 9,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
  portText: {
    fontFamily: Typography.terminalFont,
    fontSize: 13,
    lineHeight: 17,
  },
});
