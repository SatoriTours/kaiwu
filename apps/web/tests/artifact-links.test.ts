import { test } from "node:test";
import assert from "node:assert/strict";
import { safeArtifactURL, preserveArtifactLinks, isWebPreviewable } from "../src/preview.ts";
import type { TaskPreview } from "../src/types.ts";

const url = `/preview/${"a".repeat(64)}/~download/${"b".repeat(64)}`;
const metadata = (over = {}): TaskPreview => ({ taskId: "t", roundId: "r", manifestId: "m", kind: "files", state: "running", message: "files", artifacts: [{ id: "b".repeat(64), name: "总结 报告.pdf", size: 12, type: "application/pdf" }], ...over });

test("only exact local download capabilities become file links", () => {
  assert.equal(safeArtifactURL(url), url);
  for (const unsafe of ["https://evil.invalid" + url, "//evil.invalid" + url, url + "?q=x", url + "#x", url + "/../x", url.replace("~download/", "~download%2f"), url.replace("~download/", "~download/%2e%2e/"), url.replace("a".repeat(64), "a".repeat(63)), "javascript:alert(1)"]) {
    assert.equal(safeArtifactURL(unsafe), undefined, unsafe);
  }
});

test("files-only results cannot become webpages even with a stray URL", () => {
  assert.equal(isWebPreviewable(metadata({ url })), false);
});

test("status retains links only for the identical live task, round, manifest and file metadata", () => {
  const before = metadata();
  before.artifacts![0].url = url;
  assert.equal(preserveArtifactLinks(metadata(), before)?.[0].url, url);
  for (const after of [metadata({ taskId: "other" }), metadata({ roundId: "next" }), metadata({ manifestId: "changed" }), metadata({ state: "stopped" }), metadata({ artifacts: [{ ...before.artifacts![0], url: undefined, size: 99 }] })]) {
    assert.equal(preserveArtifactLinks(after, before)?.[0].url, undefined);
  }
  assert.equal(before.artifacts![0].url, url, "never mutate the prior snapshot");
});
