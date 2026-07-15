import { describe, expect, test } from "bun:test";
import {
  restoreFailedAttachments,
  restoreFailedDraft,
} from "./messageSendRecovery";

describe("failed structured send recovery", () => {
  test("restores the submitted draft when the composer is still empty", () => {
    expect(restoreFailedDraft("please retry", "")).toBe("please retry");
  });

  test("retains text typed while awaiting acknowledgement", () => {
    expect(restoreFailedDraft("failed message", "new draft")).toBe(
      "failed message\nnew draft",
    );
  });

  test("restores attachments without duplicating newly retained files", () => {
    const oldFile = { id: "old", path: "/old" };
    const sharedFile = { id: "shared", path: "/shared" };
    const newFile = { id: "new", path: "/new" };
    expect(
      restoreFailedAttachments(
        [oldFile, sharedFile],
        [sharedFile, newFile],
      ).map((attachment) => attachment.id),
    ).toEqual(["old", "shared", "new"]);
  });
});
