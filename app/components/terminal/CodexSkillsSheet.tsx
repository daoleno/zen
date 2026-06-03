import { Ionicons } from "@expo/vector-icons";
import React, {
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  type CodexSkill,
  wsClient,
} from "../../services/websocket";
import { BottomSheetFrame } from "../ui";
import { ComposerLoadingDots } from "./ComposerLoadingDots";

interface CodexSkillsSheetProps {
  visible: boolean;
  serverId: string;
  cwd?: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSelectSkill(skill: CodexSkill): void;
  onClose(): void;
}

export function CodexSkillsSheet({
  visible,
  serverId,
  cwd,
  chrome,
  theme,
  onSelectSkill,
  onClose,
}: CodexSkillsSheetProps) {
  const [query, setQuery] = useState("");
  const [skills, setSkills] = useState<CodexSkill[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const searchInputRef = useRef<TextInput | null>(null);

  useEffect(() => {
    if (!visible) {
      searchInputRef.current?.blur();
      return;
    }
    const timer = setTimeout(() => {
      searchInputRef.current?.focus();
    }, 90);
    return () => clearTimeout(timer);
  }, [visible]);

  useEffect(() => {
    if (!visible) {
      return;
    }
    let cancelled = false;
    setQuery("");
    setLoading(true);
    setError(null);
    void wsClient
      .getCodexSkills(serverId, { cwd })
      .then((snapshot) => {
        if (cancelled) {
          return;
        }
        setSkills(snapshot.skills);
      })
      .catch((err: any) => {
        if (cancelled) {
          return;
        }
        setSkills([]);
        setError(err?.message || "Could not load skills.");
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [cwd, serverId, visible]);

  const visibleSkills = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    if (!normalized) {
      return skills;
    }
    return skills.filter((skill) => {
      const haystack = [
        skill.name,
        skill.description || "",
        skill.scope,
        skill.path,
      ].join(" ").toLowerCase();
      return haystack.includes(normalized);
    });
  }, [query, skills]);

  return (
    <BottomSheetFrame
      visible={visible}
      maxHeight="84%"
      cardStyle={styles.sheet}
      contentStyle={styles.content}
      keyboardAvoiding
      onClose={onClose}
    >
      <View style={styles.header}>
        <View style={styles.headerCopy}>
          <Text style={[styles.title, { color: chrome.text }]}>Skills</Text>
        </View>
        <TouchableOpacity
          accessibilityLabel="Close skills"
          accessibilityRole="button"
          style={[styles.closeButton, { backgroundColor: chrome.surfaceMuted }]}
          onPress={onClose}
          activeOpacity={0.78}
        >
          <Ionicons name="close" size={18} color={chrome.textSubtle} />
        </TouchableOpacity>
      </View>

      <View
        style={[
          styles.search,
          { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
        ]}
      >
        <Ionicons name="search-outline" size={15} color={chrome.textSubtle} />
        <TextInput
          ref={searchInputRef}
          value={query}
          onChangeText={setQuery}
          placeholder="Search skills"
          placeholderTextColor={chrome.textSubtle}
          selectionColor={chrome.accent}
          autoCapitalize="none"
          autoCorrect={false}
          autoFocus={visible}
          returnKeyType="search"
          style={[styles.searchInput, { color: chrome.text }]}
        />
        {query ? (
          <TouchableOpacity
            accessibilityLabel="Clear skill search"
            accessibilityRole="button"
            hitSlop={8}
            onPress={() => setQuery("")}
            activeOpacity={0.72}
          >
            <Ionicons name="close-circle" size={16} color={chrome.textSubtle} />
          </TouchableOpacity>
        ) : null}
      </View>

      {loading ? (
        <View style={styles.state}>
          <ComposerLoadingDots color={chrome.accent} size={5} gap={4} />
          <Text style={[styles.stateText, { color: chrome.textSubtle }]}>
            Loading skills
          </Text>
        </View>
      ) : error ? (
        <View style={styles.state}>
          <Ionicons name="warning-outline" size={18} color={chrome.textSubtle} />
          <Text style={[styles.stateText, { color: chrome.textSubtle }]}>
            {error}
          </Text>
        </View>
      ) : visibleSkills.length === 0 ? (
        <View style={styles.state}>
          <Ionicons name="construct-outline" size={18} color={chrome.textSubtle} />
          <Text style={[styles.stateText, { color: chrome.textSubtle }]}>
            No matching skills
          </Text>
        </View>
      ) : (
        <ScrollView
          style={styles.list}
          contentContainerStyle={styles.listContent}
          keyboardShouldPersistTaps="handled"
          showsVerticalScrollIndicator={false}
        >
          {visibleSkills.map((skill) => (
            <TouchableOpacity
              key={`${skill.path}:${skill.name}`}
              accessibilityLabel={`Use ${skill.name}`}
              accessibilityRole="button"
              style={[
                styles.row,
                { borderBottomColor: chrome.border },
              ]}
              onPress={() => onSelectSkill(skill)}
              activeOpacity={0.78}
            >
              <View
                style={[
                  styles.skillIcon,
                  { backgroundColor: chrome.surfaceMuted },
                ]}
              >
                <Text
                  style={[
                    styles.skillGlyph,
                    { color: skill.enabled ? chrome.accent : chrome.textSubtle },
                  ]}
                >
                  $
                </Text>
              </View>
              <View style={styles.rowCopy}>
                <View style={styles.rowTitleLine}>
                  <Text
                    style={[styles.skillName, { color: chrome.text }]}
                    numberOfLines={1}
                  >
                    {skill.name}
                  </Text>
                  <Text
                    style={[
                      styles.scope,
                      { color: scopeColor(skill.scope, chrome, theme) },
                    ]}
                    numberOfLines={1}
                  >
                    {skill.scope}
                  </Text>
                </View>
                {skill.description ? (
                  <Text
                    style={[styles.description, { color: chrome.textSubtle }]}
                    numberOfLines={2}
                  >
                    {skill.description}
                  </Text>
                ) : null}
              </View>
              <Ionicons name="add" size={17} color={chrome.textSubtle} />
            </TouchableOpacity>
          ))}
        </ScrollView>
      )}
    </BottomSheetFrame>
  );
}

function scopeColor(
  scope: string,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
) {
  switch (scope) {
    case "repo":
      return theme.brightGreen || chrome.accent;
    case "system":
      return chrome.textMuted;
    default:
      return chrome.accent;
  }
}

const styles = StyleSheet.create({
  sheet: {
    paddingHorizontal: 14,
    paddingTop: 10,
    paddingBottom: 18,
  },
  content: {
    flexShrink: 1,
    minHeight: 0,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    paddingHorizontal: 4,
    paddingBottom: 10,
  },
  headerCopy: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    fontSize: 17,
    lineHeight: 22,
    fontFamily: Typography.uiFontMedium,
  },
  closeButton: {
    width: 34,
    height: 34,
    borderRadius: 17,
    alignItems: "center",
    justifyContent: "center",
  },
  search: {
    height: 40,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingHorizontal: 10,
    marginBottom: 8,
    zIndex: 2,
    elevation: 2,
  },
  searchInput: {
    flex: 1,
    minWidth: 0,
    height: 38,
    paddingTop: 0,
    paddingBottom: 0,
    paddingVertical: 0,
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
    includeFontPadding: false,
    textAlignVertical: "center",
  },
  list: {
    flexShrink: 1,
    minHeight: 0,
    maxHeight: 430,
  },
  listContent: {
    paddingBottom: 18,
  },
  row: {
    minHeight: 62,
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    paddingHorizontal: 3,
    paddingVertical: 9,
    borderBottomWidth: StyleSheet.hairlineWidth,
  },
  skillIcon: {
    width: 30,
    height: 30,
    borderRadius: 8,
    alignItems: "center",
    justifyContent: "center",
  },
  skillGlyph: {
    fontSize: 17,
    lineHeight: 20,
    fontFamily: Typography.terminalFontBold,
  },
  rowCopy: {
    flex: 1,
    minWidth: 0,
  },
  rowTitleLine: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
  },
  skillName: {
    flex: 1,
    minWidth: 0,
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
  },
  scope: {
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFontMedium,
    textTransform: "uppercase",
  },
  description: {
    marginTop: 2,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
  state: {
    minHeight: 148,
    alignItems: "center",
    justifyContent: "center",
    gap: 8,
    paddingHorizontal: 20,
  },
  stateText: {
    fontSize: 13,
    lineHeight: 18,
    textAlign: "center",
    fontFamily: Typography.uiFont,
  },
});
