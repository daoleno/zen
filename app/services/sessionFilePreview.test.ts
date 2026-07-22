import { describe, expect, test } from "bun:test";
import {
  appendSessionFileAuthorizationQuery,
  bindSessionFileRequestToGeneration,
  buildSessionFileBinaryUrl,
  classifySessionFileRenderer,
  initialSessionFilePreviewState,
  normalizeSessionFileMetadata,
  normalizeSessionFileText,
  recognizeSessionFileReference,
  reduceSessionFilePreviewState,
  sessionFileTooLargeMessage,
  sessionFilePreviewScopeKey,
} from "./sessionFilePreview";

describe("current-Session file reference recognition", () => {
  test("recognizes explicit local Markdown destinations and common inline-code paths", () => {
    expect(recognizeSessionFileReference("/repo/zen/docs/notes.md#L12")).toBe(
      "/repo/zen/docs/notes.md",
    );
    expect(recognizeSessionFileReference("./app/index.tsx:41:7")).toBe(
      "./app/index.tsx",
    );
    expect(recognizeSessionFileReference("README.md")).toBe("README.md");
    expect(recognizeSessionFileReference("file:///repo/My%20File.pdf")).toBe(
      "/repo/My File.pdf",
    );
    expect(recognizeSessionFileReference("C:\\repo\\zen\\main.go:18")).toBe(
      "C:\\repo\\zen\\main.go",
    );
  });

  test("does not steal external, executable, anchor, or prose links", () => {
    for (const value of [
      "https://example.com/file.md",
      "mailto:user@example.com",
      "javascript:alert(1)",
      "#section",
      "release-notes",
      "",
    ]) {
      expect(recognizeSessionFileReference(value)).toBeNull();
    }
  });
});

describe("current-Session file renderer contract", () => {
  test("projects every supported wire kind to one shared mobile renderer", () => {
    expect(classifySessionFileRenderer("markdown")).toBe("markdown");
    expect(classifySessionFileRenderer("text")).toBe("text");
    expect(classifySessionFileRenderer("image")).toBe("image");
    expect(classifySessionFileRenderer("pdf")).toBe("pdf");
    expect(classifySessionFileRenderer("unsupported")).toBe("unsupported");
  });

  test("builds the authenticated streaming endpoint without route ownership", () => {
    const url = buildSessionFileBinaryUrl("wss://host.example/ws", {
      agentId: "main:@7",
      processId: 412,
      startedAt: 1_784_518_400_123,
      path: "docs/guide.pdf",
      generation: "generation-token",
    });
    expect(url).toBe(
      "https://host.example/session-file?agent_id=main%3A%407&process_id=412&started_at=1784518400123&path=docs%2Fguide.pdf&generation=generation-token",
    );
    expect(url).not.toContain("/terminal/");
  });

  test("binds follow-up reads to the daemon's exact canonical host path", () => {
    const metadata = normalizeSessionFileMetadata({
      name: "shared.md",
      path: "/host/other-repo/shared.md",
      relative_path: "../other-repo/shared.md",
      kind: "markdown",
      content_type: "text/markdown; charset=utf-8",
      size: 9,
      modified_at: "2026-07-20T04:00:00Z",
      generation: "generation-token",
      too_large: false,
      preview_limit_bytes: 0,
    });

    expect(
      bindSessionFileRequestToGeneration(
        {
          agentId: "main:@7",
          processId: 412,
          startedAt: 1_784_518_400_123,
          path: "shared-alias.md",
        },
        metadata,
      ),
    ).toEqual({
      agentId: "main:@7",
      processId: 412,
      startedAt: 1_784_518_400_123,
      path: "/host/other-repo/shared.md",
      generation: "generation-token",
    });
  });

  test("carries the same signed authorization through native binary URL loading", () => {
    const authorizationHeader =
      "ZenDevice v1:device:daemon:1784518400123:nonce:signature";
    const sourceUrl = appendSessionFileAuthorizationQuery(
      "https://host.example/session-file?path=docs%2Foverview.webp&generation=token",
      authorizationHeader,
    );
    const encoded = new URL(sourceUrl).searchParams.get("auth");

    expect(encoded).not.toBeNull();
    const padded = `${encoded!.replace(/-/g, "+").replace(/_/g, "/")}${"=".repeat((4 - (encoded!.length % 4)) % 4)}`;
    expect(globalThis.atob(padded)).toBe(authorizationHeader);
    expect(sourceUrl).toContain("path=docs%2Foverview.webp");
    expect(sourceUrl).toContain("generation=token");
  });

  test("rejects an oversized text payload even if a server violates its bound", () => {
    expect(() =>
      normalizeSessionFileText({
        content: "x".repeat(512 * 1024 + 1),
        bytes_read: 512 * 1024 + 1,
        truncated: false,
        generation: "generation-token",
      }),
    ).toThrow("oversized text preview");
  });

  test("keeps the daemon-owned binary size limit as an explicit no-download state", () => {
    const metadata = normalizeSessionFileMetadata({
      name: "manual.pdf",
      path: "/repo/manual.pdf",
      relative_path: "manual.pdf",
      kind: "pdf",
      content_type: "application/pdf",
      size: 50 * 1024 * 1024 + 1,
      modified_at: "2026-07-20T04:00:00Z",
      generation: "generation-token",
      too_large: true,
      preview_limit_bytes: 50 * 1024 * 1024,
    });

    expect(metadata.tooLarge).toBe(true);
    expect(metadata.previewLimitBytes).toBe(50 * 1024 * 1024);
    expect(sessionFileTooLargeMessage(metadata)).toBe(
      "This 50 MB file exceeds Zen's 50 MB preview limit. It was not downloaded.",
    );

    const opened = reduceSessionFilePreviewState(
      initialSessionFilePreviewState,
      {
        type: "open",
        reference: "manual.pdf",
      },
    );
    const inspected = reduceSessionFilePreviewState(opened, {
      type: "metadata_loaded",
      metadata,
    });
    const rejected = reduceSessionFilePreviewState(inspected, {
      type: "failed",
      message: sessionFileTooLargeMessage(metadata),
      stale: false,
    });
    expect(rejected.metadata).toBe(metadata);
    expect(rejected.binarySource).toBeNull();
    expect(rejected.status).toBe("error");
  });
});

describe("current-Session preview lifecycle", () => {
  test("missing and changed files remain retryable without retaining old content", () => {
    const opened = reduceSessionFilePreviewState(
      initialSessionFilePreviewState,
      {
        type: "open",
        reference: "notes.md",
      },
    );
    const missing = reduceSessionFilePreviewState(opened, {
      type: "failed",
      message: "file does not exist",
      stale: false,
    });
    expect(missing.status).toBe("error");
    expect(missing.reference).toBe("notes.md");
    expect(missing.text).toBeNull();

    const retrying = reduceSessionFilePreviewState(missing, { type: "retry" });
    expect(retrying.status).toBe("loading");
    expect(retrying.requestEpoch).toBe(missing.requestEpoch + 1);

    const changed = reduceSessionFilePreviewState(retrying, {
      type: "failed",
      message: "file changed",
      stale: true,
    });
    expect(changed.stale).toBe(true);
    expect(changed.text).toBeNull();
  });

  test("server, Session, process, start, or live CWD changes close the sheet", () => {
    const scope = sessionFilePreviewScopeKey({
      serverId: "server-a",
      serverUrl: "wss://server-a.example/ws",
      daemonId: "daemon-a",
      agentId: "main:@7",
      processId: 412,
      startedAt: 1_784_518_400_123,
      cwd: "/repo/zen",
    });
    expect(scope).toBe(
      "server-a\u0000wss://server-a.example/ws\u0000daemon-a\u0000main:@7\u0000412\u00001784518400123\u0000/repo/zen",
    );

    const opened = reduceSessionFilePreviewState(
      initialSessionFilePreviewState,
      {
        type: "open",
        reference: "notes.md",
      },
    );
    const closed = reduceSessionFilePreviewState(opened, {
      type: "context_changed",
    });
    expect(closed).toEqual(initialSessionFilePreviewState);
  });
});
