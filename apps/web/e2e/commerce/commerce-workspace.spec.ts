import { expect, test, type Locator, type Page, type Request, type Route } from "@playwright/test";

const organizationId = "00000000-0000-4000-8000-000000000101";
const workspaceId = "00000000-0000-4000-8000-000000000102";
const projectId = "00000000-0000-4000-8000-000000000103";
const productId = "00000000-0000-4000-8000-000000000104";
const firstUnitId = "00000000-0000-4000-8000-000000000105";
const secondUnitId = "00000000-0000-4000-8000-000000000106";
const firstGenerationId = "00000000-0000-4000-8000-000000000107";
const secondGenerationId = "00000000-0000-4000-8000-000000000108";
const firstPlanId = "00000000-0000-4000-8000-000000000109";
const secondPlanId = "00000000-0000-4000-8000-00000000010a";
const now = "2026-07-23T00:00:00Z";

test.beforeEach(async ({ page }) => {
  await installSession(page);
  await page.route("**/*", mockApiRoute);
});

test("带货项目表单隐藏叙事配置并保留产品与脚本入口", async ({ page }) => {
  await page.goto("/projects/new");
  await page.getByRole("button", { name: /带货视频/ }).click();

  await expect(page.getByLabel("产品名称 *")).toBeVisible();
  await expect(page.getByText("产品图片", { exact: true })).toBeVisible();
  await expect(page.getByText("广告脚本", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "创建并准备分镜方案" })).toBeVisible();
  await expect(page.getByText("视觉手册", { exact: true })).toHaveCount(0);
  await expect(page.getByText("导演手册", { exact: true })).toHaveCount(0);
  await expect(page.getByText("生成方式", { exact: true })).toHaveCount(0);
});

test("带货创建配置 403 显示真实权限错误且不误报模板", async ({ page }) => {
  await installScenarioApiRoute(page, (_request, url) => {
    if (url.pathname === `/api/workspaces/${workspaceId}/commerce/project-options`) {
      return {
        status: 403,
        error: { code: "ACCESS_DENIED", message: "access denied", retryable: false },
      };
    }
    return undefined;
  });
  await page.goto("/projects/new");
  await page.getByRole("button", { name: /带货视频/ }).click();

  await expect(page.getByText("没有执行此操作的权限", { exact: true })).toBeVisible();
  await expect(page.getByText("带货视频工作流模板尚未发布。", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "创建并准备分镜方案" })).toBeDisabled();
});

test("只读成员看不到新建入口且不能直接打开新建页", async ({ page }) => {
  let projectOptionsRequests = 0;
  await installScenarioApiRoute(page, (_request, url) => {
    if (url.pathname === "/api/auth/me") {
      return {
        data: {
          ...(mockApiData(url.pathname, url.searchParams) as Record<string, unknown>),
          permissions: ["organization.read", "workspace.read", "project.read"],
        },
      };
    }
    if (url.pathname === `/api/workspaces/${workspaceId}/commerce/project-options`) {
      projectOptionsRequests += 1;
    }
    return undefined;
  });

  await page.goto("/projects");
  await expect(page.getByRole("link", { name: "新建项目" })).toHaveCount(0);
  await page.goto("/projects/new");
  await expect(page.getByText("当前账号没有创建项目的权限，请联系组织管理员授权。", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "创建项目" })).toHaveCount(0);
  expect(projectOptionsRequests).toBe(0);
});

test("Commerce 工作台只显示专用导航且成片链接不落入叙事页", async ({ page }) => {
  await page.goto(`/projects/${projectId}/commerce/materials`);

  await expect(page.getByRole("link", { name: "商品与脚本" })).toBeVisible();
  await expect(page.getByRole("link", { name: "分镜方案" })).toBeVisible();
  await expect(page.getByRole("link", { name: "视频制作" })).toBeVisible();
  await expect(page.getByRole("link", { name: "成片" })).toHaveAttribute("href", `/projects/${projectId}/commerce/final`);
  await expect(page.getByRole("link", { name: "内容" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "剧本" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "资产" })).toHaveCount(0);
});

test("切换脚本单元后分镜页面不会残留上一单元镜头", async ({ page }) => {
  await page.goto(`/projects/${projectId}/commerce/storyboard`);

  await expect(page.getByText("第一条脚本镜头内容", { exact: true })).toBeVisible();
  await page.getByRole("combobox").first().click();
  await page.getByRole("option", { name: /02 · 第二条脚本/ }).click();
  await expect(page.getByText("第二条脚本镜头内容", { exact: true })).toBeVisible();
  await expect(page.getByText("第一条脚本镜头内容", { exact: true })).toHaveCount(0);
});

test("脚本单元游标分页合并时去重并保留顺序", async ({ page }) => {
  await installScenarioApiRoute(page, (_request, url) => {
    if (url.pathname !== `/api/projects/${projectId}/commerce/script-units`) return undefined;
    if (url.searchParams.get("cursor") === "next-page") {
      return { data: { items: commerceUnits(), hasMore: false, scriptUnitsRevision: 2 } };
    }
    return { data: { items: [commerceUnits()[0]], nextCursor: "next-page", hasMore: true, scriptUnitsRevision: 2 } };
  });
  await page.goto(`/projects/${projectId}/commerce/materials`);
  await expect(page.getByText("第一条脚本", { exact: true })).toHaveCount(1);
  await page.getByRole("button", { name: "加载更多" }).click();
  await expect(page.getByText("第二条脚本", { exact: true })).toBeVisible();
  await expect(page.getByText("第一条脚本", { exact: true })).toHaveCount(1);
});

test("低置信度语言确认提交选中的可执行语言", async ({ page }) => {
  const setupSessionId = "00000000-0000-4000-8000-000000000120";
  const resolutionId = "00000000-0000-4000-8000-000000000121";
  let submitted: Record<string, unknown> | undefined;
  await installScenarioApiRoute(page, async (request, url) => {
    if (url.pathname === `/api/projects/${projectId}`) {
      return { data: { ...commerceProject(), setupSessionId, setupState: "waiting_user_confirmation" } };
    }
    if (url.pathname === `/api/projects/${projectId}/commerce/setup-sessions/${setupSessionId}`) {
      return { data: { id: setupSessionId, projectId, scriptUnitId: firstUnitId, state: "waiting_user_confirmation", revision: 2 } };
    }
    if (url.pathname === `/api/projects/${projectId}/commerce/script-units/${firstUnitId}/language-resolution`) {
      return { data: { id: resolutionId, scriptUnitId: firstUnitId, targetLanguage: "zh-CN", status: "needs_confirmation", needsUserConfirmation: true } };
    }
    if (request.method() === "POST" && url.pathname.endsWith(`/commerce/setup-sessions/${setupSessionId}/language-confirmation`)) {
      submitted = request.postDataJSON() as Record<string, unknown>;
      return { data: { setupSession: { id: setupSessionId, state: "running", revision: 3 }, setupRun: { id: "setup-run-e2e", status: "running" } } };
    }
    return undefined;
  });
  await page.goto(`/projects/${projectId}/commerce/materials`);
  await expect(page.getByText("请确认视频语言", { exact: true })).toBeVisible();
  await page.getByRole("combobox").first().click();
  await page.getByRole("option", { name: "English" }).click();
  await page.getByRole("button", { name: "确认语言" }).click();
  await expect.poll(() => submitted?.targetLanguage).toBe("en-US");
  expect(submitted).toMatchObject({ expectedRevision: 2, resolutionId });
});

test("新增脚本从项目默认值初始化并只展示能力允许的选项", async ({ page }) => {
  await installScenarioApiRoute(page, (_request, url) => {
    if (url.pathname === `/api/projects/${projectId}`) {
      return {
        data: {
          ...commerceProject(),
          scriptUnitDefaults: {
            targetDurationSeconds: 60,
            targetPlatform: "tiktok",
            languageMode: "explicit",
            targetLanguage: "en-US",
          },
        },
      };
    }
    return undefined;
  });
  await page.goto(`/projects/${projectId}/commerce/materials`);
  await page.getByRole("button", { name: "新增脚本" }).first().click();
  const dialog = page.getByRole("dialog", { name: "新增广告脚本" });
  await expect(fieldCombobox(dialog, "目标语言方式")).toContainText("明确指定");
  await expect(fieldCombobox(dialog, "目标语言")).toContainText("English");
  await expect(fieldCombobox(dialog, "目标时长")).toContainText("60 秒");
  await expect(fieldCombobox(dialog, "目标平台")).toContainText("TikTok");
});

test("带货项目设置使用专用分区且不显示叙事手册", async ({ page }) => {
  await page.goto(`/projects/${projectId}/settings`);
  await expect(page.getByText("新脚本默认值", { exact: true })).toBeVisible();
  await expect(page.getByText("音频设置", { exact: true })).toBeVisible();
  await expect(page.getByText("图片与视频模型", { exact: true })).toBeVisible();
  await expect(page.getByText("视觉手册", { exact: true })).toHaveCount(0);
  await expect(page.getByText("导演手册", { exact: true })).toHaveCount(0);
  await expect(page.getByText("角色声音", { exact: true })).toHaveCount(0);
});

test("视频页显示 Render Plan 状态并保留部分失败重试入口", async ({ page }) => {
  const run = commerceProductionRun("partially_succeeded");
  let retried = false;
  await installScenarioApiRoute(page, async (request, url) => {
    if (url.pathname === `/api/projects/${projectId}/commerce/production-runs`) {
      return { data: { items: url.searchParams.get("filter[runType]") === "shot_videos" ? [run] : [] } };
    }
    if (url.pathname === `/api/projects/${projectId}/commerce/production-runs/${run.id}`) {
      return { data: { run, items: [failedProductionItem()] } };
    }
    if (request.method() === "POST" && url.pathname === `/api/projects/${projectId}/commerce/production-runs/${run.id}/retry-failed`) {
      retried = true;
      return { data: { ...run, id: "commerce-run-retry", status: "queued" } };
    }
    const planDetail = url.pathname.match(new RegExp(`^/api/projects/${projectId}/commerce/script-units/([^/]+)/storyboard-plans/([^/]+)$`));
    if (planDetail) {
      const unitId = planDetail[1];
      return { data: { plan: storyboardPlan(unitId), shots: [{ ...storyboardShot(unitId), imageStatus: "succeeded", videoPromptStatus: "succeeded", videoRenderPlanStatus: "planned", videoStatus: "failed" }] } };
    }
    return undefined;
  });
  await page.goto(`/projects/${projectId}/commerce/video`);
  await expect(page.getByText("执行计划：已规划", { exact: true })).toBeVisible();
  await expect(page.getByText("部分完成", { exact: true })).toBeVisible();
  await expect(page.getByText("镜头视频批次部分完成，可重试失败镜头", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "重试失败项" }).click();
  await expect.poll(() => retried).toBe(true);
  await page.reload();
  await expect(page.getByText("部分完成", { exact: true })).toBeVisible();
});

test("单镜头提交期间不会禁用其他镜头的生成按钮", async ({ page }) => {
  let releaseRequest: (() => void) | undefined;
  const requestGate = new Promise<void>((resolve) => { releaseRequest = resolve; });
  let submitted = false;
  await installScenarioApiRoute(page, async (request, url) => {
    const planDetail = url.pathname.match(new RegExp(`^/api/projects/${projectId}/commerce/script-units/([^/]+)/storyboard-plans/([^/]+)$`));
    if (planDetail) {
      const unitId = planDetail[1];
      return { data: { plan: { ...storyboardPlan(unitId), shotCount: 2 }, shots: [storyboardShot(unitId, 1), storyboardShot(unitId, 2)] } };
    }
    if (request.method() === "POST" && url.pathname.endsWith("/video-prompts/generate-batch")) {
      submitted = true;
      await requestGate;
      return { data: commerceProductionRun("queued") };
    }
    return undefined;
  });
  await page.goto(`/projects/${projectId}/commerce/video`);
  const firstShot = page.locator("article").filter({ hasText: "01-01" });
  const secondShot = page.locator("article").filter({ hasText: "01-02" });
  await firstShot.getByRole("button", { name: "生成提示词" }).click();
  await expect.poll(() => submitted).toBe(true);
  await expect(secondShot.getByRole("button", { name: "生成提示词" })).toBeEnabled();
  releaseRequest?.();
});

async function installSession(page: Page) {
  await page.addInitScript(({ organization, workspace }) => {
    window.localStorage.setItem("cineweave.session.v2", JSON.stringify({
      accessToken: "commerce-e2e-access",
      refreshToken: "commerce-e2e-refresh",
      organizationId: organization,
      workspaceId: workspace,
      currentProjectId: "",
    }));
  }, { organization: organizationId, workspace: workspaceId });
}

async function mockApiRoute(route: Route) {
  const request = route.request();
  const url = new URL(request.url());
  if (!url.pathname.startsWith("/api/")) {
    if (url.port === "19281") {
      await route.abort();
      return;
    }
    await route.continue();
    return;
  }

  const data = mockApiData(url.pathname, url.searchParams);
  await route.fulfill({
    status: 200,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify({ data, meta: {} }),
  });
}

type ScenarioResponse = {
  data?: unknown;
  error?: { code: string; message: string; retryable?: boolean };
  status?: number;
};
type ScenarioResolver = (request: Request, url: URL) => ScenarioResponse | undefined | Promise<ScenarioResponse | undefined>;

async function installScenarioApiRoute(page: Page, resolve: ScenarioResolver) {
  await page.unroute("**/*", mockApiRoute);
  await page.route("**/*", async (route) => {
    const request = route.request();
    const url = new URL(request.url());
    if (!url.pathname.startsWith("/api/")) {
      if (url.port === "19281") await route.abort();
      else await route.continue();
      return;
    }
    const scenario = await resolve(request, url);
    if (scenario?.error) {
      await route.fulfill({
        status: scenario.status ?? 500,
        contentType: "application/json; charset=utf-8",
        body: JSON.stringify({ error: scenario.error }),
      });
      return;
    }
    const data = scenario && Object.hasOwn(scenario, "data")
      ? scenario.data
      : mockApiData(url.pathname, url.searchParams);
    await route.fulfill({
      status: scenario?.status ?? 200,
      contentType: "application/json; charset=utf-8",
      body: JSON.stringify({ data, meta: {} }),
    });
  });
}

function mockApiData(pathname: string, search: URLSearchParams): unknown {
  if (pathname === "/api/auth/me") {
    return {
      user: { id: "user-e2e", email: "e2e@example.test", username: "e2e-admin", displayName: "E2E 管理员", status: "active" },
      organizationId,
      workspaceId,
      membership: { id: "membership-e2e", organizationId, userId: "user-e2e", role: "owner", status: "active" },
      permissions: ["project.read", "project.write", "script.read", "script.write", "asset.read", "asset.write", "storyboard.generate", "workflow.read", "workflow.start", "workflow.cancel"],
    };
  }
  if (pathname === "/api/workspaces") {
    return { items: [{ id: workspaceId, organizationId, name: "Commerce E2E", slug: "commerce-e2e" }] };
  }
  if (pathname === `/api/workspaces/${workspaceId}/commerce/project-options`) {
    return {
      workflowTemplateVersionId: "template-version-e2e",
      workflowTemplateVersion: 1,
      templateContentHash: "a".repeat(64),
      videoProductionProfileKey: "single_frame_i2v",
      videoProductionProfileVersion: 1,
      available: true,
      blockers: [],
      durations: [15, 30, 60],
      aspectRatios: ["9:16", "16:9"],
      imageQualities: ["standard", "hd"],
      languageModes: ["auto", "explicit"],
      audioStrategies: ["native_av"],
      audioRequirements: ["preferred", "required"],
      languages: [
        { locale: "zh-CN", label: "简体中文", textAvailable: true, imagePromptAvailable: true, videoPromptAvailable: true, nativeAudioAvailable: true, blockers: [] },
        { locale: "en-US", label: "English", textAvailable: true, imagePromptAvailable: true, videoPromptAvailable: true, nativeAudioAvailable: true, blockers: [] },
      ],
      modelRequirements: [],
    };
  }
  if (pathname === `/api/projects/${projectId}`) {
    return commerceProject();
  }
  if (pathname === `/api/projects/${projectId}/commerce/product`) {
    return commerceProduct();
  }
  if (pathname === `/api/projects/${projectId}/commerce/product/references`) {
    return { items: [] };
  }
  if (pathname === `/api/projects/${projectId}/commerce/script-units`) {
    return { items: commerceUnits(), hasMore: false, scriptUnitsRevision: 2 };
  }
  const planListMatch = pathname.match(new RegExp(`^/api/projects/${projectId}/commerce/script-units/([^/]+)/storyboard-plans$`));
  if (planListMatch) {
    const unitId = planListMatch[1];
    return { items: [storyboardPlan(unitId)] };
  }
  const planDetailMatch = pathname.match(new RegExp(`^/api/projects/${projectId}/commerce/script-units/([^/]+)/storyboard-plans/([^/]+)$`));
  if (planDetailMatch) {
    const unitId = planDetailMatch[1];
    return { plan: storyboardPlan(unitId), shots: [storyboardShot(unitId)] };
  }
  if (pathname === "/api/workflow-runs" || pathname === `/api/projects/${projectId}/commerce/production-runs`) {
    return { items: [] };
  }
  if (pathname.includes("/commerce/product/reference-packs/")) {
    return { items: [] };
  }
  if (pathname === "/api/projects") {
    return { items: [commerceProject()] };
  }
  if (pathname.includes("prompt") || pathname.includes("manual") || pathname.includes("profile")) {
    return { items: [] };
  }
  if (search.has("cursor")) {
    return { items: [], hasMore: false };
  }
  return { items: [] };
}

function commerceProject() {
  return {
    id: projectId,
    organizationId,
    workspaceId,
    name: "多脚本带货项目",
    description: "Commerce browser E2E",
    projectKind: "commerce_video",
    projectType: "commerce_video",
    contentType: null,
    aspectRatio: "9:16",
    videoRatio: "9:16",
    imageQuality: "standard",
    videoProductionState: "ready",
    audioStrategy: "native_av",
    audioRequirement: "preferred",
    revision: 1,
    scriptUnitDefaults: { targetDurationSeconds: 30, targetPlatform: "douyin", languageMode: "auto", targetLanguage: null },
    createdAt: now,
    updatedAt: now,
  };
}

function commerceProduct() {
  return {
    id: productId,
    organizationId,
    projectId,
    currentVersionId: "product-version-e2e",
    status: "active",
    revision: 1,
    scriptUnitsRevision: 2,
    metadata: {},
    currentVersion: {
      id: "product-version-e2e",
      organizationId,
      projectId,
      productId,
      version: 1,
      name: "测试商品",
      brand: "CineWeave",
      sellingPoints: ["真实商品展示"],
      immutableFeatures: ["蓝色包装"],
      prohibitedClaims: [],
      factsSnapshot: {},
      factsHash: "b".repeat(64),
      createdAt: now,
    },
    createdAt: now,
    updatedAt: now,
  };
}

function commerceUnits() {
  return [
    commerceUnit(firstUnitId, firstGenerationId, 1, "第一条脚本"),
    commerceUnit(secondUnitId, secondGenerationId, 2, "第二条脚本"),
  ];
}

function commerceUnit(id: string, generationId: string, unitNo: number, title: string) {
  return {
    id,
    organizationId,
    projectId,
    productId,
    unitNo,
    title,
    sortOrder: unitNo,
    status: "active",
    languageMode: "explicit",
    explicitTargetLanguage: "zh-CN",
    targetDurationSeconds: 30,
    targetPlatform: "douyin",
    draftContent: `${title}正文`,
    activeUnitGenerationId: generationId,
    unitGenerationNo: 1,
    revision: 1,
    metadata: {},
    createdAt: now,
    updatedAt: now,
  };
}

function storyboardPlan(unitId: string) {
  const first = unitId === firstUnitId;
  return {
    id: first ? firstPlanId : secondPlanId,
    organizationId,
    projectId,
    productId,
    productVersionId: "product-version-e2e",
    scriptUnitId: unitId,
    sourceScriptVersionId: `source-${unitId}`,
    localizationId: `localization-${unitId}`,
    referencePackId: `pack-${unitId}`,
    projectProductionGenerationId: "project-generation-e2e",
    scriptUnitGenerationId: first ? firstGenerationId : secondGenerationId,
    commerceWorkflowBindingId: "commerce-binding-e2e",
    commerceWorkflowBindingRevision: 1,
    planRevision: 1,
    revision: 1,
    status: "ready",
    active: true,
    staleState: "fresh",
    targetLanguage: "zh-CN",
    targetDurationSeconds: 30,
    aspectRatio: "9:16",
    timelineTimebase: 90_000,
    fpsNumerator: 24,
    fpsDenominator: 1,
    allowedShotDurations: [5, 10, 15],
    shotCount: 1,
    reviewStatus: "approved",
    planHash: "c".repeat(64),
    projectionHash: "d".repeat(64),
    createdAt: now,
    activatedAt: now,
  };
}

function storyboardShot(unitId: string, ordinal = 1) {
  const first = unitId === firstUnitId;
  return {
    id: `${first ? "shot-first" : "shot-second"}-${ordinal}`,
    storyboardPlanId: first ? firstPlanId : secondPlanId,
    scriptUnitId: unitId,
    scriptUnitGenerationId: first ? firstGenerationId : secondGenerationId,
    revision: 1,
    shotOrdinal: ordinal,
    title: `第${ordinal}镜`,
    durationSeconds: 5,
    startTick: 0,
    endTick: 450_000,
    salesBeat: "hook",
    visualAction: first ? `第一条脚本镜头内容${ordinal === 1 ? "" : ` ${ordinal}`}` : `第二条脚本镜头内容${ordinal === 1 ? "" : ` ${ordinal}`}`,
    shotPurpose: "展示商品",
    composition: "产品特写",
    camera: {},
    voiceoverText: "逐字旁白",
    onscreenText: "立即了解",
    targetLanguage: "zh-CN",
    soundEffects: [],
    musicCue: "",
    requiredProductFeatures: ["蓝色包装"],
    reviewStatus: "approved",
    manualOverride: false,
    staleState: "fresh",
    imagePromptStatus: "approved",
    imageStatus: "succeeded",
    videoPromptStatus: "pending",
    videoStatus: "pending",
    segmentLinks: [],
    productReferences: [],
  };
}

function commerceProductionRun(status: string) {
  return {
    id: "commerce-run-partial",
    organizationId,
    projectId,
    scriptUnitId: firstUnitId,
    runType: "shot_videos",
    status,
    totalItems: 1,
    completedItems: 0,
    failedItems: status === "queued" ? 0 : 1,
    cancelledItems: 0,
    createdAt: now,
    updatedAt: now,
  };
}

function failedProductionItem() {
  return {
    id: "commerce-run-item-failed",
    status: "failed_retryable",
    subject: { storyboardShotId: "shot-first-1" },
    errorCode: "UPSTREAM_TIMEOUT",
    errorMessage: "供应商响应超时",
  };
}

function fieldCombobox(scope: Locator, label: string) {
  return scope.getByText(label, { exact: true }).locator("..").getByRole("combobox");
}
