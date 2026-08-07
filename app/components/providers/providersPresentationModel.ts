/**
 * Pure presentation policy for the one-surface Providers editor. Supplier
 * selection and credentials always coexist in the same overlay, so this stays
 * free of React Native imports to keep it unit-testable.
 */
export function providerEditorCanSave(input: {
  mutating: boolean;
  apiKey: string;
  credentialMode: boolean;
  presetSelected: boolean;
  customSelected: boolean;
  name: string;
  baseUrl: string;
}): boolean {
  if (input.mutating) return false;
  if (input.apiKey.trim().length === 0) return false;
  if (input.credentialMode) return true;
  if (input.presetSelected) return true;
  return (
    input.customSelected &&
    input.name.trim().length > 0 &&
    input.baseUrl.trim().length > 0
  );
}
