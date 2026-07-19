import React from "react";
import MaskedView from "@react-native-masked-view/masked-view";
import { LinearGradient } from "expo-linear-gradient";
import { Platform, StyleSheet, View, type ViewStyle } from "react-native";
import Reanimated, {
  useAnimatedStyle,
  type SharedValue,
} from "react-native-reanimated";
import {
  structuredChatContentFadeGeometry,
  structuredChatNativeMaskColors,
} from "./chatKeyboardOverlayPolicy";

const WEB_MASK_VISIBLE = "rgba(255, 255, 255, 1)";
const WEB_MASK_HIDDEN = "rgba(255, 255, 255, 0)";
const AnimatedLinearGradient =
  Reanimated.createAnimatedComponent(LinearGradient);

type WebMaskStyle = ViewStyle & {
  maskImage: string;
  WebkitMaskImage: string;
  maskMode: "alpha";
  WebkitMaskMode: "alpha";
  maskRepeat: "no-repeat";
  WebkitMaskRepeat: "no-repeat";
  maskSize: "100% 100%";
  WebkitMaskSize: "100% 100%";
};

interface StructuredChatContentFadeProps {
  canvasColor: string;
  composerHeight: SharedValue<number>;
  overlayTranslateY: SharedValue<number>;
  children: React.ReactNode;
}

/**
 * Masks timeline pixels only. The mask is transparent below the Composer, so
 * the continuous page canvas shows through without drawing a scrim or band.
 */
export function StructuredChatContentFade({
  canvasColor,
  composerHeight,
  overlayTranslateY,
  children,
}: StructuredChatContentFadeProps) {
  const nativeMask = structuredChatNativeMaskColors(canvasColor);
  const opaqueMaskStyle = useAnimatedStyle(() => {
    const geometry = structuredChatContentFadeGeometry(
      composerHeight.value,
      overlayTranslateY.value,
    );
    return { bottom: geometry.opaqueBottomInset };
  });
  const fadeMaskStyle = useAnimatedStyle(() => {
    const geometry = structuredChatContentFadeGeometry(
      composerHeight.value,
      overlayTranslateY.value,
    );
    return {
      bottom: geometry.transparentBottomInset,
      height: geometry.fadeHeight,
    };
  });
  const webMaskStyle = useAnimatedStyle<WebMaskStyle>(() => {
    const geometry = structuredChatContentFadeGeometry(
      composerHeight.value,
      overlayTranslateY.value,
    );
    const maskImage = [
      "linear-gradient(to bottom,",
      `${WEB_MASK_VISIBLE} 0,`,
      `${WEB_MASK_VISIBLE} calc(100% - ${geometry.opaqueBottomInset}px),`,
      `${WEB_MASK_HIDDEN} calc(100% - ${geometry.transparentBottomInset}px),`,
      `${WEB_MASK_HIDDEN} 100%)`,
    ].join(" ");

    return {
      maskImage,
      WebkitMaskImage: maskImage,
      maskMode: "alpha",
      WebkitMaskMode: "alpha",
      maskRepeat: "no-repeat",
      WebkitMaskRepeat: "no-repeat",
      maskSize: "100% 100%",
      WebkitMaskSize: "100% 100%",
    };
  });

  if (Platform.OS === "web") {
    return (
      <Reanimated.View style={[styles.container, webMaskStyle]}>
        {children}
      </Reanimated.View>
    );
  }

  return (
    <MaskedView
      androidRenderingMode="software"
      style={styles.container}
      maskElement={
        <View pointerEvents="none" style={styles.maskCanvas}>
          <Reanimated.View
            style={[
              styles.opaqueMask,
              opaqueMaskStyle,
              { backgroundColor: nativeMask.visible },
            ]}
          />
          <AnimatedLinearGradient
            pointerEvents="none"
            colors={[nativeMask.visible, nativeMask.hidden]}
            locations={[0, 1]}
            start={{ x: 0.5, y: 0 }}
            end={{ x: 0.5, y: 1 }}
            style={[styles.fadeMask, fadeMaskStyle]}
          />
        </View>
      }
    >
      {children}
    </MaskedView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    minHeight: 0,
  },
  maskCanvas: {
    flex: 1,
  },
  opaqueMask: {
    position: "absolute",
    top: 0,
    right: 0,
    left: 0,
  },
  fadeMask: {
    position: "absolute",
    right: 0,
    left: 0,
  },
});
