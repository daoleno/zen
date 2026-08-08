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
 * Fades timeline pixels below the floating Composer without changing timeline
 * geometry. Android deliberately uses a sibling overlay instead of wrapping
 * the live FlatList in a software MaskedView: software masking rasterizes the
 * complete timeline after every streaming descendant update and can expose a
 * cleared black backing surface. Zen's chat canvas is a single flat color, so
 * covering the same pixels is visually equivalent and keeps the list on its
 * ordinary native composition layer. iOS retains its native alpha mask and
 * Web retains the CSS mask.
 */
export function StructuredChatContentFade(
  props: StructuredChatContentFadeProps,
) {
  if (Platform.OS === "android") {
    return <AndroidStructuredChatContentFade {...props} />;
  }
  if (Platform.OS === "web") {
    return <WebStructuredChatContentFade {...props} />;
  }
  return <IosStructuredChatContentFade {...props} />;
}

function AndroidStructuredChatContentFade({
  canvasColor,
  composerHeight,
  overlayTranslateY,
  children,
}: StructuredChatContentFadeProps) {
  const colors = structuredChatNativeMaskColors(canvasColor);
  const opaqueCoverStyle = useAnimatedStyle(() => {
    const geometry = structuredChatContentFadeGeometry(
      composerHeight.value,
      overlayTranslateY.value,
    );
    return { height: geometry.transparentBottomInset };
  });
  const fadeOverlayStyle = useAnimatedStyle(() => {
    const geometry = structuredChatContentFadeGeometry(
      composerHeight.value,
      overlayTranslateY.value,
    );
    return {
      bottom: geometry.transparentBottomInset,
      height: geometry.fadeHeight,
    };
  });

  return (
    <View style={styles.container}>
      {children}
      <AnimatedLinearGradient
        pointerEvents="none"
        colors={[colors.hidden, colors.visible]}
        locations={[0, 1]}
        start={{ x: 0.5, y: 0 }}
        end={{ x: 0.5, y: 1 }}
        style={[styles.fadeMask, fadeOverlayStyle]}
      />
      <Reanimated.View
        pointerEvents="none"
        style={[
          styles.opaqueCover,
          opaqueCoverStyle,
          { backgroundColor: canvasColor },
        ]}
      />
    </View>
  );
}

function IosStructuredChatContentFade({
  canvasColor,
  composerHeight,
  overlayTranslateY,
  children,
}: StructuredChatContentFadeProps) {
  const colors = structuredChatNativeMaskColors(canvasColor);
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

  return (
    <MaskedView
      style={styles.container}
      maskElement={
        <View pointerEvents="none" style={styles.maskCanvas}>
          <Reanimated.View
            style={[
              styles.opaqueMask,
              opaqueMaskStyle,
              { backgroundColor: colors.visible },
            ]}
          />
          <AnimatedLinearGradient
            pointerEvents="none"
            colors={[colors.visible, colors.hidden]}
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

function WebStructuredChatContentFade({
  composerHeight,
  overlayTranslateY,
  children,
}: StructuredChatContentFadeProps) {
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

  return (
    <Reanimated.View style={[styles.container, webMaskStyle]}>
      {children}
    </Reanimated.View>
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
  opaqueCover: {
    position: "absolute",
    right: 0,
    bottom: 0,
    left: 0,
  },
});
