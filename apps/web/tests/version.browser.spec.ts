import { test, expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { resolve } from "node:path";
import { mkdirSync } from "node:fs";

const TOKEN = "kaiwu-version-test-token-00000000";
// This suite validates any deployment candidate. The front-end bundle and the
// fixture back end both use the revision passed by the pipeline; without it the
// suite keeps validating its own fixture revision.
const FIXTURE_REVISION = "8b54309f03dd75759291be48ff3f3b766ab89b5e";
const RAW_CANDIDATE = (process.env.KAIWU_VERSION_TEST_REVISION || "").trim();
const CANDIDATE_REVISION = /^[0-9a-f]{40}$/i.test(RAW_CANDIDATE)
  ? RAW_CANDIDATE.toLowerCase()
  : FIXTURE_REVISION;
const CANDIDATE_SHORT = CANDIDATE_REVISION.slice(0, 7);
// A revision guaranteed to differ from the embedded front-end revision.
function otherRevision(fallback: string, alternate: string): string {
  return fallback === CANDIDATE_REVISION ? alternate : fallback;
}
const OTHER_REVISION = otherRevision("1111111111111111111111111111111111111111", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
const OTHER_SHORT = OTHER_REVISION.slice(0, 7);
const NEXT_REVISION = otherRevision("2222222222222222222222222222222222222222", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");
const NEXT_SHORT = NEXT_REVISION.slice(0, 7);
const LATER_REVISION = otherRevision("3333333333333333333333333333333333333333", "cccccccccccccccccccccccccccccccccccccccc");
const LATER_SHORT = LATER_REVISION.slice(0, 7);
const screenshotDir = resolve("../../.kaiwu");

try {
  mkdirSync(screenshotDir, { recursive: true });
} catch {}

async function login(page: Page) {
  await page.goto("/");
  const token = page.getByLabel("本机访问令牌");
  if (await token.isVisible().catch(() => false)) {
    await token.fill(TOKEN);
    await page.getByRole("button", { name: "进入工作台", exact: true }).click();
  }
  await expect(page.locator(".version-trigger")).toBeVisible({ timeout: 10000 });
}

test.describe("VersionBadge UI and Interactions", () => {
  test("1. 登录后顶栏展示真实运行短版本及对话框语义属性", async ({ page }) => {
    await login(page);

    const trigger = page.locator(".version-trigger");
    await expect(trigger).toBeVisible();
    await expect(trigger).toHaveText(CANDIDATE_SHORT);
    await expect(trigger).toHaveAttribute("aria-haspopup", "dialog");
    await expect(trigger).toHaveAttribute("aria-expanded", "false");
    await expect(trigger).toHaveAttribute("title", `版本信息: ${CANDIDATE_SHORT}`);
  });

  test("2. 详情打开、内容完整显示、Esc键盘操作与焦点恢复", async ({ page }) => {
    await login(page);

    const trigger = page.locator(".version-trigger");
    await trigger.click();

    const dialog = page.locator("#version-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveAttribute("role", "dialog");
    await expect(dialog).toHaveAttribute("aria-modal", "true");
    await expect(dialog).toHaveAttribute("aria-labelledby", "version-dialog-title");

    // Verify full SHA and metadata
    await expect(dialog.locator(".sha-code").first()).toHaveText(CANDIDATE_REVISION);
    await expect(dialog.locator(".summary-text")).toHaveText("版本可见性与部署状态");
    await expect(dialog).toContainText("当前运行程序");
    await expect(dialog).toContainText("最近部署尝试");
    await expect(dialog.locator(".status-tag")).toHaveText("成功");

    // Focus trap verification (Tab should cycle inside dialog between focusable elements)
    const closeBtn = dialog.locator(".close-button");
    const refreshBtn = dialog.locator(".refresh-button");
    const dismissBtn = dialog.locator(".dismiss-button");

    // Currently closeBtn is focused on open
    await expect(closeBtn).toBeFocused();

    // Tab -> refreshBtn
    await page.keyboard.press("Tab");
    await expect(refreshBtn).toBeFocused();

    // Tab -> dismissBtn
    await page.keyboard.press("Tab");
    await expect(dismissBtn).toBeFocused();

    // Tab wraps around -> closeBtn
    await page.keyboard.press("Tab");
    await expect(closeBtn).toBeFocused();

    // Shift+Tab wraps backwards -> dismissBtn
    await page.keyboard.press("Shift+Tab");
    await expect(dismissBtn).toBeFocused();

    // Close with Escape and verify focus restores to trigger
    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();
    await expect(trigger).toBeFocused();

    // Reopen and close with close button, verify focus restores to trigger
    await trigger.click();
    await expect(dialog).toBeVisible();
    await closeBtn.click();
    await expect(dialog).not.toBeVisible();
    await expect(trigger).toBeFocused();
  });

  test("3. 请求失败显示版本未知，刷新失败不显示过期成功状态", async ({ page }) => {
    // Intercept GET /api/version to simulate network/server failure
    await page.route("**/api/version", (route) => {
      route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({ error: { code: "server_error", message: "服务内部错误" } }),
      });
    });

    await login(page);

    const trigger = page.locator(".version-trigger");
    await expect(trigger).toHaveText("版本未知");
    await expect(trigger).toHaveClass(/is-unknown/);

    // Open dialog to verify failure message
    await trigger.click();
    const dialog = page.locator("#version-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.locator(".alert-error")).toContainText("服务内部错误");

    // Click refresh button in dialog
    const refreshBtn = dialog.locator(".refresh-button");
    await refreshBtn.click();

    // Verify it still honestly displays failure and does not show fake success
    await expect(dialog.locator(".alert-error")).toBeVisible();
    await expect(trigger).toHaveText("版本未知");
  });

  test("4. 最近部署失败不掩盖当前实际运行版本，显示核对文案", async ({ page }) => {
    await page.route("**/api/version", (route) => {
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          revision: CANDIDATE_REVISION,
          shortRevision: CANDIDATE_SHORT,
          summary: "版本可见性与部署状态",
          builtAt: "2026-10-19T01:02:03Z",
          deployedAt: "2026-10-19T01:03:00Z",
          deployment: {
            revision: "9999999999999999999999999999999999999999",
            status: "failed",
            at: "2026-10-19T02:00:00Z",
            message: "部署未完成，请核对当前版本/服务状态",
          },
        }),
      });
    });

    await login(page);

    const trigger = page.locator(".version-trigger");
    // Topbar still preserves running revision
    await expect(trigger).toHaveText(CANDIDATE_SHORT);

    await trigger.click();
    const dialog = page.locator("#version-dialog");
    await expect(dialog).toBeVisible();

    // Running version is still the active revision
    await expect(dialog.locator(".sha-code").first()).toHaveText(CANDIDATE_REVISION);

    // Failure banner shows exact required wording
    const failureBanner = dialog.locator(".failure-banner");
    await expect(failureBanner).toBeVisible();
    await expect(failureBanner).toHaveText("部署未完成，请核对当前版本/服务状态");

    // Attempted status and revision are displayed honestly
    await expect(dialog.locator(".status-tag")).toHaveText("失败");
    await expect(dialog.locator(".sha-code").nth(1)).toHaveText("9999999999999999999999999999999999999999");
  });

  test("5. 开发版本明确显示为开发版本，不虚构未知", async ({ page }) => {
    await page.route("**/api/version", (route) => {
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          revision: "dev",
          shortRevision: "dev",
          summary: "本地调试",
          builtAt: "",
          deployedAt: "",
          deployment: {
            revision: "",
            status: "unknown",
            at: "",
            message: "暂无部署记录",
          },
        }),
      });
    });

    await login(page);

    const trigger = page.locator(".version-trigger");
    await expect(trigger).toHaveText("开发版本");
    await expect(trigger).toHaveClass(/is-dev/);

    await trigger.click();
    const dialog = page.locator("#version-dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.locator(".sha-code").first()).toHaveText("dev");
    await expect(dialog.locator(".status-tag")).toHaveText("未知");
  });

  test("6. 前后端合法SHA不一致提示页面过期并提供刷新按钮", async ({ page }) => {
    // Provide a different valid backend SHA (production build embeds 8b54309...)
    await page.route("**/api/version", (route) => {
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          revision: OTHER_REVISION,
          shortRevision: OTHER_SHORT,
          summary: "新版本发布",
          builtAt: "2026-10-19T03:00:00Z",
          deployedAt: "2026-10-19T03:01:00Z",
          deployment: {
            revision: OTHER_REVISION,
            status: "succeeded",
            at: "2026-10-19T03:01:00Z",
            message: "部署成功",
          },
        }),
      });
    });

    await login(page);

    const trigger = page.locator(".version-trigger");
    await expect(trigger).toHaveClass(/is-outdated/);
    await expect(trigger.locator(".outdated-dot")).toBeVisible();

    await trigger.click();

    const dialog = page.locator("#version-dialog");
    await expect(dialog).toBeVisible();

    // Verify running revision is not secretly replaced with frontend version
    await expect(dialog.locator(".sha-code").first()).toHaveText(OTHER_REVISION);

    // Outdated warning banner is visible with reload button
    const alertWarning = dialog.locator(".alert-warning");
    await expect(alertWarning).toBeVisible();
    await expect(alertWarning).toContainText("页面版本已过期，请刷新");
    await expect(alertWarning.locator(".reload-button")).toBeVisible();
  });

  test("7. 1440px 与 360px 响应式布局不挤坏，截屏保存至 .kaiwu", async ({ page }) => {
    // 1440px desktop
    await page.setViewportSize({ width: 1440, height: 900 });
    await login(page);

    const trigger = page.locator(".version-trigger");
    await expect(trigger).toBeVisible();

    // Check header horizontal scroll
    const headerOverflow = await page.evaluate(() => {
      const header = document.querySelector(".top");
      if (!header) return false;
      return header.scrollWidth > header.clientWidth;
    });
    expect(headerOverflow).toBe(false);

    await page.screenshot({ path: `${screenshotDir}/version-1440px.png` });

    // 360px mobile
    await page.setViewportSize({ width: 360, height: 640 });
    await page.waitForTimeout(300);

    await expect(trigger).toBeVisible();
    const triggerBox = await trigger.boundingBox();
    expect(triggerBox).not.toBeNull();
    expect(triggerBox!.width).toBeLessThanOrEqual(100);

    await page.screenshot({ path: `${screenshotDir}/version-360px.png` });

    // Open dialog at 360px
    await trigger.click();
    const dialog = page.locator("#version-dialog");
    await expect(dialog).toBeVisible();

    const dialogBox = await dialog.boundingBox();
    expect(dialogBox).not.toBeNull();
    expect(dialogBox!.width).toBeLessThanOrEqual(360);

    await page.screenshot({ path: `${screenshotDir}/version-dialog-360px.png` });
  });

  test("8. 页面长开时后台更新版本，打开详情或窗口激活即时感知并提示过期", async ({ page }) => {
    let currentBackendRevision = CANDIDATE_REVISION;
    let currentShortRevision = CANDIDATE_SHORT;

    await page.route("**/api/version", (route) => {
      route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          revision: currentBackendRevision,
          shortRevision: currentShortRevision,
          summary: "部署版本",
          builtAt: "2026-10-19T01:02:03Z",
          deployedAt: "2026-10-19T01:03:00Z",
          deployment: {
            revision: currentBackendRevision,
            status: "succeeded",
            at: "2026-10-19T01:03:00Z",
            message: "部署成功",
          },
        }),
      });
    });

    await login(page);

    const trigger = page.locator(".version-trigger");
    await expect(trigger).toHaveText(CANDIDATE_SHORT);
    await expect(trigger).not.toHaveClass(/is-outdated/);

    // Simulate backend deployment to new version while page is still open
    currentBackendRevision = NEXT_REVISION;
    currentShortRevision = NEXT_SHORT;

    // 1. Test window focus triggers refresh
    await page.evaluate(() => {
      window.dispatchEvent(new Event("focus"));
    });

    // Topbar trigger should detect the new version and show outdated indicator
    await expect(trigger.locator(".version-text")).toHaveText(NEXT_SHORT);
    await expect(trigger).toHaveClass(/is-outdated/);
    await expect(trigger.locator(".outdated-dot")).toBeVisible();

    // Reset back to matching version
    currentBackendRevision = CANDIDATE_REVISION;
    currentShortRevision = CANDIDATE_SHORT;

    // 2. Test opening dialog triggers refresh
    // Deploy another new version
    currentBackendRevision = LATER_REVISION;
    currentShortRevision = LATER_SHORT;

    await trigger.click();
    const dialog = page.locator("#version-dialog");
    await expect(dialog).toBeVisible();

    // Dialog should show the freshly fetched new version and outdated alert
    await expect(dialog.locator(".sha-code").first()).toHaveText(LATER_REVISION);
    await expect(dialog.locator(".alert-warning")).toContainText("页面版本已过期，请刷新");

    // Close dialog
    await page.keyboard.press("Escape");
    await expect(dialog).not.toBeVisible();
    await expect(trigger.locator(".version-text")).toHaveText(LATER_SHORT);
  });
});

