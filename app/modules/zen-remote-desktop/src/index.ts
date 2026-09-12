import { requireNativeViewManager } from "expo-modules-core";
import type { ComponentType, Ref } from "react";
import type { StyleProp, ViewStyle } from "react-native";
import type { DesktopCommandTarget } from "../../../services/remoteDesktopCommands";

export interface DesktopState {
  state: "sources" | "requesting" | "streaming" | "connected" | "disconnected" | "denied" | "unsupported";
  reason?: string;
  source?: string;
  width?: number;
  height?: number;
  control?: boolean;
  sensitiveInput?: boolean;
  inputError?: string;
  surface?: "desktop" | "locked" | "greeter";
  presented?: number;
  dropped?: number;
}

export interface DesktopViewProps {
  style?: StyleProp<ViewStyle>;
  connection: string;
  ref?: Ref<DesktopCommandTarget>;
  onState: (event: { nativeEvent: DesktopState }) => void;
}

export const NativeDesktopView: ComponentType<DesktopViewProps> =
  requireNativeViewManager("ZenRemoteDesktop");

export interface DesktopKeyboardApi {
  focus(): Promise<void>;
  clear(): Promise<void>;
}

export interface DesktopKeyboardProps {
  style?: StyleProp<ViewStyle>;
  ref?: Ref<DesktopKeyboardApi>;
  // Event names must match the native `Events("onDesktopText", "onDesktopKey")`
  // registration on both platforms; the contract test enforces the pair.
  onDesktopText: (event: { nativeEvent: { value: string } }) => void;
  onDesktopKey: (event: { nativeEvent: { key: "Backspace" | "Enter" | "Delete" } }) => void;
}

/** The exact native view event names; the native source contract test checks them. */
export const DESKTOP_KEYBOARD_EVENTS = ["onDesktopText", "onDesktopKey"] as const;

/**
 * Commit-aware phone keyboard. Null on a native shell built before this view
 * existed; the route then falls back to the plain TextInput capture surface.
 * The native view is the only path that keeps IME composing text local.
 */
export const NativeDesktopKeyboard: ComponentType<DesktopKeyboardProps> | null = (() => {
  try {
    const expo = (globalThis as {
      expo?: { getViewConfig?: (module: string, view?: string) => unknown };
    }).expo;
    if (!expo || typeof expo.getViewConfig !== "function" ||
        expo.getViewConfig("ZenRemoteDesktop", "DesktopKeyboardView") == null) return null;
    return requireNativeViewManager<DesktopKeyboardProps>("ZenRemoteDesktop", "DesktopKeyboardView");
  } catch {
    return null;
  }
})();

export interface MoonlightDesktopState {
  state:
    | "connecting"
    | "start_accepted"
    | "start_failed"
    | "stage"
    | "connected"
    | "status"
    | "frame"
    | "rejected"
    | "failed"
    | "disconnected"
    | "revoked";
  reason?: string;
  generation?: number;
  width?: number;
  height?: number;
  presented?: number;
  dropped?: number;
  connected?: boolean;
}

export interface MoonlightDesktopApi {
  disconnect(generation: number): Promise<boolean>;
  revoke(generation: number): Promise<boolean>;
  sendKey(generation: number, keyCode: number, keyAction: number, modifiers: number, flags: number): Promise<boolean>;
  sendText(generation: number, value: string): Promise<boolean>;
  sendPointerMove(generation: number, deltaX: number, deltaY: number): Promise<boolean>;
  sendPointerPosition(generation: number, x: number, y: number, referenceWidth: number, referenceHeight: number): Promise<boolean>;
  sendPointerButton(generation: number, button: number, action: number): Promise<boolean>;
  sendScroll(generation: number, clicks: number): Promise<boolean>;
}

export interface MoonlightDesktopProps {
  style?: StyleProp<ViewStyle>;
  /** JSON connection document: host/ports/uniqueId/appId/pin/resolution. */
  connection: string;
  onState: (event: { nativeEvent: MoonlightDesktopState }) => void;
  ref?: Ref<MoonlightDesktopApi>;
}

/**
 * Paired Moonlight view. Null on native shells that predate the view; callers
 * then keep using the existing WebSocket route.
 */
export const NativeMoonlightDesktopView: ComponentType<MoonlightDesktopProps> | null = (() => {
  try {
    const expo = (globalThis as {
      expo?: { getViewConfig?: (module: string, view?: string) => unknown };
    }).expo;
    if (!expo || typeof expo.getViewConfig !== "function" ||
        expo.getViewConfig("ZenRemoteDesktop", "MoonlightDesktopView") == null) return null;
    return requireNativeViewManager<MoonlightDesktopProps>("ZenRemoteDesktop", "MoonlightDesktopView");
  } catch {
    return null;
  }
})();
