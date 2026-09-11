import { MERMAID_CACHE_LIMIT } from "./mermaidLimits";
import type { MermaidRenderTheme } from "./mermaidTheme";

export type MermaidCachedSvg = {
  svg: string;
  width: number;
  height: number;
};

const cache = new Map<string, MermaidCachedSvg>();

export function mermaidCacheKey(
  source: string,
  theme: MermaidRenderTheme,
) {
  return `${theme.mode}:${theme.foreground}:${theme.primaryColor}:${source}`;
}

export function readMermaidCache(key: string): MermaidCachedSvg | null {
  const value = cache.get(key);
  if (!value) {
    return null;
  }
  cache.delete(key);
  cache.set(key, value);
  return value;
}

export function writeMermaidCache(key: string, value: MermaidCachedSvg) {
  cache.delete(key);
  cache.set(key, value);
  while (cache.size > MERMAID_CACHE_LIMIT) {
    const oldest = cache.keys().next().value;
    if (oldest === undefined) {
      break;
    }
    cache.delete(oldest);
  }
}

export function clearMermaidCache() {
  cache.clear();
}
