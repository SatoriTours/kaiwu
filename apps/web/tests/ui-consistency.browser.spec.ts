import { test, expect } from "@playwright/test";
import { resolve } from "node:path";
import { readFileSync, existsSync } from "node:fs";

async function authenticateResultTest(page: any) {
  const fixture = readFileSync(
    resolve("../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt"),
    "utf8"
  );
  const cookies = resolve(fixture, "..", "browser-session.json");
  if (existsSync(cookies))
    await page.context().addCookies(JSON.parse(readFileSync(cookies, "utf8")));
  if (!(await page.request.get("/api/session")).ok()) {
    const login = await page.request.post("/api/login", {
      data: { token: "kaiwu-browser-test-token-00000000" },
      headers: { Origin: "http://127.0.0.1:17331", "X-Kaiwu-Client": "web" },
    });
    expect(login.ok(), await login.text()).toBe(true);
  }
}

async function enterWorkbench(page: any) {
  await authenticateResultTest(page);
  const fixturePath = readFileSync(
    resolve("../../.superpowers/sdd/2026-10-11-web-service/browser-project.txt"),
    "utf8"
  ).trim();

  let snapshot = (await (await page.request.get("/api/snapshot")).json()).state;
  let targetProject = Object.values(snapshot.projects).find(
    (p: any) => p.name === "开悟验收项目"
  ) as any;
  if (!targetProject) {
    const resp = await page.request.post("/api/commands", {
      data: {
        type: "add_project",
        operationId: "ui-consistency-" + Date.now(),
        revision: snapshot.revision,
        name: "开悟验收项目",
        path: fixturePath,
      },
      headers: { Origin: "http://127.0.0.1:17331", "X-Kaiwu-Client": "web" },
    });
    expect(resp.ok()).toBe(true);
    targetProject = { id: (await resp.json()).createdId, name: "开悟验收项目" };
  }
  await page.goto("/projects/" + targetProject.id);
  await expect(page.locator("#chat-title")).toBeVisible();
}

test.describe("UI Consistency & CustomSelect Verification", () => {
  test("D1: Mobile topic dropdown is not visible on desktop 1440px but visible on mobile 360px", async ({
    page,
  }) => {
    await enterWorkbench(page);
    await page.setViewportSize({ width: 1440, height: 900 });

    // D1 验证：桌面 1440px 下，看板工具栏中的“当前看板专题”不得可见
    const desktopMobileTopic = page.getByRole("combobox", { name: "当前看板专题" });
    await expect(desktopMobileTopic).not.toBeVisible();

    // 移动端 360px 下，应可见且占位正常
    await page.setViewportSize({ width: 360, height: 800 });
    const mobileTopic = page.getByRole("combobox", { name: "当前看板专题" });
    await expect(mobileTopic).toBeVisible();
  });

  test("D2: Keyboard navigation on options has gray-green outline and no UA blue focus ring", async ({
    page,
  }) => {
    await enterWorkbench(page);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.getByRole("button", { name: "默认模型", exact: true }).click();
    const modelDialog = page.getByRole("dialog", { name: "默认模型", exact: true });
    await expect(modelDialog).toBeVisible();

    const providerTrigger = modelDialog.getByRole("combobox", { name: "提供商" });
    // Model discovery is asynchronous; key presses on a disabled trigger are ignored.
    await expect(providerTrigger).toBeEnabled();
    await providerTrigger.press("ArrowDown");
    const dropdown = modelDialog.locator('[role="listbox"]');
    await expect(dropdown).toBeVisible();

    // 键盘移动到选项
    await page.keyboard.press("ArrowDown");
    const activeOption = dropdown.locator(".custom-select-option.active");
    await expect(activeOption).toBeVisible();

    // 验证当前激活选项具有 focus，且在 :focus 状态下具有灰绿 outline (#849e90 -> rgb(132, 158, 144))
    const focusState = await activeOption.evaluate((el) => {
      const cs = window.getComputedStyle(el);
      const isFocused = document.activeElement === el;
      return {
        isFocused,
        outlineColor: cs.outlineColor,
        outlineStyle: cs.outlineStyle,
      };
    });
    // 明确断言 DOM 焦点、outlineColor 与 outlineStyle
    expect(focusState.isFocused).toBe(true);
    expect(focusState.outlineColor).toBe("rgb(132, 158, 144)");
    expect(focusState.outlineStyle).toBe("solid");

    // 截取带选项焦点的实际渲染截图，存入 docs/design/ui-consistency/
    await page.screenshot({
      path: resolve("../../docs/design/ui-consistency/custom-select-keynav-focus.png"),
    });

    await page.keyboard.press("Escape");
    await modelDialog.getByRole("button", { name: "取消" }).click();
  });

  test("D3: Empty model dropdown on Escape only closes menu, preserves dialog, keeps trigger focus, recovers dynamically, and allows dialog close when closed", async ({
    page,
  }) => {
    await enterWorkbench(page);
    await page.setViewportSize({ width: 1440, height: 900 });

    await page.getByRole("button", { name: "默认模型", exact: true }).click();
    const modelDialog = page.getByRole("dialog", { name: "默认模型", exact: true });
    await expect(modelDialog).toBeVisible();

    // 1. 选择一个提供商确保模型列表存在
    const providerTrigger = modelDialog.getByRole("combobox", { name: "提供商" });
    await providerTrigger.click();
    const providerList = modelDialog.locator('[role="listbox"]');
    await expect(providerList).toBeVisible();
    const firstProviderOption = providerList.locator(".custom-select-option").first();
    await firstProviderOption.click();
    await expect(providerList).toHaveCount(0);

    // 2. 在“搜索模型”中输入无匹配关键字
    const searchInput = modelDialog.getByLabel("搜索模型");
    await searchInput.fill("NON_EXISTING_MODEL_KEYWORD_XYZ");

    // 3. 打开模型下拉，呈现空列表“无可用选项”，焦点留在 trigger 上
    const modelTrigger = modelDialog.getByRole("combobox", { name: "模型" });
    await modelTrigger.click();
    const modelDropdown = modelDialog.locator('[role="listbox"]');
    await expect(modelDropdown).toBeVisible();
    await expect(modelDropdown.locator(".custom-select-empty")).toHaveText("无可用选项");
    await expect(modelTrigger).toBeFocused();

    // 4. 按 Escape，必须仅关闭下拉菜单，不误关外层 dialog，且焦点保留在 trigger 上
    await page.keyboard.press("Escape");
    await expect(modelDropdown).toHaveCount(0);
    await expect(modelDialog).toBeVisible();
    await expect(modelTrigger).toBeFocused();

    // 5. 动态恢复与无 option 聚焦边界验证：
    // 5.1 清空搜索关键字，验证动态恢复非空选项列表
    await searchInput.fill("");
    await modelTrigger.click();
    await expect(modelDropdown).toBeVisible();
    await expect(modelDropdown.locator(".custom-select-option").first()).toBeVisible();

    // 5.2 非空菜单下按 Escape 也能正常只关菜单不关 dialog
    await page.keyboard.press("Escape");
    await expect(modelDropdown).toHaveCount(0);
    await expect(modelDialog).toBeVisible();

    // 5.3 再次搜索变空后展开，验证 options 动态变空时焦点留在 trigger，按 Escape 同样只关菜单不关 dialog
    await searchInput.fill("ANOTHER_NON_EXISTING_KEYWORD");
    await modelTrigger.click();
    await expect(modelDropdown).toBeVisible();
    await expect(modelDropdown.locator(".custom-select-empty")).toHaveText("无可用选项");
    await expect(modelTrigger).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(modelDropdown).toHaveCount(0);
    await expect(modelDialog).toBeVisible();
    await expect(modelTrigger).toBeFocused();

    // 6. 处于闭合状态时按 Escape，应正常触发 dialog 关闭
    await page.keyboard.press("Escape");
    await expect(modelDialog).toHaveCount(0);
  });

  test("CustomSelect renders with gray-green styling and handles keyboard navigation", async ({
    page,
  }) => {
    await enterWorkbench(page);

    // 1. 打开“默认模型”对话框
    await page.getByRole("button", { name: "默认模型", exact: true }).click();
    const modelDialog = page.getByRole("dialog", { name: "默认模型", exact: true });
    await expect(modelDialog).toBeVisible();

    // 验证自定义下拉组件 trigger 具备 gray-green 规范和正确无原生 select
    const providerTrigger = modelDialog.getByRole("combobox", { name: "提供商" });
    await expect(providerTrigger).toBeVisible();
    await expect(modelDialog.locator("select")).toHaveCount(0);

    // 截图保存到 docs/design/ui-consistency/
    await page.screenshot({
      path: resolve("../../docs/design/ui-consistency/custom-select-dialog.png"),
    });

    // 展开提供商下拉框
    await providerTrigger.click();
    const dropdown = modelDialog.locator('[role="listbox"]');
    await expect(dropdown).toBeVisible();

    // 验证 Escape 关闭下拉框且不关闭父 dialog
    await page.keyboard.press("Escape");
    await expect(dropdown).toHaveCount(0);
    await expect(modelDialog).toBeVisible();

    // 重新打开并通过键盘方向键导航和 Enter 选择
    await providerTrigger.press("ArrowDown");
    await expect(dropdown).toBeVisible();
    await page.keyboard.press("Enter");
    await expect(dropdown).toHaveCount(0);

    // 关闭 dialog
    await modelDialog.getByRole("button", { name: "取消" }).click();
    await expect(modelDialog).toHaveCount(0);

    // 2. 打开创建任务卡片对话框，验证专题下拉
    await page.locator("[data-action=new-task]").click();
    const taskDialog = page.getByRole("dialog", { name: "创建任务卡片" });
    await expect(taskDialog).toBeVisible();
    const topicTrigger = taskDialog.getByRole("combobox", { name: "功能专题" });
    await expect(topicTrigger).toBeVisible();
    await expect(taskDialog.locator("select")).toHaveCount(0);

    await taskDialog.getByRole("button", { name: "取消" }).click();
    await expect(taskDialog).toHaveCount(0);

    // 3. 截取看板与聊天输入框聚焦风格，验证无残留突兀蓝色
    await page.screenshot({
      path: resolve("../../docs/design/ui-consistency/kanban-gray-green-tokens.png"),
    });
  });
});
