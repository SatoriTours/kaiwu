import { test } from "node:test";
import assert from "node:assert/strict";
import {
  formatRevision,
  isValidSHA,
  isRevisionMismatch,
  formatDateTime,
  formatDeploymentStatus,
  type VersionInfo,
} from "../src/version.ts";

test("formatRevision formats dev, empty, and commit SHAs correctly", () => {
  assert.equal(formatRevision(undefined, undefined), "版本未知");
  assert.equal(formatRevision("", ""), "版本未知");
  assert.equal(formatRevision("dev", "dev"), "开发版本");
  assert.equal(formatRevision("dev", ""), "开发版本");
  assert.equal(formatRevision("", "dev"), "开发版本");
  assert.equal(
    formatRevision(
      "8b54309f03dd75759291be48ff3f3b766ab89b5e",
      "8b54309",
    ),
    "8b54309",
  );
  assert.equal(
    formatRevision("8b54309f03dd75759291be48ff3f3b766ab89b5e", ""),
    "8b54309",
  );
});

test("isValidSHA only accepts 7-40 character hexadecimal git SHAs", () => {
  assert.equal(isValidSHA(""), false);
  assert.equal(isValidSHA("dev"), false);
  assert.equal(isValidSHA("123456"), false); // too short (<7)
  assert.equal(isValidSHA("8b54309"), true);
  assert.equal(isValidSHA("8b54309f03dd75759291be48ff3f3b766ab89b5e"), true);
  assert.equal(isValidSHA("8B54309F03DD75759291BE48FF3F3B766AB89B5E"), true);
  assert.equal(isValidSHA("8b54309xyz"), false); // invalid hex
  assert.equal(isValidSHA(" 8b54309 "), false);
  assert.equal(isValidSHA(undefined), false);
});

test("isRevisionMismatch detects outdated frontend without false alarms for dev builds", () => {
  // Undefined or dev frontend build should not report mismatch
  assert.equal(isRevisionMismatch(undefined, "8b54309f03dd75759291be48ff3f3b766ab89b5e"), false);
  assert.equal(isRevisionMismatch("", "8b54309f03dd75759291be48ff3f3b766ab89b5e"), false);
  assert.equal(isRevisionMismatch("dev", "8b54309f03dd75759291be48ff3f3b766ab89b5e"), false);

  // Dev backend should not report mismatch
  assert.equal(isRevisionMismatch("8b54309f03dd75759291be48ff3f3b766ab89b5e", "dev"), false);
  assert.equal(isRevisionMismatch("8b54309f03dd75759291be48ff3f3b766ab89b5e", ""), false);
  assert.equal(isRevisionMismatch("8b54309f03dd75759291be48ff3f3b766ab89b5e", undefined), false);

  // Matching frontend and backend
  assert.equal(
    isRevisionMismatch(
      "8b54309f03dd75759291be48ff3f3b766ab89b5e",
      "8b54309f03dd75759291be48ff3f3b766ab89b5e",
    ),
    false,
  );
  // Short prefix matches full SHA
  assert.equal(
    isRevisionMismatch(
      "8b54309",
      "8b54309f03dd75759291be48ff3f3b766ab89b5e",
    ),
    false,
  );
  assert.equal(
    isRevisionMismatch(
      "8b54309f03dd75759291be48ff3f3b766ab89b5e",
      "8b54309",
    ),
    false,
  );

  // Different valid SHAs -> mismatch!
  assert.equal(
    isRevisionMismatch(
      "1111111111111111111111111111111111111111",
      "8b54309f03dd75759291be48ff3f3b766ab89b5e",
    ),
    true,
  );
  assert.equal(
    isRevisionMismatch(
      "1111111",
      "8b54309",
    ),
    true,
  );
});

test("formatDateTime formats RFC3339 strings and handles empty values gracefully", () => {
  assert.equal(formatDateTime(""), "未知");
  assert.equal(formatDateTime(undefined), "未知");
  assert.equal(formatDateTime("   "), "未知");
  // RFC3339 formatted output contains year and date
  const formatted = formatDateTime("2026-10-19T01:02:03Z");
  assert.ok(formatted.includes("2026"), `Expected 2026 in ${formatted}`);
  assert.ok(formatted.includes("10"), `Expected 10 in ${formatted}`);
});

test("VersionBadge bundle does not contain test mock globals", async () => {
  const fs = await import("node:fs");
  const path = await import("node:path");
  const distDir = path.resolve("dist/assets");
  if (fs.existsSync(distDir)) {
    const files = fs.readdirSync(distDir).filter(f => f.endsWith(".js"));
    for (const file of files) {
      const content = fs.readFileSync(path.join(distDir, file), "utf-8");
      assert.equal(
        content.includes("__MOCK_FRONTEND_REVISION__"),
        false,
        `Found __MOCK_FRONTEND_REVISION__ in ${file}`
      );
    }
  }
});

test("formatDeploymentStatus provides honest status text and failure message", () => {
  const failed = formatDeploymentStatus("failed", "");
  assert.equal(failed.isFailed, true);
  assert.equal(failed.fallbackMessage, "部署未完成，请核对当前版本/服务状态");

  const customFailed = formatDeploymentStatus("failed", "编译错误");
  assert.equal(customFailed.isFailed, true);
  assert.equal(customFailed.fallbackMessage, "部署未完成，请核对当前版本/服务状态");

  const succeeded = formatDeploymentStatus("succeeded", "部署成功");
  assert.equal(succeeded.isFailed, false);
  assert.equal(succeeded.label, "成功");

  const testing = formatDeploymentStatus("testing", "自动化测试中");
  assert.equal(testing.isFailed, false);
  assert.equal(testing.label, "测试中");

  const building = formatDeploymentStatus("building", "构建中");
  assert.equal(building.isFailed, false);
  assert.equal(building.label, "构建中");

  const deploying = formatDeploymentStatus("deploying", "部署中");
  assert.equal(deploying.isFailed, false);
  assert.equal(deploying.label, "部署中");

  const unknown = formatDeploymentStatus("unknown", "");
  assert.equal(unknown.isFailed, false);
  assert.equal(unknown.label, "未知");
});
