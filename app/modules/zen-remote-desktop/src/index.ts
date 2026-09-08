import { requireNativeViewManager } from "expo-modules-core";
import type { ComponentType } from "react";
import type { StyleProp, ViewStyle } from "react-native";

export interface DesktopState {
  state: "sources" | "requesting" | "streaming" | "connected" | "disconnected" | "denied" | "unsupported";
  reason?: string;
  width?: number;
  height?: number;
  control?: boolean;
  presented?: number;
  dropped?: number;
}

export interface DesktopViewProps {
  style?: StyleProp<ViewStyle>;
  connection: string;
  command: string;
  onState: (event: { nativeEvent: DesktopState }) => void;
}

export const NativeDesktopView: ComponentType<DesktopViewProps> =
  requireNativeViewManager("ZenRemoteDesktop");
