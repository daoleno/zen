// @ts-nocheck
import { describe, expect, test } from "bun:test";
import {
  isSafeMarkdownUrl,
  openSafeMarkdownUrl,
  tokenizeMarkdownLinks,
} from "./markdownLinks";

describe("Markdown fallback links", () => {
  test("retains surrounding Markdown while exposing link destinations", () => {
    expect(tokenizeMarkdownLinks("See **[docs](https://example.com/docs)** now."))
      .toEqual([
        { text: "See **" },
        { text: "docs", url: "https://example.com/docs" },
        { text: "** now." },
      ]);
  });

  test("exposes unsafe destinations for policy rejection", () => {
    expect(tokenizeMarkdownLinks("See [unsafe](javascript:alert) now."))
      .toEqual([
        { text: "See " },
        { text: "unsafe", url: "javascript:alert" },
        { text: " now." },
      ]);
    expect(isSafeMarkdownUrl("javascript:alert")).toBe(false);
  });
});

describe("Markdown link activation", () => {
  test("opens trimmed HTTP and HTTPS links", async () => {
    const opened: string[] = [];
    const openUrl = async (url: string) => {
      opened.push(url);
    };

    expect(await openSafeMarkdownUrl(" https://example.com/docs ", openUrl)).toBe(true);
    expect(await openSafeMarkdownUrl("http://localhost:8080/path", openUrl)).toBe(true);
    expect(opened).toEqual([
      "https://example.com/docs",
      "http://localhost:8080/path",
    ]);
  });

  test("rejects unsafe and malformed URLs without invoking the opener", async () => {
    const opened: string[] = [];
    const openUrl = async (url: string) => {
      opened.push(url);
    };

    for (const url of [
      "javascript:alert(1)",
      "data:text/html,unsafe",
      "file:///etc/passwd",
      "mailto:user@example.com",
      "/relative/path",
      "not a URL",
    ]) {
      expect(isSafeMarkdownUrl(url)).toBe(false);
      expect(await openSafeMarkdownUrl(url, openUrl)).toBe(false);
    }
    expect(opened).toEqual([]);
  });

  test("contains openURL rejection", async () => {
    const result = await openSafeMarkdownUrl("https://example.com", async () => {
      throw new Error("no URL handler");
    });

    expect(result).toBe(false);
  });
});
