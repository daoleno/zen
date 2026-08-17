import { describe, expect, test } from "bun:test";
import {
  buildSkillsMutationConfirmation,
  normalizeSkillsInspectDetail,
  type SkillsMutationCommand,
} from "./skillsManagement";

const base = {
  skill_name: "demo",
  manager: "external",
  owned: false,
  tracked: false,
  enabled: true,
  scope: "global",
  agents: ["codex"],
  bindings: [],
  capability: { can_manage: true, operations: ["adopt"] },
};

describe("Skill inspection normalization", () => {
  test("accepts typed nested package files and previews", () => {
    const detail = normalizeSkillsInspectDetail({
      ...base,
      files: [
        {
          path: "config/data.json",
          size: 12,
          mode: "0600",
          kind: "json",
          media_type: "application/json",
          preview_status: "ready",
        },
      ],
      preview: {
        path: "config/data.json",
        kind: "json",
        media_type: "application/json",
        status: "ready",
        size: 12,
        bytes_returned: 12,
        content: '{"ok":true}',
      },
    });
    expect(detail.files?.[0]?.path).toBe("config/data.json");
    expect(detail.preview?.content).toBe('{"ok":true}');
  });
  test("preserves explicit binary and truncated states", () => {
    expect(
      normalizeSkillsInspectDetail({
        ...base,
        files: [
          {
            path: "a.bin",
            kind: "binary",
            media_type: "application/octet-stream",
            preview_status: "binary",
            size: 99,
            mode: "0600",
          },
        ],
        preview: {
          path: "a.bin",
          kind: "binary",
          media_type: "application/octet-stream",
          status: "binary",
          size: 99,
          bytes_returned: 0,
          notice: "metadata only",
        },
      }).preview?.status,
    ).toBe("binary");
    expect(
      normalizeSkillsInspectDetail({
        ...base,
        files: [
          {
            path: "large.txt",
            kind: "text",
            media_type: "text/plain",
            preview_status: "large",
            size: 70000,
            mode: "0600",
          },
        ],
        preview: {
          path: "large.txt",
          kind: "text",
          media_type: "text/plain",
          status: "truncated",
          size: 70000,
          bytes_returned: 65536,
          content: "part",
          notice: "limited",
        },
      }).preview?.status,
    ).toBe("truncated");
  });

  test("accepts large external file metadata while keeping previews bounded", () => {
    const detail = normalizeSkillsInspectDetail({
      ...base,
      files: [
        {
          path: "model.bin",
          size: 128 * 1024 * 1024,
          mode: "0600",
          kind: "binary",
          media_type: "application/octet-stream",
          preview_status: "binary",
        },
      ],
      preview: {
        path: "model.bin",
        kind: "binary",
        media_type: "application/octet-stream",
        status: "binary",
        size: 128 * 1024 * 1024,
        bytes_returned: 0,
        notice: "Binary files are shown as metadata only.",
      },
    });
    expect(detail.files?.[0]?.size).toBe(128 * 1024 * 1024);
    expect(detail.preview?.bytesReturned).toBe(0);
  });
  test("rejects malformed preview metadata", () => {
    expect(() =>
      normalizeSkillsInspectDetail({
        ...base,
        preview: {
          path: "x",
          kind: "binary",
          media_type: "",
          status: "ready",
          size: -1,
          bytes_returned: 0,
        },
      }),
    ).toThrow("invalid Skill file preview");
  });
  test("rejects unsafe, duplicate, and out-of-list file identities", () => {
    const listed = {
      path: "notes.txt",
      size: 4,
      mode: "0600",
      kind: "text",
      media_type: "text/plain",
      preview_status: "ready",
    };
    for (const path of ["../secret", "/etc/passwd", "a//b", "a\\b"]) {
      expect(() =>
        normalizeSkillsInspectDetail({ ...base, files: [{ ...listed, path }] }),
      ).toThrow("invalid Skill package file list");
    }
    expect(() =>
      normalizeSkillsInspectDetail({ ...base, files: [listed, listed] }),
    ).toThrow("invalid Skill package file list");
    expect(() =>
      normalizeSkillsInspectDetail({
        ...base,
        files: [listed],
        preview: {
          path: "other.txt",
          kind: "text",
          media_type: "text/plain",
          status: "ready",
          size: 4,
          bytes_returned: 4,
          content: "test",
        },
      }),
    ).toThrow("outside the Skill package file list");
  });
});

describe("Skill lifecycle confirmation copy", () => {
  const command = (
    operation: SkillsMutationCommand["operation"],
    destructive = false,
  ): SkillsMutationCommand => ({
    operation,
    scope: "global",
    agents: ["codex"],
    skillName: "demo",
    summary: "Lifecycle summary",
    changes: [
      {
        kind: destructive ? "remove" : "copy_file",
        path: ".../.zen/skills/demo",
        detail: destructive ? "Zen managed copy" : "Copy into managed store",
      },
    ],
    destructive,
  });

  test("names adoption as Manage with Zen", () => {
    const confirmation = buildSkillsMutationConfirmation(command("adopt"));
    expect(confirmation.title).toBe("Manage with Zen demo?");
    expect(confirmation.confirmLabel).toBe("Manage with Zen");
    expect(confirmation.message).toContain("Copy into managed store");
  });

  test("makes managed uninstall destruction explicit", () => {
    const confirmation = buildSkillsMutationConfirmation(
      command("uninstall", true),
    );
    expect(confirmation.title).toBe("Uninstall demo?");
    expect(confirmation.confirmLabel).toBe("Uninstall");
    expect(confirmation.message).toContain("This removes the following:");
    expect(confirmation.message).toContain("Zen managed copy");
  });

  test("keeps Forget non-destructive to external files", () => {
    const confirmation = buildSkillsMutationConfirmation(command("forget"));
    expect(confirmation.confirmLabel).toBe("Forget");
    expect(confirmation.message).not.toContain("This removes the following:");
    expect(confirmation.message).toContain("Changes:");
  });
});
