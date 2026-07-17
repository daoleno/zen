import { describe, expect, test } from "bun:test";
import {
  beginLiveMessageAttempt,
  restoreFailedAttachments,
  restoreFailedDraft,
} from "./messageSendRecovery";

describe("failed structured send recovery", () => {
  test("restores the submitted draft when the composer is still empty", () => {
    expect(restoreFailedDraft("please retry", "")).toBe("please retry");
  });

  test("retains any newer draft state when the live write fails", () => {
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

  test("a synchronous frame-write failure restores both inputs and creates no row", () => {
    const previousAttachment = { id: "previous", path: "/previous" };
    const currentAttachment = { id: "current", path: "/current" };
    let optimisticRows = 0;
    const result = beginLiveMessageAttempt({
      writeNow: () => {
        throw new Error("socket closed before write");
      },
      createOptimisticRow: () => {
        optimisticRows += 1;
        return "pending-a";
      },
      previousDraft: "submitted draft",
      currentDraft: "newer draft",
      previousAttachments: [previousAttachment],
      currentAttachments: [currentAttachment],
    });

    expect(result).toEqual({
      kind: "write_failed",
      error: new Error("socket closed before write"),
      restoredDraft: "submitted draft\nnewer draft",
      restoredAttachments: [previousAttachment, currentAttachment],
    });
    expect(optimisticRows).toBe(0);
  });

  test("an optimistic row is created only after the frame write succeeds", () => {
    const order: string[] = [];
    const receipt = { requestId: "request-a" };
    const result = beginLiveMessageAttempt({
      writeNow: () => {
        order.push("frame-written");
        return receipt;
      },
      createOptimisticRow: (writtenReceipt) => {
        order.push("row-created");
        expect(writtenReceipt).toBe(receipt);
        return "pending-a";
      },
      previousDraft: "submitted draft",
      currentDraft: "",
      previousAttachments: [],
      currentAttachments: [],
    });

    expect(order).toEqual(["frame-written", "row-created"]);
    expect(result).toEqual({
      kind: "written",
      receipt,
      pendingMessageId: "pending-a",
    });
  });
});
