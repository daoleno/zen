/**
 * Keep one declarative owner for the chat frame throughout a keyboard cycle.
 * Padding always emits an explicit zero value when the keyboard is closed,
 * unlike height avoidance, which drops its animated height/flex properties.
 */
export function keyboardAvoidancePolicy(surfaceEnabled: boolean) {
  return {
    enabled: surfaceEnabled,
    behavior: "padding" as const,
  };
}
