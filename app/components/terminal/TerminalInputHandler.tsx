import React, { forwardRef, useCallback, useImperativeHandle, useRef } from 'react';
import { Platform, TextInput, StyleSheet } from 'react-native';
import { applyCtrlModifier } from './terminalControl';
import {
  diffTerminalInput,
  encodeTerminalInputDelta,
  trimTrailingInputUnits,
} from './terminalInputBuffer';

export interface TerminalInputHandleRef {
  focus(): void;
  blur(): void;
  clear(): void;
}

interface TerminalInputHandlerProps {
  onInput: (data: string) => void;
  ctrlArmed: boolean;
  onCtrlConsumed: () => void;
}

/**
 * Hidden TextInput for keyboard/IME capture.
 * Keeps IME capture in React Native so the renderer stays display-only.
 */
export const TerminalInputHandler = forwardRef<TerminalInputHandleRef, TerminalInputHandlerProps>(
  ({ onInput, ctrlArmed, onCtrlConsumed }, ref) => {
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

        if (previous === text) {
          return;
        }

        if (text.startsWith(previous)) {
          const appendedText = text.slice(previous.length);
          if (!appendedText) {
            return;
          }

          if (ctrlArmed) {
            const appendedUnits = Array.from(appendedText);
            onInput(applyCtrlModifier(appendedUnits[0]));
            onCtrlConsumed();
            if (appendedUnits.length > 1) {
              onInput(appendedUnits.slice(1).join(''));
            }
            clearInput();
            return;
          }

          onInput(appendedText);
          return;
        }

        if (ctrlArmed) {
          const delta = diffTerminalInput(previous, text);
          const insertedUnits = Array.from(delta.insertedText);

          if (delta.backspaces > 0) {
            onInput('\x7f'.repeat(delta.backspaces));
          }

          if (insertedUnits.length === 0) {
            return;
          }

          // Apply Ctrl modifier to the first inserted character only, then
          // reset the local mirror because the terminal state may diverge.
          onInput(applyCtrlModifier(insertedUnits[0]));
          onCtrlConsumed();
          if (insertedUnits.length > 1) {
            onInput(insertedUnits.slice(1).join(''));
          }
          clearInput();
        } else {
          const payload = encodeTerminalInputDelta(diffTerminalInput(previous, text));
          if (!payload) {
            return;
          }
          onInput(payload);
        }
      },
      [clearInput, ctrlArmed, onInput, onCtrlConsumed],
    );

    const handleKeyPress = useCallback(
      (e: { nativeEvent: { key: string } }) => {
        const { key } = e.nativeEvent;

        // Keys that either do not flow through onChangeText or would leave the
        // hidden TextInput mirror out of sync with the terminal state.
        switch (key) {
          case 'Enter':
            handledSubmitRef.current = true;
            onInput('\r');
            clearInput();
            return;
          case 'Backspace':
            lastTextRef.current = trimTrailingInputUnits(lastTextRef.current, 1);
            onInput('\x7f');
            return;
          case 'Tab':
            onInput('\t');
            clearInput();
            return;
          case 'Escape':
            onInput('\x1b');
            clearInput();
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
        keyboardType={Platform.OS === 'android' ? 'visible-password' : 'default'}
        disableFullscreenUI
        importantForAutofill="no"
        selectTextOnFocus={false}
        underlineColorAndroid="transparent"
        blurOnSubmit={false}
        editable
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
    opacity: 0,
    left: -1000,
    top: -1000,
    color: 'transparent',
    backgroundColor: 'transparent',
  },
});
