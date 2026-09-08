import { requireNativeViewManager } from "expo-modules-core";
import type { ComponentType, Ref } from "react";
import type { StyleProp, ViewStyle } from "react-native";
import type { DesktopCommandTarget } from "../../../services/remoteDesktopCommands";

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
  ref?: Ref<DesktopCommandTarget>;
  onState: (event: { nativeEvent: DesktopState }) => void;
}

export const NativeDesktopView: ComponentType<DesktopViewProps> =
  requireNativeViewManager("ZenRemoteDesktop");
