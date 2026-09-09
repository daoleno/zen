import React, { useMemo } from "react";
import { View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { MermaidDiagram } from "./MermaidDiagram";
import { splitMarkdownMermaidSegments } from "./mermaidFences";

export function MarkdownWithMermaid({
  markdown,
  chrome,
  theme,
  compact = false,
  streaming = false,
  renderMarkdown,
}: {
  markdown: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
  streaming?: boolean;
  renderMarkdown: (value: string) => React.ReactNode;
}) {
  const segments = useMemo(
    () => splitMarkdownMermaidSegments(markdown),
    [markdown],
  );
  if (!segments.some((segment) => segment.type === "mermaid")) {
    return renderMarkdown(markdown);
  }
  return (
    <View>
      {segments.map((segment, index) => {
        if (segment.type === "markdown") {
          return <View key={`md:${index}`}>{renderMarkdown(segment.text)}</View>;
        }
        const fence = segment.closed
          ? `\`\`\`mermaid\n${segment.source}\n\`\`\``
          : `\`\`\`mermaid\n${segment.source}`;
        return (
          <MermaidDiagram
            key={`mermaid:${index}`}
            source={segment.source}
            closed={segment.closed}
            streaming={streaming}
            chrome={chrome}
            theme={theme}
            compact={compact}
            fallback={renderMarkdown(fence)}
          />
        );
      })}
    </View>
  );
}
