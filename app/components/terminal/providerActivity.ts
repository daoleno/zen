import {
  isProviderActivityRunning,
  type ProviderActivity,
} from "../../services/codexConversation";

/**
 * Resolves the single Activity that owns timeline Working and Composer state.
 * A local send result is never executor lifecycle evidence.
 */
export function resolveRunningProviderActivity(
  authoritativeActivity: ProviderActivity | undefined,
): ProviderActivity | undefined {
  return isProviderActivityRunning(authoritativeActivity)
    ? authoritativeActivity
    : undefined;
}
