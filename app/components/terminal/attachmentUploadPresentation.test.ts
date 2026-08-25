import { describe, expect, test } from "bun:test";
import {
  buildAttachmentUploadPresentation,
  buildAttachmentUploadProgressLabel,
  formatAttachmentUploadBytes,
  formatAttachmentUploadDuration,
} from "./attachmentUploadPresentation";

describe("attachment upload presentation", () => {
  test("known totals show determinate percent and transferred/total bytes", () => {
    expect(
      buildAttachmentUploadProgressLabel({
        transferredBytes: 1_572_864,
        totalBytes: 6_291_456,
        fraction: 0.25,
      }),
    ).toBe("25% · 1.5 MB / 6 MB");
  });

  test("unknown totals stay indeterminate and show transferred bytes when known", () => {
    expect(
      buildAttachmentUploadProgressLabel({
        transferredBytes: 1_572_864,
        totalBytes: null,
        fraction: null,
      }),
    ).toBe("Uploading · 1.5 MB");
    expect(buildAttachmentUploadProgressLabel(null)).toBe("Uploading");
  });

  test("byte labels remain compact and honest", () => {
    expect(formatAttachmentUploadBytes(0)).toBe("0 B");
    expect(formatAttachmentUploadBytes(1024)).toBe("1 KB");
    expect(formatAttachmentUploadBytes(2.5 * 1024 ** 3)).toBe("2.5 GB");
  });

  test("stable throughput replaces verbose byte totals and includes ETA", () => {
    expect(
      buildAttachmentUploadProgressLabel({
        transferredBytes: 512 * 1024 ** 2,
        totalBytes: 1536 * 1024 ** 2,
        fraction: 1 / 3,
        bytesPerSecond: 12.4 * 1024 ** 2,
        etaSeconds: 83,
      }),
    ).toBe("33% · 12 MB/s · 1m 23s left");
    expect(formatAttachmentUploadDuration(0)).toBe("0s");
    expect(formatAttachmentUploadDuration(3725)).toBe("1h 2m");
  });

  test("the shared presentation model exposes determinate accessibility and one Cancel action", () => {
    expect(
      buildAttachmentUploadPresentation("archive.zip", {
        transferredBytes: 25,
        totalBytes: 100,
        fraction: 0.25,
      }),
    ).toEqual({
      accessibilityLabel: "Uploading archive.zip, 25% · 25 B / 100 B",
      accessibilityValue: { min: 0, max: 100, now: 25 },
      cancelAccessibilityLabel: "Cancel upload of archive.zip",
      cancelLabel: "Cancel",
      progressLabel: "25% · 25 B / 100 B",
      progressPercent: 25,
    });
  });

  test("unknown totals have no fabricated determinate accessibility value", () => {
    expect(
      buildAttachmentUploadPresentation("archive.zip", {
        transferredBytes: 25,
        totalBytes: null,
        fraction: null,
      }),
    ).toEqual({
      accessibilityLabel: "Uploading archive.zip, Uploading · 25 B",
      accessibilityValue: undefined,
      cancelAccessibilityLabel: "Cancel upload of archive.zip",
      cancelLabel: "Cancel",
      progressLabel: "Uploading · 25 B",
      progressPercent: null,
    });
  });
});
