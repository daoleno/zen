import React, { useCallback, useRef, useState } from "react";
import {
  NativeSyntheticEvent,
  StyleSheet,
  TextInput,
  type TextInputSelectionChangeEventData,
  View,
} from "react-native";
import { Colors, Typography } from "../../constants/tokens";
import {
  MentionPicker,
  mentionCandidateValue,
  type MentionCandidate,
} from "./MentionPicker";

export function activeMention(value: string, pos: number): { query: string; start: number } | null {
  let cursor = pos - 1;
  while (cursor >= 0) {
    const char = value[cursor];
    if (char === "@") {
      if (cursor === 0 || /\s/.test(value[cursor - 1])) {
        return {
          query: value.slice(cursor + 1, pos),
          start: cursor,
        };
      }
      return null;
    }
    if (!/[a-z0-9-]/i.test(char)) {
      return null;
    }
    cursor -= 1;
  }
  return null;
}

export function MarkdownEditor({
  value,
  onChange,
  candidates,
  autoFocus,
}: {
  value: string;
  onChange: (text: string) => void;
  candidates: MentionCandidate[];
  autoFocus?: boolean;
}) {
  const ref = useRef<TextInput>(null);
  const [selection, setSelection] = useState({ start: 0, end: 0 });

  const onSelectionChange = useCallback(
    (event: NativeSyntheticEvent<TextInputSelectionChangeEventData>) => {
      setSelection(event.nativeEvent.selection);
    },
    [],
  );

  const mention = activeMention(value, selection.start);

  const handleSelectMention = useCallback(
    (candidate: MentionCandidate) => {
      if (!mention) {
        return;
      }
      const insert = `${mentionCandidateValue(candidate)} `;
      const before = value.slice(0, mention.start);
      const after = value.slice(selection.start);
      onChange(before + insert + after);
      requestAnimationFrame(() => ref.current?.focus());
    },
    [mention, onChange, selection.start, value],
  );

  return (
    <View style={styles.container}>
      <TextInput
        ref={ref}
        value={value}
        onChangeText={onChange}
        onSelectionChange={onSelectionChange}
        multiline
        autoFocus={autoFocus}
        placeholder="# Issue title\n\nDescribe the work..."
        placeholderTextColor={Colors.textSecondary}
        style={styles.input}
        textAlignVertical="top"
        autoCapitalize="none"
        autoCorrect={false}
      />
      {mention ? (
        <MentionPicker
          candidates={candidates}
          query={mention.query}
          onSelect={handleSelectMention}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  input: {
    flex: 1,
    paddingHorizontal: 18,
    paddingVertical: 16,
    color: Colors.textPrimary,
    fontFamily: Typography.terminalFont,
    fontSize: 15,
    lineHeight: 22,
  },
});
