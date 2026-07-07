export function normalizePath(value?: string): string {
  return value?.trim().replace(/\/+$/, "") || "";
}

export function isLikelyPath(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) {
    return false;
  }
  if (
    trimmed.startsWith("/") ||
    trimmed.startsWith("~/") ||
    trimmed.startsWith("./")
  ) {
    return true;
  }
  return /^[\w.-]+(\/[\w.-]+)+$/.test(trimmed);
}

export function compactPathLabel(
  value?: string,
  options?: { tailSegments?: number; showFullUpTo?: number },
): string {
  const tailSegments = options?.tailSegments ?? 2;
  const showFullUpTo = options?.showFullUpTo ?? 2;
  const trimmed = normalizePath(value);
  if (!trimmed || trimmed === "/") {
    return trimmed;
  }

  const absolute = trimmed.startsWith("/");
  const parts = trimmed.split("/").filter(Boolean);
  if (parts.length === 0) {
    return trimmed;
  }
  if (parts.length <= showFullUpTo) {
    return absolute ? `/${parts.join("/")}` : parts.join("/");
  }
  return `…/${parts.slice(-tailSegments).join("/")}`;
}

export function compactCommandLabel(value: string): string {
  const trimmed = value.replace(/\s+/g, " ").trim();
  if (!trimmed) {
    return "";
  }
  if (isLikelyPath(trimmed)) {
    return compactPathLabel(trimmed);
  }
  return trimmed.replace(
    /(^|\s)(\/[^\s]+|~\/[^\s]+)/g,
    (_, prefix: string, path: string) => `${prefix}${compactPathLabel(path)}`,
  );
}

export function displayPathSubtitle(value?: string): string {
  const trimmed = value?.trim() || "";
  if (!trimmed) {
    return "";
  }
  if (isLikelyPath(trimmed)) {
    return compactPathLabel(trimmed);
  }
  return compactCommandLabel(trimmed);
}