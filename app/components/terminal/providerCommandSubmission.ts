import type { ComposerAttachment } from "./InterfaceChatSession";

/** Provider commands use the same live send and local optimistic path as text. */
export function submitProviderCommandAsUserInput(
  text: string,
  previousDraft: string | undefined,
  previousAttachments: ComposerAttachment[] | undefined,
  submit: (
    text: string,
    previousDraft: string,
    previousAttachments: ComposerAttachment[],
  ) => void,
) {
  submit(
    text,
    previousDraft?.trim() ? previousDraft : text,
    previousAttachments ?? [],
  );
}
