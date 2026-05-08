import React, { useMemo, useState } from "react";
import {
  Pressable,
  SectionList,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { SafeAreaView } from "react-native-safe-area-context";
import {
  Colors,
  Spacing,
  Typography,
  useAppColors,
} from "../../constants/tokens";
import { WorkRow, workItemStatus } from "../../components/work/WorkRow";
import { useWork, type WorkItem } from "../../store/work";

type SectionItem =
  | { kind: "work"; key: string }
  | { kind: "done-toggle"; projectKey: string; count: number; expanded: boolean };

type WorkSection = {
  key: string;
  projectKey: string;
  title: string;
  subtitle: string | null;
  activeCount: number;
  doneCount: number;
  data: SectionItem[];
};

type WorkBucket = WorkSection & {
  latestTime: number;
  active: WorkItem[];
  done: WorkItem[];
};

export default function WorkScreen() {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const { state } = useWork();
  const [expandedDone, setExpandedDone] = useState<Record<string, boolean>>({});

  const workItems = useMemo(() => Object.values(state.byKey), [state.byKey]);
  const showServerPrefix = useMemo(() => {
    const ids = new Set(workItems.map((item) => item.serverId));
    return ids.size > 1;
  }, [workItems]);

  const sections = useMemo<WorkSection[]>(
    () => buildWorkSections(workItems, expandedDone, showServerPrefix),
    [expandedDone, showServerPrefix, workItems],
  );
  const summary = useMemo(() => workSummary(sections), [sections]);
  const isEmpty = sections.length === 0;

  const toggleDone = (projectKey: string) =>
    setExpandedDone((prev) => ({ ...prev, [projectKey]: !prev[projectKey] }));

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <View style={styles.header}>
        <View style={styles.headerCopy}>
          <Text style={styles.title}>Work</Text>
          <Text style={styles.subtitle}>{summary}</Text>
        </View>
      </View>

      {isEmpty ? (
        <View style={styles.emptyState}>
          <Text style={styles.emptyGlyph}>▤</Text>
          <Text style={styles.emptyTitle}>No work logged</Text>
          <Text style={styles.emptyBody}>
            Agent sessions will appear here after the daemon records them.
          </Text>
        </View>
      ) : (
        <SectionList
          sections={sections}
          keyExtractor={(item, index) =>
            item.kind === "done-toggle"
              ? `toggle:${item.projectKey}:${index}`
              : item.key
          }
          renderItem={({ item }) => {
            if (item.kind === "done-toggle") {
              return (
                <Pressable
                  onPress={() => toggleDone(item.projectKey)}
                  style={({ pressed }) => [
                    styles.doneToggle,
                    pressed && styles.doneTogglePressed,
                  ]}
                >
                  <Ionicons
                    name={item.expanded ? "chevron-down" : "chevron-forward"}
                    size={14}
                    color={colors.textSecondary}
                  />
                  <Text style={styles.doneToggleText}>
                    {item.expanded ? "hide log" : "show log"} · {item.count}
                  </Text>
                </Pressable>
              );
            }

            const workItem = state.byKey[item.key];
            return workItem ? <WorkRow item={workItem} /> : null;
          }}
          renderSectionHeader={({ section }) => (
            <View style={styles.sectionHeader}>
              <View style={styles.sectionHeaderCopy}>
                <View style={styles.sectionPrompt}>
                  <Text style={styles.sectionTitle} numberOfLines={1}>
                    {section.title}
                  </Text>
                  <Text style={styles.sectionArrow}>❯</Text>
                  <Text style={styles.sectionState}>{section.activeCount}</Text>
                </View>
                {section.subtitle ? (
                  <Text style={styles.sectionSubtitle} numberOfLines={1}>
                    {section.subtitle}
                  </Text>
                ) : null}
              </View>
            </View>
          )}
          SectionSeparatorComponent={() => <View style={styles.sectionSeparator} />}
          stickySectionHeadersEnabled={false}
          contentContainerStyle={styles.listContent}
          showsVerticalScrollIndicator={false}
          removeClippedSubviews={false}
          windowSize={15}
        />
      )}
    </SafeAreaView>
  );
}

function buildWorkSections(
  workItems: WorkItem[],
  expandedDone: Record<string, boolean>,
  showServerPrefix: boolean,
): WorkSection[] {
  const buckets: Record<string, WorkBucket> = {};

  const ensureBucket = (item: WorkItem) => {
    const project = item.project || "workspace";
    const key = `${item.serverId}:${project}`;
    if (!buckets[key]) {
      buckets[key] = {
        key,
        projectKey: key,
        title: project,
        subtitle: showServerPrefix ? item.serverName : null,
        activeCount: 0,
        doneCount: 0,
        data: [],
        latestTime: 0,
        active: [],
        done: [],
      };
    }
    return buckets[key];
  };

  for (const item of workItems) {
    const bucket = ensureBucket(item);
    if (isClosedWork(item)) {
      bucket.done.push(item);
    } else {
      bucket.active.push(item);
    }
    bucket.latestTime = Math.max(
      bucket.latestTime,
      timestampMillis(item.mtime || item.frontmatter.created),
    );
  }

  return Object.values(buckets)
    .map((bucket) => {
      const active = [...bucket.active].sort(sortWorkByTime);
      const done = [...bucket.done].sort(sortWorkByTime);
      const expanded = !!expandedDone[bucket.projectKey];
      const data: SectionItem[] = active.map((item) => ({
        kind: "work",
        key: item.key,
      }));
      if (done.length > 0) {
        data.push({
          kind: "done-toggle",
          projectKey: bucket.projectKey,
          count: done.length,
          expanded,
        });
        if (expanded) {
          data.push(...done.map((item) => ({ kind: "work" as const, key: item.key })));
        }
      }

      return {
        key: bucket.key,
        projectKey: bucket.projectKey,
        title: bucket.title,
        subtitle: bucket.subtitle,
        activeCount: active.length,
        doneCount: done.length,
        data,
        latestTime: bucket.latestTime,
      };
    })
    .filter((section) => section.data.length > 0)
    .sort((left, right) => right.latestTime - left.latestTime)
    .map(({ latestTime: _latestTime, ...section }) => section);
}

function isClosedWork(item: WorkItem): boolean {
  const status = workItemStatus(item);
  return status === "done" || status === "failed";
}

function sortWorkByTime(left: WorkItem, right: WorkItem): number {
  return timestampMillis(right.mtime || right.frontmatter.created) -
    timestampMillis(left.mtime || left.frontmatter.created);
}

function workSummary(sections: WorkSection[]): string {
  if (sections.length === 0) {
    return "daemon auto log";
  }

  const active = sections.reduce((sum, section) => sum + section.activeCount, 0);
  const logged = sections.reduce((sum, section) => sum + section.doneCount, 0);
  const parts = [`${active} active`];
  if (logged > 0) {
    parts.push(`${logged} logged`);
  }
  parts.push(`${sections.length} workspace${sections.length === 1 ? "" : "s"}`);
  return parts.join(" · ");
}

function timestampMillis(value?: string | number | null) {
  if (typeof value === "number" && Number.isFinite(value)) {
    return value > 10_000_000_000 ? value : value * 1000;
  }
  if (typeof value === "string" && value.trim()) {
    const parsed = Date.parse(value);
    return Number.isNaN(parsed) ? 0 : parsed;
  }
  return 0;
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    screen: {
      flex: 1,
      backgroundColor: colors.bgPrimary,
    },
    header: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      paddingHorizontal: 16,
      paddingTop: 8,
      paddingBottom: 10,
    },
    headerCopy: {
      flex: 1,
      minWidth: 0,
    },
    title: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 22,
      lineHeight: 28,
      letterSpacing: 0.6,
      opacity: 0.9,
    },
    subtitle: {
      marginTop: 2,
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 10,
      lineHeight: 13,
      opacity: 0.58,
    },
    listContent: {
      paddingHorizontal: 16,
      paddingTop: 6,
      paddingBottom: 28,
    },
    sectionHeader: {
      minHeight: 31,
      paddingTop: 7,
      paddingBottom: 3,
      flexDirection: "row",
      alignItems: "flex-end",
      gap: 10,
      backgroundColor: colors.bgPrimary,
    },
    sectionHeaderCopy: {
      flex: 1,
      minWidth: 0,
    },
    sectionPrompt: {
      flexDirection: "row",
      alignItems: "center",
      minWidth: 0,
    },
    sectionTitle: {
      maxWidth: "76%",
      flexShrink: 1,
      color: colors.promptGreen,
      fontFamily: Typography.terminalFont,
      fontSize: 13,
      lineHeight: 17,
      letterSpacing: 0,
    },
    sectionArrow: {
      marginHorizontal: 7,
      color: colors.promptYellow,
      fontFamily: Typography.terminalFontBold,
      fontSize: 12,
      lineHeight: 17,
    },
    sectionState: {
      color: colors.textSecondary,
      fontFamily: Typography.terminalFont,
      fontSize: 10,
      lineHeight: 14,
      opacity: 0.5,
    },
    sectionSubtitle: {
      marginTop: 2,
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 10,
      lineHeight: 12,
      opacity: 0.48,
    },
    sectionSeparator: {
      height: 4,
    },
    doneToggle: {
      flexDirection: "row",
      alignItems: "center",
      gap: Spacing.sm,
      height: 28,
      borderTopWidth: StyleSheet.hairlineWidth,
      borderTopColor: colors.borderSubtle,
    },
    doneTogglePressed: {
      opacity: 0.7,
    },
    doneToggleText: {
      color: colors.textSecondary,
      fontFamily: Typography.terminalFont,
      fontSize: 10,
      opacity: 0.56,
    },
    emptyState: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: Spacing.xl,
    },
    emptyGlyph: {
      color: colors.textSecondary,
      fontSize: 34,
      marginBottom: 12,
      opacity: 0.38,
    },
    emptyTitle: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 16,
    },
    emptyBody: {
      marginTop: Spacing.sm,
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      textAlign: "center",
      opacity: 0.58,
    },
  });
}
