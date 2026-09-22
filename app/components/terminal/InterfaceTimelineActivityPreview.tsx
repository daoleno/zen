import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { imageReference } from "../../services/imageSource";
import { ZenImage } from "./ZenImage";

export function ActivityPreview({ path, chrome }: { path: string; failed?: boolean; chrome: TerminalThemeChrome }) {
  return <ZenImage source={imageReference(path, path.split("/").pop() || "Tool image")} chrome={chrome} />;
}
