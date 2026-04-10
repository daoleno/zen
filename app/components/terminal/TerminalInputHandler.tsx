import React, { forwardRef, useCallback, useImperativeHandle, useRef } from 'react';
import { TextInput, StyleSheet } from 'react-native';
import { applyCtrlModifier } from './terminalControl';

export interface TerminalInputHandleRef {
  focus(): void;
  blur(): void;
  clear(): void;
}

interface TerminalInputHandlerProps {
  onInput: (data: string) => void;
  ctrlArmed: boolean;
  onCtrlConsumed: () => void;
  disabled: boolean;
}

/**
 * Hidden TextInput for keyboard/IME capture.
 * Replaces the hidden textarea in the xterm.js WebView.
 *
 * React Native's TextInput handles IME composition natively,
 * eliminating the 100+ lines of xterm.js composition patches.
 */
export const TerminalInputHandler = forwardRef<TerminalInputHandleRef, TerminalInputHandlerProps>(
  ({ onInput, ctrlArmed, onCtrlConsumed, disabled }, ref) => {
    const inputRef = useRef<TextInput>(null);
    const lastTextRef = useRef('');
    const handledSubmitRef = useRef(false);

    const clearInput = useCallback(() => {
      lastTextRef.current = '';
      inputRef.current?.clear();
    }, []);

    useImperativeHandle(ref, () => ({
      focus() {
        inputRef.current?.focus();
      },
      blur() {
        inputRef.current?.blur();
      },
      clear() {
        clearInput();
      },
    }), [clearInput]);

    const handleChangeText = useCallback(
      (text: string) => {
        const previous = lastTextRef.current;
        lastTextRef.current = text;

        if (!text) {
          return;
        }

        let sharedPrefixLength = 0;
        const sharedLength = Math.min(previous.length, text.length);
        while (
          sharedPrefixLength < sharedLength &&
          previous[sharedPrefixLength] === text[sharedPrefixLength]
        ) {
          sharedPrefixLength += 1;
        }

        const appended = text.slice(sharedPrefixLength);
        if (!appended) {
          return;
        }

        if (ctrlArmed) {
          // Apply Ctrl modifier to the first inserted character only.
          const modified = applyCtrlModifier(appended[0]);
          onInput(modified);
          onCtrlConsumed();
          if (appended.length > 1) {
            onInput(appended.slice(1));
          }
        } else {
          onInput(appended);
        }

      },
      [ctrlArmed, onInput, onCtrlConsumed],
    );

    const handleKeyPress = useCallback(
      (e: { nativeEvent: { key: string } }) => {
        const { key } = e.nativeEvent;

        // Special keys that don't trigger onChangeText
        switch (key) {
          case 'Enter':
            handledSubmitRef.current = true;
            onInput('\r');
            clearInput();
            return;
          case 'Backspace':
            onInput('\x7f');
            return;
          case 'Tab':
            onInput('\t');
            return;
          case 'Escape':
            onInput('\x1b');
            return;
        }
      },
      [clearInput, onInput],
    );

    return (
      <TextInput
        ref={inputRef}
        style={styles.hiddenInput}
        autoCorrect={false}
        autoCapitalize="none"
        autoComplete="off"
        spellCheck={false}
        keyboardType="default"
        blurOnSubmit={false}
        editable={!disabled}
        onChangeText={handleChangeText}
        onKeyPress={handleKeyPress}
        onSubmitEditing={() => {
          if (handledSubmitRef.current) {
            handledSubmitRef.current = false;
            return;
          }
          onInput('\r');
          clearInput();
        }}
        onBlur={clearInput}
        caretHidden
        contextMenuHidden
      />
    );
  },
);

const styles = StyleSheet.create({
  hiddenInput: {
    position: 'absolute',
    width: 1,
    height: 1,
    opacity: 0.01,
    left: 0,
    top: 0,
    color: 'transparent',
    backgroundColor: 'transparent',
  },
});
