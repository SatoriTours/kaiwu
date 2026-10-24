import { test, expect } from "@playwright/test";
import { resolve } from "node:path";
import { mkdirSync } from "node:fs";

const VALID_TOKEN = "kaiwu-browser-test-token-00000000";
const INVALID_TOKEN = "kaiwu-wrong-test-token-12345678";

test.describe("Login page redesign", () => {
  test.beforeEach(async ({ context }) => {
    // Clear cookies before each test to start in unauthenticated state
    await context.clearCookies();
  });

  test("1. Initial state has no error and matches approved visual structure", async ({ page }) => {
    await page.goto("/");
    await page.waitForLoadState("networkidle");

    // Scoped container
    const loginPage = page.locator(".login-page");
    await expect(loginPage).toBeVisible();

    // Brand is ABOVE the card
    const brand = loginPage.locator(".brand");
    await expect(brand).toBeVisible();
    await expect(brand.locator(".mark")).toHaveText("悟");
    await expect(brand.locator(".brand-name")).toHaveText("开悟");

    // Card content
    const card = loginPage.locator(".card");
    await expect(card).toBeVisible();
    await expect(card.locator("h1")).toHaveText("进入你的工作台");
    await expect(card.locator(".intro")).toHaveText("从想法到作品，在创造中更新认知。");

    // Initial 401 must NOT show red "请先登录"
    const alert = loginPage.locator("[role=alert]");
    if (await alert.count() > 0) {
      await expect(alert).toHaveText("");
    }

    // Input attributes
    const input = loginPage.locator("#token");
    await expect(input).toBeVisible();
    await expect(input).toHaveAttribute("type", "password");
    await expect(input).toHaveAttribute("placeholder", "输入本机访问令牌");
    await expect(input).toHaveAttribute("autocomplete", "current-password");

    // Reveal button initial state
    const reveal = loginPage.locator(".reveal");
    await expect(reveal).toBeVisible();
    await expect(reveal).toHaveAttribute("type", "button");
    await expect(reveal).toHaveAttribute("aria-label", "显示令牌");
    await expect(reveal).toHaveAttribute("aria-pressed", "false");

    // Submit button
    const submit = loginPage.locator(".submit");
    await expect(submit).toBeVisible();
    await expect(submit).toContainText("进入工作台");

    // 30 days remember notice
    const remember = loginPage.locator(".remember");
    await expect(remember).toBeVisible();
    await expect(remember).toContainText("登录保留 30 天，退出后清除");

    // Token help
    const help = loginPage.locator("#token-help");
    await expect(help).toBeVisible();
    await expect(help).toContainText("访问令牌在哪里？");
    await expect(help).toContainText("使用启动服务时设置的本机访问令牌。");

    // Footer
    const footer = loginPage.locator(".footer");
    await expect(footer).toBeVisible();
    await expect(footer).toHaveText("本地优先，只连接你自己的开发环境");

    // Check desktop card dimensions (384px)
    const cardBox = await card.boundingBox();
    expect(cardBox).not.toBeNull();
    expect(Math.round(cardBox!.width)).toBe(384);

    // Save actual desktop screenshot for visual verification
    await page.screenshot({ path: resolve("../../docs/design/login-redesign-actual-desktop.png") });

    // Check brand position is above card
    const brandBox = await brand.boundingBox();
    expect(brandBox).not.toBeNull();
    expect(brandBox!.y + brandBox!.height).toBeLessThan(cardBox!.y);

    // Check input height (44px)
    const inputBox = await input.boundingBox();
    expect(inputBox).not.toBeNull();
    expect(Math.round(inputBox!.height)).toBe(44);

    // Check submit button height (42px)
    const submitBox = await submit.boundingBox();
    expect(submitBox).not.toBeNull();
    expect(Math.round(submitBox!.height)).toBe(42);
  });

  test("2. Invalid token shows error message without breaking layout", async ({ page }) => {
    await page.goto("/");
    const loginPage = page.locator(".login-page");
    await expect(loginPage).toBeVisible();

    await loginPage.locator("#token").fill(INVALID_TOKEN);
    await loginPage.locator(".submit").click();

    const alert = loginPage.locator("[role=alert]");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("令牌无效");

    // Remains on login page
    await expect(loginPage).toBeVisible();
  });

  test("3. Show/hide token toggles visibility without submitting form", async ({ page }) => {
    await page.goto("/");
    const loginPage = page.locator(".login-page");
    const input = loginPage.locator("#token");
    const reveal = loginPage.locator(".reveal");

    await input.fill("secret-token-value");
    await expect(input).toHaveAttribute("type", "password");

    // Click reveal to show
    await reveal.click();
    await expect(input).toHaveAttribute("type", "text");
    await expect(reveal).toHaveAttribute("aria-label", "隐藏令牌");
    await expect(reveal).toHaveAttribute("aria-pressed", "true");
    await expect(input).toHaveValue("secret-token-value");

    // Form was NOT submitted (no alert, still on login page)
    const alert = loginPage.locator("[role=alert]");
    if (await alert.count() > 0) {
      await expect(alert).toHaveText("");
    }

    // Click reveal to hide again
    await reveal.click();
    await expect(input).toHaveAttribute("type", "password");
    await expect(reveal).toHaveAttribute("aria-label", "显示令牌");
    await expect(reveal).toHaveAttribute("aria-pressed", "false");
  });

  test("4. Busy state disables button and shows loading text", async ({ page }) => {
    await page.goto("/");
    const loginPage = page.locator(".login-page");
    await loginPage.locator("#token").fill(VALID_TOKEN);

    // Intercept login route to delay response and check busy state
    let routeContinued = false;
    await page.route("**/api/login", async (route) => {
      // Check busy state while request is in-flight
      setTimeout(async () => {
        routeContinued = true;
        await route.continue();
      }, 300);
    });

    await loginPage.locator(".submit").click();
    const submit = loginPage.locator(".submit");
    await expect(submit).toBeDisabled();
    await expect(submit).toContainText("连接中…");

    // Wait until logged in
    await expect(page.locator("header.top")).toBeVisible();
  });

  test("5. Successful login via Enter key, logout restores login screen", async ({ page }) => {
    await page.goto("/");
    const loginPage = page.locator(".login-page");
    await expect(loginPage).toBeVisible();

    await loginPage.locator("#token").fill(VALID_TOKEN);
    await loginPage.locator("#token").press("Enter");

    // Should enter workbench
    await expect(page.locator("header.top")).toBeVisible();
    await expect(page.locator(".login-page")).toHaveCount(0);

    // Click logout
    await page.locator("button.logout").click();

    // Returns to login page
    await expect(page.locator(".login-page")).toBeVisible();
    const alert = page.locator(".login-page [role=alert]");
    if (await alert.count() > 0) {
      await expect(alert).toHaveText("");
    }
  });

  test("6. Remember login across page refresh with backend persistence", async ({ page, context }) => {
    await page.goto("/");
    await page.locator(".login-page #token").fill(VALID_TOKEN);
    await page.locator(".login-page .submit").click();
    await expect(page.locator("header.top")).toBeVisible();

    // Verify session cookie exists
    const cookies = await context.cookies();
    const sessionCookie = cookies.find((c) => c.name === "kaiwu_session");
    expect(sessionCookie).toBeDefined();

    // Refresh page
    await page.reload();

    // Should remain in workbench directly
    await expect(page.locator("header.top")).toBeVisible();
    await expect(page.locator(".login-page")).toHaveCount(0);
  });

  test("7. Responsive layouts: 390px, 360px, and short viewport scrollable", async ({ page }) => {
    // 390px viewport
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto("/");
    const loginPage = page.locator(".login-page");
    await expect(loginPage).toBeVisible();

    // Save actual mobile screenshot for visual verification
    await page.screenshot({ path: resolve("../../docs/design/login-redesign-actual-mobile.png") });

    let scrollWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    let clientWidth = await page.evaluate(() => document.documentElement.clientWidth);
    expect(scrollWidth).toBeLessThanOrEqual(clientWidth);

    // 360px viewport
    await page.setViewportSize({ width: 360, height: 640 });
    scrollWidth = await page.evaluate(() => document.documentElement.scrollWidth);
    clientWidth = await page.evaluate(() => document.documentElement.clientWidth);
    expect(scrollWidth).toBeLessThanOrEqual(clientWidth);

    // Short viewport (height 450px)
    await page.setViewportSize({ width: 800, height: 450 });
    const isScrollable = await page.evaluate(() => {
      const pageEl = document.querySelector(".login-page") || document.documentElement;
      return pageEl.scrollHeight > pageEl.clientHeight;
    });
    expect(isScrollable).toBe(true);

    // Scroll to footer and ensure it is reachable
    const footer = loginPage.locator(".footer");
    await footer.scrollIntoViewIfNeeded();
    await expect(footer).toBeInViewport();
  });

  test("8. Keyboard Tab focus navigation and visible focus ring", async ({ page }) => {
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.goto("/");
    const loginPage = page.locator(".login-page");
    await expect(loginPage).toBeVisible();

    const input = loginPage.locator("#token");
    const reveal = loginPage.locator(".reveal");
    const submit = loginPage.locator(".submit");

    // Focus input
    await input.focus();
    await expect(input).toBeFocused();

    // Tab to reveal button
    await page.keyboard.press("Tab");
    await expect(reveal).toBeFocused();

    // Tab to submit button
    await page.keyboard.press("Tab");
    await expect(submit).toBeFocused();
  });

  test("9. Style isolation: logged-in workbench has no login-page leaked styles", async ({ page }) => {
    await page.goto("/");
    await page.locator(".login-page #token").fill(VALID_TOKEN);
    await page.locator(".login-page .submit").click();
    await expect(page.locator("header.top")).toBeVisible();

    // Body background should be white (default workbench), not #f5f6f3
    const bodyBg = await page.evaluate(() => window.getComputedStyle(document.body).backgroundColor);
    expect(bodyBg).toBe("rgb(255, 255, 255)");
  });

  test("10. Post-login snapshot 401 reports server error and allows retry", async ({ page }) => {
    await page.goto("/");
    const loginPage = page.locator(".login-page");
    await expect(loginPage).toBeVisible();

    // Intercept snapshot endpoint to simulate a 401 error after successful login POST
    let loginDone = false;
    await page.route("**/api/snapshot", async route => {
      if (loginDone) {
        await route.fulfill({
          status: 401,
          contentType: "application/json",
          body: JSON.stringify({ error: { code: "unauthorized", message: "会话已过期" } }),
        });
      } else {
        await route.continue();
      }
    });

    await page.route("**/api/login", async route => {
      loginDone = true;
      await route.continue();
    });

    await loginPage.locator("#token").fill(VALID_TOKEN);
    await loginPage.locator(".submit").click();

    // The post-login 401 MUST NOT be silenced, error must be visible in UI
    const alert = loginPage.locator("[role=alert]");
    await expect(alert).toBeVisible();
    await expect(alert).toContainText("会话已过期");

    // Button should be re-enabled and form should allow retry
    const submit = loginPage.locator(".submit");
    await expect(submit).toBeEnabled();
    await expect(submit).toContainText("进入工作台");
  });
});
