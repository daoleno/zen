import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  ActivityIndicator,
  Linking,
  Pressable,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { EnrichedMarkdownText } from "react-native-enriched-markdown";
import { TypeScale, Typography, useAppColors } from "../../constants/tokens";
import { openSafeMarkdownUrl } from "../markdown/markdownLinks";
import type { PackageDetail } from "../../services/skillsManagement";
import {
  buildSkillFileTree,
  defaultSkillFile,
  skillRenderer,
  type SkillTreeNode,
} from "../../services/skillsScreenModel";
import { buildSkillsMarkdownStyle } from "../../services/skillsMarkdownPresentation";

/**
 * Shared read-only Skill content browser: expandable file tree plus inline
 * preview. Both the Skill inspector and Plugin inspector render this one
 * implementation so details never stack competing surfaces.
 */
export function SkillFileBrowser({
  detail,
  loading,
  error,
  onSelectFile,
}: {
  detail: PackageDetail;
  loading?: boolean;
  error?: string;
  onSelectFile(path: string): void;
}) {
  const colors = useAppColors();
  const files = useMemo(() => detail.files ?? [], [detail]);
  const tree = useMemo(() => buildSkillFileTree(files), [files]);
  const [selectedPath, setSelectedPath] = useState<string>();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const previousCopy = useRef<string | null>(null);
  useEffect(() => {
    const selected = detail.preview?.path ?? defaultSkillFile(files);
    setSelectedPath(selected);
    const changed = previousCopy.current !== detail.copyId;
    previousCopy.current = detail.copyId;
    if (changed)
      setExpanded(
        new Set(
          selected
            ?.split("/")
            .slice(0, -1)
            .map((_, index, parts) => parts.slice(0, index + 1).join("/")),
        ),
      );
  }, [detail.copyId, detail.preview?.path, detail.skillName, files]);
  useEffect(() => {
    if (detail.preview?.path) setSelectedPath(detail.preview.path);
  }, [detail.preview?.path]);
  const preview = detail.preview;
  const renderer = preview
    ? skillRenderer(preview.kind, preview.content)
    : null;
  return (
    <View>
      {tree.length ? (
        tree.map((node) => (
          <TreeNode
            key={node.path}
            node={node}
            depth={0}
            expanded={expanded}
            selected={selectedPath}
            onToggle={(path) =>
              setExpanded((current) => {
                const next = new Set(current);
                next.has(path) ? next.delete(path) : next.add(path);
                return next;
              })
            }
            onSelect={(path) => {
              setSelectedPath(path);
              onSelectFile(path);
            }}
          />
        ))
      ) : (
        <Text style={{ color: colors.textTertiary }}>
          This copy contains no readable files.
        </Text>
      )}
      <View style={[styles.preview, { borderTopColor: colors.borderSubtle }]}>
        <Text style={[styles.fileTitle, { color: colors.textPrimary }]}>
          {selectedPath || "No file selected"}
        </Text>
        {loading ? (
          <View style={styles.loadingRow}>
            <ActivityIndicator size="small" color={colors.accent} />
            <Text style={{ color: colors.textSecondary }}>Loading file</Text>
          </View>
        ) : null}
        {error ? (
          <BrowserState
            icon="warning-outline"
            title="File unavailable"
            detail={error}
          />
        ) : null}
        {preview?.notice ? (
          <Text style={[styles.previewNotice, { color: colors.warning }]}>
            {preview.notice}
          </Text>
        ) : null}
        {preview?.status === "binary" ? (
          <BrowserState
            icon="document-attach-outline"
            title="Binary file"
            detail={`${preview.mediaType} · ${preview.size} bytes. Content preview is unavailable.`}
          />
        ) : null}
        {renderer === "markdown" ? (
          <MarkdownContent content={preview?.content ?? ""} />
        ) : null}
        {renderer === "json" ? (
          <Code
            content={JSON.stringify(
              JSON.parse(preview?.content ?? "null"),
              null,
              2,
            )}
          />
        ) : null}
        {renderer === "invalid-json" ? (
          <>
            <Text style={[styles.previewNotice, { color: colors.warning }]}>
              Invalid JSON; showing original text.
            </Text>
            <Code content={preview?.content ?? ""} />
          </>
        ) : null}
        {renderer === "text" ? <Code content={preview?.content ?? ""} /> : null}
      </View>
    </View>
  );
}

function MarkdownContent({ content }: { content: string }) {
  const colors = useAppColors();
  const markdownStyle = useMemo(
    () =>
      buildSkillsMarkdownStyle(colors, {
        body: Typography.uiFont,
        bodyMedium: Typography.uiFontMedium,
        mono: Typography.terminalFont,
      }),
    [colors],
  );
  return (
    <EnrichedMarkdownText
      markdown={content}
      markdownStyle={markdownStyle}
      selectable
      onLinkPress={(event) =>
        void openSafeMarkdownUrl(event.url, (url) => Linking.openURL(url))
      }
    />
  );
}

function BrowserState({
  icon,
  title,
  detail,
}: {
  icon: React.ComponentProps<typeof Ionicons>["name"];
  title: string;
  detail: string;
}) {
  const colors = useAppColors();
  return (
    <View style={styles.browserState}>
      <Ionicons name={icon} size={22} color={colors.textTertiary} />
      <Text style={[styles.browserStateTitle, { color: colors.textPrimary }]}>
        {title}
      </Text>
      <Text style={[styles.browserStateDetail, { color: colors.textTertiary }]}>
        {detail}
      </Text>
    </View>
  );
}

function TreeNode({
  node,
  depth,
  expanded,
  selected,
  onToggle,
  onSelect,
}: {
  node: SkillTreeNode;
  depth: number;
  expanded: Set<string>;
  selected?: string;
  onToggle(path: string): void;
  onSelect(path: string): void;
}) {
  const colors = useAppColors();
  const open = expanded.has(node.path);
  return (
    <>
      <Pressable
        accessibilityRole="button"
        accessibilityState={
          node.kind === "directory"
            ? { expanded: open }
            : { selected: selected === node.path }
        }
        onPress={() =>
          node.kind === "directory" ? onToggle(node.path) : onSelect(node.path)
        }
        style={[
          styles.treeRow,
          {
            paddingLeft: 8 + depth * 16,
            backgroundColor:
              selected === node.path ? colors.accentSoft : "transparent",
          },
        ]}
      >
        <Ionicons
          name={
            node.kind === "directory"
              ? open
                ? "folder-open-outline"
                : "folder-outline"
              : node.file?.kind === "binary"
                ? "document-attach-outline"
                : "document-text-outline"
          }
          size={17}
          color={
            node.kind === "directory" ? colors.warning : colors.textTertiary
          }
        />
        <Text
          numberOfLines={1}
          style={{
            color:
              selected === node.path ? colors.accent : colors.textSecondary,
            flex: 1,
          }}
        >
          {node.name}
        </Text>
      </Pressable>
      {node.kind === "directory" && open
        ? node.children.map((child) => (
            <TreeNode
              key={child.path}
              node={child}
              depth={depth + 1}
              expanded={expanded}
              selected={selected}
              onToggle={onToggle}
              onSelect={onSelect}
            />
          ))
        : null}
    </>
  );
}

function Code({ content }: { content: string }) {
  const colors = useAppColors();
  return (
    <ScrollView horizontal>
      <Text selectable style={[styles.code, { color: colors.textSecondary }]}>
        {content || "This file is empty."}
      </Text>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  preview: {
    marginTop: 8,
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingTop: 12,
    minHeight: 160,
  },
  fileTitle: {
    ...TypeScale.compact,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 7,
  },
  previewNotice: { ...TypeScale.compact, marginBottom: 8 },
  loadingRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingVertical: 8,
  },
  browserState: { alignItems: "center", gap: 6, paddingVertical: 14 },
  browserStateTitle: {
    ...TypeScale.body,
    fontFamily: Typography.uiFontMedium,
    textAlign: "center",
  },
  browserStateDetail: {
    ...TypeScale.compact,
    textAlign: "center",
    maxWidth: 360,
  },
  treeRow: {
    minHeight: 38,
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
    paddingRight: 8,
    borderRadius: 4,
  },
  code: {
    fontFamily: Typography.terminalFont,
    fontSize: 13,
    lineHeight: 20,
    paddingVertical: 8,
  },
});
