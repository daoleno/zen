import React from "react";
import Svg, { Circle, Path } from "react-native-svg";

interface PrimaryNavIconProps {
  color: string;
  size?: number;
}

const STROKE = 1.75;

function iconSize(size: number | undefined): number {
  return size ?? 22;
}

/** Minimal two-line menu glyph for the primary drawer control. */
export function NavMenuIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M5 9.25h14"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M5 14.75h14"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
    </Svg>
  );
}

/** Quiet outline close mark for the drawer. */
export function NavCloseIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M7 7l10 10"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M17 7L7 17"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
    </Svg>
  );
}

/** Restrained outline chevron for drawer rows. */
export function NavChevronIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M9.5 6.5L15.5 12l-6 5.5"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </Svg>
  );
}

/** Outline bar chart for Stats. */
export function NavStatsIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M6.5 16.5v-4"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M12 16.5V7.5"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M17.5 16.5v-7"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
    </Svg>
  );
}

/** Three stacked cards for the Skills catalog and inventory. */
export function NavSkillsIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path d="M7 7.5h10" stroke={color} strokeWidth={STROKE} strokeLinecap="round" />
      <Path d="M7 12h10" stroke={color} strokeWidth={STROKE} strokeLinecap="round" />
      <Path d="M7 16.5h10" stroke={color} strokeWidth={STROKE} strokeLinecap="round" />
      <Circle cx={4.5} cy={7.5} r={0.9} fill={color} />
      <Circle cx={4.5} cy={12} r={0.9} fill={color} />
      <Circle cx={4.5} cy={16.5} r={0.9} fill={color} />
    </Svg>
  );
}

/** Outline sliders for Settings — stroke-only, no fill. */
export function NavSettingsIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Path
        d="M5 8h2.2"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M11.8 8H19"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M5 16h8.2"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Path
        d="M17.8 16H19"
        stroke={color}
        strokeWidth={STROKE}
        strokeLinecap="round"
      />
      <Circle
        cx={9}
        cy={8}
        r={2.1}
        stroke={color}
        strokeWidth={STROKE}
        fill="none"
      />
      <Circle
        cx={15}
        cy={16}
        r={2.1}
        stroke={color}
        strokeWidth={STROKE}
        fill="none"
      />
    </Svg>
  );
}

/** Vertical overflow glyph for primary page actions. */
export function NavOverflowIcon({ color, size }: PrimaryNavIconProps) {
  const dim = iconSize(size);
  return (
    <Svg width={dim} height={dim} viewBox="0 0 24 24" fill="none">
      <Circle cx={12} cy={6.5} r={1.15} fill={color} />
      <Circle cx={12} cy={12} r={1.15} fill={color} />
      <Circle cx={12} cy={17.5} r={1.15} fill={color} />
    </Svg>
  );
}
