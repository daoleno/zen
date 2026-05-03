import React, {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  NativeSyntheticEvent,
  StyleSheet,
  TextInput,
  type TextInputSelectionChangeEventData,
} from "react-native";
import { Colors, Spacing, Typography, useAppColors } from "../../constants/tokens";
import {
  mentionCandidateValue,
  type MentionCandidate,
} from "./MentionPicker";

export function activeMention(
  value: string,
  pos: number,
): { query: string; start: number } | null {
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

export type ActiveMention = { query: string; start: number };

export type MarkdownEditorHandle = {
  insertMention(candidate: MentionCandidate): void;
  focus(): void;
};

type Props = {
  value: string;
  onChange: (text: string) => void;
  onActiveMentionChange?: (mention: ActiveMention | null) => void;
  onBlur?: () => void;
  autoFocus?: boolean;
};

export const MarkdownEditor = forwardRef<MarkdownEditorHandle, Props>(
  function MarkdownEditor(
    { value, onChange, onActiveMentionChange, onBlur, autoFocus },
    forwardedRef,
  ) {
    const colors = useAppColors();
    const styles = useMemo(() => createStyles(colors), [colors]);
    const inputRef = useRef<TextInput>(null);
    const selectionRef = useRef({ start: value.length, end: value.length });
    const [controlledSelection, setControlledSelection] = useState<
      { start: number; end: number } | undefined
    >(undefined);

    const notifyMention = useCallback(
      (nextValue: string, pos: number) => {
        onActiveMentionChange?.(activeMention(nextValue, pos));
      },
      [onActiveMentionChange],
    );

    const handleChangeText = useCallback(
      (next: string) => {
        onChange(next);
        const pos = Math.min(selectionRef.current.start, next.length);
        notifyMention(next, pos);
      },
      [notifyMention, onChange],
    );

    const handleSelectionChange = useCallback(
      (event: NativeSyntheticEvent<TextInputSelectionChangeEventData>) => {
        selectionRef.current = event.nativeEvent.selection;
        notifyMention(value, event.nativeEvent.selection.start);
      },
      [notifyMention, value],
    );

    useImperativeHandle(
      forwardedRef,
      () => ({
        insertMention(candidate) {
          const pos = selectionRef.current.start;
          const current = activeMention(value, pos);
          if (!current) {
            return;
          }
          const insert = `${mentionCandidateValue(candidate)} `;
          const before = value.slice(0, current.start);
          const after = value.slice(pos);
          const nextValue = before + insert + after;
          const nextPos = current.start + insert.length;

          onChange(nextValue);
          selectionRef.current = { start: nextPos, end: nextPos };
          setControlledSelection({ start: nextPos, end: nextPos });
          notifyMention(nextValue, nextPos);
          requestAnimationFrame(() => inputRef.current?.focus());
        },
        focus() {
          inputRef.current?.focus();
        },
      }),
      [notifyMention, onChange, value],
    );

    // Release the controlled selection on the next tick so further user
    // typing/navigation moves the caret naturally.
    useEffect(() => {
      if (!controlledSelection) {
        return;
      }
      const handle = setTimeout(() => setControlledSelection(undefined), 0);
      return () => clearTimeout(handle);
    }, [controlledSelection]);

    return (
      <TextInput
        ref={inputRef}
        value={value}
        onChangeText={handleChangeText}
        onSelectionChange={handleSelectionChange}
        selection={controlledSelection}
        onBlur={onBlur}
        multiline
        autoFocus={autoFocus}
        placeholder="# Issue title\n\nDescribe the work..."
        placeholderTextColor={colors.textSecondary}
        style={styles.input}
        textAlignVertical="top"
        autoCapitalize="none"
        autoCorrect={false}
        scrollEnabled
      />
    );
  },
);

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  input: {
    flex: 1,
    paddingHorizontal: 18,
    paddingVertical: Spacing.lg,
    color: colors.textPrimary,
    fontFamily: Typography.terminalFont,
    fontSize: 15,
    lineHeight: 22,
    backgroundColor: "transparent",
  },
  });
}
