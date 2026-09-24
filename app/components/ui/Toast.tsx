import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { AccessibilityInfo, Pressable, StyleSheet, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import Animated, { FadeInUp, FadeOutUp } from "react-native-reanimated";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { useAppColors } from "../../constants/tokens";
import { AppText } from "./AppText";
import { GlassSurface } from "./GlassSurface";

type ToastTone = "success" | "error" | "info";

export interface ToastOptions {
  title: string;
  detail?: string;
  tone?: ToastTone;
}

interface ToastEntry extends Required<Pick<ToastOptions, "title" | "tone">> {
  id: number;
  detail?: string;
}

const ToastContext = createContext<{ show(options: ToastOptions): void } | null>(null);
const VISIBLE_MS = 2600;

/**
 * Non-blocking confirmations and recoverable failures. Anything that needs a
 * decision stays an Alert.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<ToastEntry | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const nextId = useRef(0);

  const dismiss = useCallback(() => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
    setToast(null);
  }, []);

  const show = useCallback((options: ToastOptions) => {
    const tone = options.tone ?? "info";
    if (timer.current) clearTimeout(timer.current);
    nextId.current += 1;
    setToast({ id: nextId.current, title: options.title, detail: options.detail, tone });
    void Haptics.notificationAsync(
      tone === "error"
        ? Haptics.NotificationFeedbackType.Error
        : Haptics.NotificationFeedbackType.Success,
    );
    AccessibilityInfo.announceForAccessibility(
      options.detail ? `${options.title}. ${options.detail}` : options.title,
    );
    timer.current = setTimeout(() => {
      timer.current = null;
      setToast(null);
    }, VISIBLE_MS);
  }, []);

  useEffect(() => () => {
    if (timer.current) clearTimeout(timer.current);
  }, []);

  const value = useMemo(() => ({ show }), [show]);
  return (
    <ToastContext.Provider value={value}>
      {children}
      {toast ? <ToastView key={toast.id} toast={toast} onDismiss={dismiss} /> : null}
    </ToastContext.Provider>
  );
}

function ToastView({ toast, onDismiss }: { toast: ToastEntry; onDismiss(): void }) {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  const icon = toast.tone === "error" ? "alert-circle" : toast.tone === "success" ? "checkmark-circle" : "information-circle";
  const ink = toast.tone === "error" ? colors.dangerText : toast.tone === "success" ? colors.success : colors.accentStrong;
  return (
    <View pointerEvents="box-none" style={[styles.layer, { top: insets.top + 8 }]}>
      <Animated.View entering={FadeInUp.springify().damping(20)} exiting={FadeOutUp.duration(160)}>
        <Pressable onPress={onDismiss} accessibilityRole="alert" accessibilityLabel={toast.title}>
          <GlassSurface material="thick" radius={24} elevation="float" style={styles.toast}>
            <Ionicons name={icon} size={20} color={ink} />
            <View style={styles.copy}>
              <AppText variant="label" numberOfLines={2}>
                {toast.title}
              </AppText>
              {toast.detail ? (
                <AppText variant="caption" tone="secondary" numberOfLines={3}>
                  {toast.detail}
                </AppText>
              ) : null}
            </View>
          </GlassSurface>
        </Pressable>
      </Animated.View>
    </View>
  );
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) throw new Error("useToast must be used within ToastProvider");
  return context;
}

const styles = StyleSheet.create({
  layer: {
    position: "absolute",
    left: 16,
    right: 16,
    alignItems: "center",
    zIndex: 1000,
    elevation: 1000,
  },
  toast: {
    flexDirection: "row",
    alignItems: "center",
    gap: 10,
    minHeight: 48,
    maxWidth: 480,
    paddingHorizontal: 16,
    paddingVertical: 11,
  },
  copy: {
    flexShrink: 1,
  },
});
