import { useCallback, useEffect, useRef, useState } from "react";
import {
  Keyboard,
  Platform,
  useWindowDimensions,
  type KeyboardEvent,
  type LayoutChangeEvent,
} from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

interface UseTerminalAccessoryLayoutInput {
  accessoryVisible: boolean;
  ctrlResetKey?: string | null;
  ctrlDisabled?: boolean;
}

export function useTerminalAccessoryLayout({
  accessoryVisible,
  ctrlResetKey,
  ctrlDisabled = false,
}: UseTerminalAccessoryLayoutInput) {
  const insets = useSafeAreaInsets();
  const { height: windowHeight } = useWindowDimensions();
  const [keyboardVisible, setKeyboardVisible] = useState(false);
  const [androidKeyboardInset, setAndroidKeyboardInset] = useState(0);
  const [accessoryHeight, setAccessoryHeight] = useState(68);
  const [ctrlArmed, setCtrlArmed] = useState(false);
  const keyboardHeightRef = useRef(0);
  const baseWindowHeightRef = useRef(windowHeight);

  useEffect(() => {
    const handleShow = (event: KeyboardEvent) => {
      keyboardHeightRef.current = event?.endCoordinates?.height ?? 0;
      setKeyboardVisible(true);
    };
    const handleHide = () => {
      keyboardHeightRef.current = 0;
      setAndroidKeyboardInset(0);
      setKeyboardVisible(false);
    };

    const showSub = Keyboard.addListener("keyboardDidShow", handleShow);
    const hideSub = Keyboard.addListener("keyboardDidHide", handleHide);
    return () => {
      showSub.remove();
      hideSub.remove();
    };
  }, []);

  useEffect(() => {
    if (!keyboardVisible) {
      baseWindowHeightRef.current = windowHeight;
    }
  }, [keyboardVisible, windowHeight]);

  useEffect(() => {
    if (!keyboardVisible || Platform.OS !== "android") return;

    const keyboardHeight = keyboardHeightRef.current;
    if (!keyboardHeight) return;

    const adjustResizeHandled = Math.max(
      0,
      baseWindowHeightRef.current - windowHeight,
    );
    const remainingInset = Math.max(0, keyboardHeight - adjustResizeHandled);
    setAndroidKeyboardInset((previous) =>
      Math.abs(previous - remainingInset) <= 1 ? previous : remainingInset,
    );
  }, [keyboardVisible, windowHeight]);

  useEffect(() => {
    if (!keyboardVisible) {
      setCtrlArmed(false);
    }
  }, [keyboardVisible]);

  useEffect(() => {
    setCtrlArmed(false);
  }, [ctrlResetKey]);

  useEffect(() => {
    if (ctrlDisabled) {
      setCtrlArmed(false);
    }
  }, [ctrlDisabled]);

  const handleCtrlArmedChange = useCallback((next: boolean) => {
    setCtrlArmed(next);
  }, []);

  const handleAccessoryLayout = useCallback((event: LayoutChangeEvent) => {
    const nextHeight = Math.ceil(event.nativeEvent.layout.height);
    setAccessoryHeight((previous) =>
      Math.abs(previous - nextHeight) <= 1 ? previous : nextHeight,
    );
  }, []);

  // Both platforms use an absolute dock. iOS uses the keyboard event height
  // directly; Android subtracts the portion already handled by window resize.
  const accessoryBottomOffset = Platform.OS === "ios"
    ? (keyboardVisible ? keyboardHeightRef.current : insets.bottom)
    : (keyboardVisible ? androidKeyboardInset + insets.bottom + 6 : insets.bottom);
  const outputBottomInset = accessoryVisible
    ? accessoryHeight + accessoryBottomOffset
    : 0;

  return {
    keyboardVisible,
    ctrlArmed,
    accessoryBottomOffset,
    outputBottomInset,
    handleCtrlArmedChange,
    handleAccessoryLayout,
  };
}
