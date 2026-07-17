export function resolveAppVersion(value: unknown): string {
  if (typeof value !== "string") {
    return "dev";
  }
  const version = value.trim();
  return version || "dev";
}
