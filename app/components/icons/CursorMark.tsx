import React from "react";
import { Path, Svg } from "react-native-svg";

// Single-path Cursor Mono shape mirrored from the web @lobehub/icons
// Cursor Mono asset (package es/Cursor/components/Mono.js), so the Zen
// mobile icon matches the LobeHub web icons page.
const CURSOR_MONO_PATH =
  "M22.106 5.68L12.5.135a.998.998 0 00-.998 0L1.893 5.68a.84.84 0 00-.419.726v11.186c0 .3.16.577.42.727l9.607 5.547a.999.999 0 00.998 0l9.608-5.547a.84.84 0 00.42-.727V6.407a.84.84 0 00-.42-.726zm-.603 1.176L12.228 22.92c-.063.108-.228.064-.228-.061V12.34a.59.59 0 00-.295-.51l-9.11-5.26c-.107-.062-.063-.228.062-.228h18.55c.264 0 .428.286.296.514z";

interface CursorMarkProps {
  size?: number;
  color?: string;
}

export function CursorMark({ size = 24, color = "currentColor" }: CursorMarkProps) {
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24" fill="none">
      <Path d={CURSOR_MONO_PATH} fill={color} />
    </Svg>
  );
}
