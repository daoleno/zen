import React from "react";
import { Path, Svg } from "react-native-svg";

// Pi Coding Agent mark mirrored from the official open-source brand geometry
// published at https://pi.dev/logo.svg (and logo-auto.svg) for badlogic/pi-mono
// (MIT), matching the LobeHub Pi Mono paths in lobe-icons
// (src/Pi/components/Mono.tsx, MIT). @lobehub/icons-rn@2.7.1 does not export Pi.

const PI_MARK_PATHS = [
  "M1 1h16.5v11H12v5.5H6.5V23H1V1zm5.5 5.5V12H12V6.5H6.5z",
  "M17.5 12H23v11h-5.5V12z",
] as const;

interface PiMarkProps {
  size?: number;
  color?: string;
}

export function PiMark({ size = 24, color = "currentColor" }: PiMarkProps) {
  return (
    <Svg width={size} height={size} viewBox="0 0 24 24" fill="none">
      <Path
        clipRule="evenodd"
        fillRule="evenodd"
        d={PI_MARK_PATHS[0]}
        fill={color}
      />
      <Path d={PI_MARK_PATHS[1]} fill={color} />
    </Svg>
  );
}
