import type { ComposerAttachment } from "./CodexChatSession";

/** Provider-dispatched commands use the same accepted input/queue path as text. */
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
