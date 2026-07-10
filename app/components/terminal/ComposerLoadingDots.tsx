import React from "react";
import { ZenLoopSpinner } from "../ui/ZenLoopSpinner";

interface ComposerLoadingDotsProps {
  color: string;
  size?: number;
  gap?: number;
}
export function ComposerLoadingDots({
  size = 10,
}: ComposerLoadingDotsProps) {
  const footprint = Math.max(size * 2.2, size + 8);
  return <ZenLoopSpinner size={footprint} />;
}
