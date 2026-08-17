import { describe, expect, test } from "bun:test";
import {
  buildSkillsMutationConfirmation,
  normalizeSkillsInspectDetail,
  type SkillsMutationCommand,
} from "./skillsManagement";

const base = {
  copy_id: "a".repeat(24),
  skill_name: "demo",
  enabled: true,
  root_path: "/home/test/.codex/skills/demo",
  canonical_path: "/home/test/.codex/skills/demo",
  allowed_root: "/home/test/.codex/skills",
  location: "Codex global Skills",
  scope: "global",
  agents: ["codex"],
  capability: { can_delete: true },
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
    ).toThrow("outside the Skill file list");
  });
});

describe("exact Skill deletion confirmation", () => {
  const command = (): SkillsMutationCommand => ({
    operation: "delete",
    scope: "global",
    agents: ["codex", "pi"],
    skillName: "demo",
    copyId: "a".repeat(24),
    rootPath: "/home/test/.agents/skills/demo",
    canonicalPath: "/home/test/.agents/skills/demo",
    allowedRoot: "/home/test/.agents/skills",
    location: "Shared project Skills",
    summary: "Delete demo from Shared project Skills",
    destructive: true,
  });

  test("names the Skill, affected Agents, location, and permanence", () => {
    const confirmation = buildSkillsMutationConfirmation(command());
    expect(confirmation.title).toBe("Delete demo?");
    expect(confirmation.confirmLabel).toBe("Delete");
    expect(confirmation.message).toContain('permanently deletes "demo"');
    expect(confirmation.message).toContain("Available to: Codex, Pi");
    expect(confirmation.message).toContain("Location: Shared project Skills");
    expect(confirmation.message).toContain("cannot be undone");
  });
});
