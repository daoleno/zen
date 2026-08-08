import React, { useEffect } from "react";
import { StyleSheet, View, type TextInput as TextInputInstance } from "react-native";
import Reanimated, {
  useAnimatedStyle,
  useReducedMotion,
  useSharedValue,
  withSpring,
} from "react-native-reanimated";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { shadow } from "../../constants/tokens";
import type { ComposerModelControlPresentation } from "../../services/providers/sessionModelHelpers";
import { ComposerIconButton } from "./ComposerIconButton";
import { ComposerModelChip } from "./ComposerModelChip";
import { ComposerSendButton } from "./ComposerSendButton";
import { COMPOSER_ACTION_SLOT_WIDTH } from "./composerActionSlot";
import {
  COMPOSER_ACTION_BAND_VERTICAL_PADDING,
  COMPOSER_MODEL_CHIP_LEFT_INSET,
  COMPOSER_MODEL_CHIP_RIGHT_INSET,
  COMPOSER_SPRING_CONFIG,
  composerActionBandHeight,
  composerExpansionRadius,
  composerExpansionTarget,
  composerInputHorizontalPadding,
  composerModelChipReveal,
  composerMotionDisabled,
} from "./composerExpansionMetrics";
import { InterfaceComposerInput } from "./InterfaceComposerInput";

interface InterfaceComposerExpandingDockProps {
  inputRef: React.RefObject<TextInputInstance | null>;
  draft: string;
  placeholder: string;
  editable: boolean;
  focused: boolean;
  uploading: boolean;
  sendEnabled: boolean;
  sending: boolean;
  sendLabel: string;
  showStopButton: boolean;
  stopEnabled: boolean;
  stopLabel: string;
  stopLoading: boolean;
  providerActivityStartedAt?: string;
  actionMenuExpanded: boolean;
  actionMenuButtonEnabled: boolean;
  showActionMenuButton: boolean;
  actionMenuIcon: "add" | "happy-outline";
  modelControl?: ComposerModelControlPresentation | null;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onDraftChange(value: string): void;
  onActionMenuPress(): void;
  onModelControlPress?(): void;
  onInputFocus(): void;
  onInputBlur(): void;
  onSendPress(): void;
  onStopPress(): void;
}

/**
 * The one shared mobile Composer capsule. `focused` drives a single
 * Reanimated progress; the action band height, corner radius, input-region
 * padding, bottom-anchored button translation, and model-control reveal all
 * derive from it. No remount, no competing LayoutAnimation owner.
 */
export function InterfaceComposerExpandingDock({
  inputRef,
  draft,
  placeholder,
  editable,
  focused,
  uploading,
  sendEnabled,
  sending,
  sendLabel,
  showStopButton,
  stopEnabled,
  stopLabel,
  stopLoading,
  providerActivityStartedAt,
  actionMenuExpanded,
  actionMenuButtonEnabled,
  showActionMenuButton,
  actionMenuIcon,
  modelControl,
  chrome,
  theme,
  onDraftChange,
  onActionMenuPress,
  onModelControlPress,
  onInputFocus,
  onInputBlur,
  onSendPress,
  onStopPress,
}: InterfaceComposerExpandingDockProps) {
  const reducedMotion = useReducedMotion();
  const progress = useSharedValue(0);

  useEffect(() => {
    const target = composerExpansionTarget(focused);
    if (composerMotionDisabled(reducedMotion)) {
      progress.value = target;
      return;
    }
    progress.value = withSpring(target, COMPOSER_SPRING_CONFIG);
  }, [focused, progress, reducedMotion]);

  const capsuleStyle = useAnimatedStyle(() => ({
    borderRadius: composerExpansionRadius(progress.value),
  }));
  const bandStyle = useAnimatedStyle(() => ({
    height: composerActionBandHeight(progress.value),
  }));
  const inputRegionStyle = useAnimatedStyle(() => {
    const padding = composerInputHorizontalPadding(progress.value);
    return { paddingLeft: padding.left, paddingRight: padding.right };
  });
  const chipRevealStyle = useAnimatedStyle(() => {
    const reveal = composerModelChipReveal(progress.value);
    return {
      opacity: reveal.opacity,
      transform: [{ translateY: reveal.translateY }],
    };
  });

  const actionButton = (
    <ComposerSendButton
      accessibilityLabel={showStopButton ? stopLabel : sendLabel}
      icon={showStopButton ? "square" : "arrow-up"}
      chrome={chrome}
      theme={theme}
      enabled={showStopButton ? stopEnabled : sendEnabled}
      loading={showStopButton ? stopLoading : sending}
      running={showStopButton}
      elapsedStartedAt={providerActivityStartedAt}
      fixedWidth={COMPOSER_ACTION_SLOT_WIDTH}
      onPress={showStopButton ? onStopPress : onSendPress}
    />
  );

  return (
    <Reanimated.View
      collapsable={false}
      testID="composer-expanding-dock"
      style={[
        styles.capsule,
        capsuleStyle,
        {
          backgroundColor: chrome.composerInput,
          borderColor: chrome.border,
          ...shadow("card", chrome.shadowColor),
        },
      ]}
    >
      <Reanimated.View style={[styles.inputRegion, inputRegionStyle]}>
        <InterfaceComposerInput
          inputRef={inputRef}
          draft={draft}
          placeholder={placeholder}
          editable={editable}
          chrome={chrome}
          onDraftChange={onDraftChange}
          onInputFocus={onInputFocus}
          onInputBlur={onInputBlur}
        />
      </Reanimated.View>

      <Reanimated.View pointerEvents="none" style={[styles.band, bandStyle]} />

      {modelControl && onModelControlPress ? (
        <Reanimated.View
          accessibilityElementsHidden={!focused}
          importantForAccessibility={focused ? "auto" : "no-hide-descendants"}
          pointerEvents={focused ? "auto" : "none"}
          style={[styles.modelChipSlot, chipRevealStyle]}
        >
          <ComposerModelChip
            label={modelControl.label}
            accessibilityLabel={modelControl.accessibilityLabel}
            chrome={chrome}
            onPress={onModelControlPress}
          />
        </Reanimated.View>
      ) : null}

      {showActionMenuButton ? (
        <View style={styles.actionSlotLeft}>
          <ComposerIconButton
            accessibilityLabel={
              actionMenuExpanded
                ? "Hide composer actions"
                : "Show composer actions"
            }
            icon={actionMenuExpanded ? "close" : actionMenuIcon}
            chrome={chrome}
            loading={uploading}
            disabled={!actionMenuButtonEnabled}
            iconColor={
              actionMenuExpanded
                ? chrome.accent
                : actionMenuButtonEnabled
                  ? chrome.textMuted
                  : chrome.textSubtle
            }
            onPress={onActionMenuPress}
          />
        </View>
      ) : null}

      <View style={styles.actionSlotRight}>{actionButton}</View>
    </Reanimated.View>
  );
}

const styles = StyleSheet.create({
  capsule: {
    flexDirection: "column",
    borderWidth: StyleSheet.hairlineWidth,
  },
  inputRegion: {
    paddingTop: COMPOSER_ACTION_BAND_VERTICAL_PADDING,
    paddingBottom: COMPOSER_ACTION_BAND_VERTICAL_PADDING,
  },
  band: {
    height: 0,
  },
  modelChipSlot: {
    position: "absolute",
    left: COMPOSER_MODEL_CHIP_LEFT_INSET,
    right: COMPOSER_MODEL_CHIP_RIGHT_INSET,
    bottom: COMPOSER_ACTION_BAND_VERTICAL_PADDING,
    height: 44,
    justifyContent: "center",
    alignItems: "flex-start",
  },
  actionSlotLeft: {
    position: "absolute",
    left: COMPOSER_ACTION_BAND_VERTICAL_PADDING,
    bottom: COMPOSER_ACTION_BAND_VERTICAL_PADDING,
  },
  actionSlotRight: {
    position: "absolute",
    right: COMPOSER_ACTION_BAND_VERTICAL_PADDING,
    bottom: COMPOSER_ACTION_BAND_VERTICAL_PADDING,
  },
});
