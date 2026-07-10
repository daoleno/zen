import React, {
  createContext,
  useCallback,
  useContext,
  useId,
  useLayoutEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { Pressable, StyleSheet, View } from "react-native";
import { useIsFocused } from "expo-router";
import { useAppColors } from "../../constants/tokens";
import { NavOverflowIcon } from "./PrimaryNavIcons";
import {
  clearPrimaryPageAction,
  registerPrimaryPageAction,
  type PrimaryPageActionRegistration,
} from "./primaryPageActionState";

export const PRIMARY_PAGE_ACTION_SLOT_SIZE = 52;
export const PRIMARY_PAGE_ACTION_HIT_SIZE = 44;

/**
 * Compact action for the global top-right slot.
 * Prefer this over a raw node when you only need one overflow/control button.
 */
export interface PrimaryPageActionDescriptor {
  accessibilityLabel: string;
  disabled?: boolean;
  /** Defaults to the shared vertical overflow glyph. */
  icon?: ReactNode;
  onPress(): void;
}

type PrimaryPageActionContent =
  | { kind: "descriptor"; value: PrimaryPageActionDescriptor }
  | { kind: "node"; value: ReactNode };

type PrimaryPageActionEntry =
  PrimaryPageActionRegistration<PrimaryPageActionContent>;

interface PrimaryPageActionDispatch {
  clear(ownerId: string): void;
  register(ownerId: string, content: PrimaryPageActionContent): void;
}

const PrimaryPageActionStateContext =
  createContext<PrimaryPageActionEntry | null>(null);

const PrimaryPageActionDispatchContext =
  createContext<PrimaryPageActionDispatch | null>(null);

function isPrimaryPageActionDescriptor(
  action: ReactNode | PrimaryPageActionDescriptor,
): action is PrimaryPageActionDescriptor {
  if (action == null || typeof action !== "object" || React.isValidElement(action)) {
    return false;
  }
  const candidate = action as PrimaryPageActionDescriptor;
  return (
    typeof candidate.accessibilityLabel === "string" &&
    typeof candidate.onPress === "function"
  );
}

function toPageActionContent(
  action: ReactNode | PrimaryPageActionDescriptor,
): PrimaryPageActionContent | null {
  if (action == null || action === false) {
    return null;
  }
  if (isPrimaryPageActionDescriptor(action)) {
    return { kind: "descriptor", value: action };
  }
  return { kind: "node", value: action };
}

function samePageActionContent(
  left: PrimaryPageActionContent,
  right: PrimaryPageActionContent,
): boolean {
  if (left.kind !== right.kind) {
    return false;
  }
  if (left.kind === "node" && right.kind === "node") {
    return left.value === right.value;
  }
  if (left.kind === "descriptor" && right.kind === "descriptor") {
    return (
      left.value.accessibilityLabel === right.value.accessibilityLabel &&
      left.value.disabled === right.value.disabled &&
      left.value.icon === right.value.icon &&
      left.value.onPress === right.value.onPress
    );
  }
  return false;
}

export function PrimaryPageActionProvider({
  children,
}: {
  children: ReactNode;
}) {
  const [entry, setEntry] = useState<PrimaryPageActionEntry | null>(null);
  const register = useCallback(
    (ownerId: string, content: PrimaryPageActionContent) => {
      setEntry((current) => {
        if (
          current != null &&
          current.ownerId === ownerId &&
          samePageActionContent(current.content, content)
        ) {
          return current;
        }
        return registerPrimaryPageAction(current, ownerId, content);
      });
    },
    [],
  );
  const clear = useCallback((ownerId: string) => {
    setEntry((current) => clearPrimaryPageAction(current, ownerId));
  }, []);
  const dispatch = useMemo<PrimaryPageActionDispatch>(
    () => ({ clear, register }),
    [clear, register],
  );

  return (
    <PrimaryPageActionDispatchContext.Provider value={dispatch}>
      <PrimaryPageActionStateContext.Provider value={entry}>
        {children}
      </PrimaryPageActionStateContext.Provider>
    </PrimaryPageActionDispatchContext.Provider>
  );
}

/**
 * Register the focused primary page's single top-right action.
 * Clears automatically on blur and unmount. Owner-scoped clear so a newly
 * focused sibling tab cannot be wiped by the previous page's cleanup.
 *
 * Pass either a descriptor or a custom node that fits the 52×52 slot.
 * Memoize descriptors/nodes when the parent re-renders often.
 */
export function usePrimaryPageAction(
  action: ReactNode | PrimaryPageActionDescriptor | null,
): void {
  const dispatch = useContext(PrimaryPageActionDispatchContext);
  const ownerId = useId();
  const focused = useIsFocused();

  useLayoutEffect(() => {
    if (dispatch == null) {
      return;
    }
    if (!focused) {
      dispatch.clear(ownerId);
      return;
    }
    const content = toPageActionContent(action);
    if (content == null) {
      dispatch.clear(ownerId);
      return;
    }
    dispatch.register(ownerId, content);
    return () => {
      dispatch.clear(ownerId);
    };
  }, [action, dispatch, focused, ownerId]);

  if (dispatch == null) {
    throw new Error(
      "usePrimaryPageAction must be used inside PrimaryDrawerShell",
    );
  }
}

interface PrimaryAppBarPageActionProps {
  drawerVisible: boolean;
}

function DescriptorActionButton({
  descriptor,
  drawerVisible,
}: {
  descriptor: PrimaryPageActionDescriptor;
  drawerVisible: boolean;
}) {
  const colors = useAppColors();
  const disabled = Boolean(descriptor.disabled);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={descriptor.accessibilityLabel}
      accessibilityState={{ disabled }}
      disabled={disabled}
      hitSlop={6}
      onPress={descriptor.onPress}
      tabIndex={drawerVisible ? -1 : 0}
      style={({ pressed }) => [
        styles.actionButton,
        pressed && !disabled ? styles.pressedIcon : null,
      ]}
    >
      {descriptor.icon ?? (
        <NavOverflowIcon
          color={
            disabled ? colors.disabledText : colors.textPrimary
          }
          size={20}
        />
      )}
    </Pressable>
  );
}

/** Renders the registered page action, or an empty 52px spacer. */
export function PrimaryAppBarPageAction({
  drawerVisible,
}: PrimaryAppBarPageActionProps) {
  const entry = useContext(PrimaryPageActionStateContext);
  if (entry == null) {
    return <View style={styles.slot} accessibilityElementsHidden />;
  }
  if (entry.content.kind === "node") {
    return <View style={styles.slot}>{entry.content.value}</View>;
  }
  return (
    <View style={styles.slot}>
      <DescriptorActionButton
        descriptor={entry.content.value}
        drawerVisible={drawerVisible}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  slot: {
    width: PRIMARY_PAGE_ACTION_SLOT_SIZE,
    minWidth: PRIMARY_PAGE_ACTION_HIT_SIZE,
    minHeight: PRIMARY_PAGE_ACTION_SLOT_SIZE,
    alignItems: "center",
    justifyContent: "center",
  },
  actionButton: {
    width: PRIMARY_PAGE_ACTION_SLOT_SIZE,
    minWidth: PRIMARY_PAGE_ACTION_HIT_SIZE,
    minHeight: PRIMARY_PAGE_ACTION_SLOT_SIZE,
    borderRadius: 10,
    alignItems: "center",
    justifyContent: "center",
  },
  pressedIcon: {
    opacity: 0.55,
  },
});
