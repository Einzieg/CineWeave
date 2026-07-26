import { expect, test, type Page, type Request, type Route } from "@playwright/test";

const organizationId = "00000000-0000-4000-8000-000000000101";
const workspaceId = "00000000-0000-4000-8000-000000000102";
const projectId = "00000000-0000-4000-8000-000000000103";
const productId = "00000000-0000-4000-8000-000000000104";
const scriptUnitId = "00000000-0000-4000-8000-000000000105";
const productReferenceId = "00000000-0000-4000-8000-000000000106";
const now = "2026-07-26T00:00:00Z";

test.beforeEach(async ({ page }) => {
  await installSession(page);
  await page.route("**/*", mockApiRoute);
});

test("带货项目创建页只保留项目基本配置", async ({ page }) => {
  await page.goto("/projects/new");
  await page.getByRole("button", { name: /带货视频/ }).click();

  await expect(page.getByLabel("项目名称 *")).toBeVisible();
  await expect(page.getByLabel("项目简介")).toBeVisible();
  await expect(page.getByText("画面比例", { exact: true })).toBeVisible();
  await expect(page.getByText("图片质量", { exact: true })).toHaveCount(0);
  await expect(page.getByLabel("产品名称 *")).toHaveCount(0);
  await expect(page.getByText("产品图片", { exact: true })).toHaveCount(0);
  await expect(page.getByText("广告脚本", { exact: true })).toHaveCount(0);
  await expect(page.getByText("视觉手册", { exact: true })).toHaveCount(0);
  await expect(page.getByText("导演手册", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "创建项目" })).toBeEnabled();
});

test("带货项目创建后直接进入商品配置且不启动旧准备流程", async ({ page }) => {
  let createBody: Record<string, unknown> | undefined;
  await installScenarioApiRoute(page, async (request, url) => {
    if (request.method() === "POST" && url.pathname === "/api/projects") {
      createBody = request.postDataJSON() as Record<string, unknown>;
      return { data: commerceProject(), status: 201 };
    }
    return undefined;
  });

  await page.goto("/projects/new");
  await page.getByRole("button", { name: /带货视频/ }).click();
  await page.getByLabel("项目名称 *").fill("头盔直生成项目");
  await page.getByRole("button", { name: "创建项目" }).click();

  await expect(page).toHaveURL(`/projects/${projectId}/commerce/materials`);
  expect(createBody).toMatchObject({
    workspaceId,
    name: "头盔直生成项目",
    projectKind: "commerce_video",
    videoRatio: "9:16",
    defaultLanguageMode: "auto",
  });
  expect(createBody).not.toHaveProperty("productName");
  expect(createBody).not.toHaveProperty("script");
  expect(createBody).not.toHaveProperty("storyboardStrategy");
});

test("只读成员不能打开新建页", async ({ page }) => {
  await installScenarioApiRoute(page, (_request, url) => {
    if (url.pathname === "/api/auth/me") {
      return {
        data: {
          ...currentSession(),
          permissions: ["organization.read", "workspace.read", "project.read"],
        },
      };
    }
    return undefined;
  });

  await page.goto("/projects/new");
  await expect(page.getByText("当前账号没有创建项目的权限，请联系组织管理员授权。", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "创建项目" })).toHaveCount(0);
});

test("带货项目工作台只显示商品配置、视频生成和项目设置", async ({ page }) => {
  await page.goto(`/projects/${projectId}/commerce/materials`);

  await expect(page.getByRole("link", { name: "商品配置" })).toHaveAttribute(
    "href",
    `/projects/${projectId}/commerce/materials`,
  );
  await expect(page.getByRole("link", { name: "视频生成" })).toHaveAttribute(
    "href",
    `/projects/${projectId}/commerce/video`,
  );
  await expect(page.getByRole("link", { name: "项目设置" })).toHaveAttribute(
    "href",
    `/projects/${projectId}/settings`,
  );
  await expect(page.getByRole("link", { name: "分镜方案" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "成片" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "商品与脚本" })).toHaveCount(0);
});

test("商品配置独立管理商品资料和默认参考图", async ({ page }) => {
  await page.goto(`/projects/${projectId}/commerce/materials`);

  await expect(page.getByRole("heading", { name: "商品配置" })).toBeVisible();
  await expect(page.getByLabel("商品名称")).toHaveValue("测试头盔");
  await expect(page.getByText("商品参考图", { exact: true })).toBeVisible();
  await expect(page.getByText("商品正面", { exact: true })).toBeVisible();
  await expect(page.getByText("主图", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "保存商品" })).toBeEnabled();
  await expect(page.getByText("广告脚本", { exact: true })).toHaveCount(0);
});

test("广告脚本按视频模型可执行时长直接生成视频", async ({ page }) => {
  let createVideoBody: Record<string, unknown> | undefined;
  await installScenarioApiRoute(page, async (request, url) => {
    if (
      request.method() === "POST"
      && url.pathname === `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/direct-videos`
    ) {
      createVideoBody = request.postDataJSON() as Record<string, unknown>;
      return { data: directVideoJob("queued"), status: 202 };
    }
    return undefined;
  });

  await page.goto(`/projects/${projectId}/commerce/video`);
  const scriptRow = page.locator("article").filter({ hasText: "头盔通勤广告" });
  await expect(scriptRow.getByText("未生成", { exact: true })).toBeVisible();
  await scriptRow.getByRole("button", { name: "生成视频" }).click();

  const dialog = page.getByRole("dialog", { name: "生成广告视频" });
  await expect(dialog.getByRole("button", { name: "6 秒", exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "10 秒", exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "12 秒", exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "16 秒", exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "15 秒" })).toHaveCount(0);
  await expect(dialog.getByText("商品主图", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "开始生成" })).toBeEnabled();
  await dialog.getByRole("button", { name: "开始生成" }).click();

  await expect.poll(() => createVideoBody).toBeTruthy();
  expect(createVideoBody).toMatchObject({
    durationSeconds: 6,
    resolution: "720p",
    aspectRatio: "9:16",
    generateAudio: true,
    references: [{ sourceType: "product", sourceId: productReferenceId }],
  });
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
    if (url.port === "19281") await route.abort();
    else await route.continue();
    return;
  }
  await fulfillAPI(route, 200, mockApiData(url.pathname));
}

type ScenarioResponse = {
  data?: unknown;
  error?: { code: string; message: string; retryable?: boolean };
  status?: number;
};

type ScenarioResolver = (
  request: Request,
  url: URL,
) => ScenarioResponse | undefined | Promise<ScenarioResponse | undefined>;

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
    await fulfillAPI(
      route,
      scenario?.status ?? 200,
      scenario && Object.hasOwn(scenario, "data") ? scenario.data : mockApiData(url.pathname),
    );
  });
}

async function fulfillAPI(route: Route, status: number, data: unknown) {
  await route.fulfill({
    status,
    contentType: "application/json; charset=utf-8",
    body: JSON.stringify({ data, meta: {} }),
  });
}

function mockApiData(pathname: string): unknown {
  if (pathname === "/api/auth/me") return currentSession();
  if (pathname === "/api/workspaces") {
    return { items: [{ id: workspaceId, organizationId, name: "Commerce E2E", slug: "commerce-e2e" }] };
  }
  if (pathname === "/api/projects") return { items: [commerceProject()] };
  if (pathname === `/api/projects/${projectId}`) return commerceProject();
  if (pathname === `/api/projects/${projectId}/commerce/product`) return commerceProduct();
  if (pathname === `/api/projects/${projectId}/commerce/product/references`) {
    return { items: [productReference()] };
  }
  if (pathname === `/api/projects/${projectId}/commerce/video-options`) return directVideoOptions();
  if (pathname === `/api/projects/${projectId}/commerce/script-units`) {
    return { items: [commerceScriptUnit()], hasMore: false, scriptUnitsRevision: 1 };
  }
  if (pathname === `/api/projects/${projectId}/commerce/direct-videos`) return { items: [] };
  if (pathname === `/api/projects/${projectId}/commerce/script-units/${scriptUnitId}/references`) {
    return { items: [] };
  }
  if (pathname === "/api/workflow-runs") return { items: [] };
  if (pathname.includes("prompt") || pathname.includes("manual") || pathname.includes("profile")) {
    return { items: [] };
  }
  return { items: [] };
}

function currentSession() {
  return {
    user: {
      id: "user-e2e",
      email: "e2e@example.test",
      username: "e2e-admin",
      displayName: "E2E 管理员",
      status: "active",
    },
    organizationId,
    workspaceId,
    membership: {
      id: "membership-e2e",
      organizationId,
      userId: "user-e2e",
      role: "owner",
      status: "active",
    },
    permissions: [
      "project.read",
      "project.write",
      "script.read",
      "script.write",
      "asset.read",
      "asset.write",
      "workflow.read",
      "workflow.start",
      "workflow.cancel",
    ],
  };
}

function commerceProject() {
  return {
    id: projectId,
    organizationId,
    workspaceId,
    name: "头盔直生成项目",
    description: "Commerce direct video E2E",
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
    scriptUnitDefaults: {
      targetDurationSeconds: 6,
      targetPlatform: "other",
      languageMode: "auto",
      targetLanguage: null,
    },
    createdAt: now,
    updatedAt: now,
  };
}

function commerceProduct() {
  return {
    id: productId,
    organizationId,
    projectId,
    currentVersionId: "00000000-0000-4000-8000-000000000111",
    status: "active",
    revision: 1,
    scriptUnitsRevision: 1,
    metadata: { notes: "保持商品真实外观" },
    currentVersion: {
      id: "00000000-0000-4000-8000-000000000111",
      organizationId,
      projectId,
      productId,
      version: 1,
      name: "测试头盔",
      brand: "CineWeave",
      sellingPoints: ["轻量", "通风"],
      immutableFeatures: { items: ["蓝色外壳"] },
      prohibitedClaims: [],
      factsSnapshot: {},
      factsHash: "b".repeat(64),
      createdAt: now,
    },
    createdAt: now,
    updatedAt: now,
  };
}

function productReference() {
  return {
    id: productReferenceId,
    organizationId,
    projectId,
    productId,
    artifactId: "00000000-0000-4000-8000-000000000112",
    mediaFileId: "00000000-0000-4000-8000-000000000113",
    referenceRole: "front",
    isPrimary: true,
    ordinal: 0,
    mimeType: "image/png",
    width: 1024,
    height: 1024,
    byteSize: 1024,
    contentHash: "c".repeat(64),
    status: "active",
    revision: 1,
    previewUrl: "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==",
    createdAt: now,
    updatedAt: now,
  };
}

function commerceScriptUnit() {
  return {
    id: scriptUnitId,
    organizationId,
    projectId,
    productId,
    unitNo: 1,
    title: "头盔通勤广告",
    sortOrder: 1,
    status: "active",
    languageMode: "auto",
    targetDurationSeconds: 6,
    targetPlatform: "other",
    draftContent: "镜头中展示商品，并使用马来语介绍头盔的轻量与通风设计。",
    revision: 1,
    metadata: {},
    currentSourceVersion: {
      id: "00000000-0000-4000-8000-000000000114",
      content: "镜头中展示商品，并使用马来语介绍头盔的轻量与通风设计。",
      status: "active",
      revision: 1,
    },
    createdAt: now,
    updatedAt: now,
  };
}

function directVideoOptions() {
  return {
    contractVersion: "commerce-direct-video/v1",
    projectProductionGenerationId: "00000000-0000-4000-8000-000000000121",
    videoProductionBindingId: "00000000-0000-4000-8000-000000000122",
    videoProductionBindingRevision: 1,
    videoProductionProfileVersionId: "00000000-0000-4000-8000-000000000123",
    videoProductionProfileSnapshotHash: "d".repeat(64),
    defaultAspectRatio: "9:16",
    defaultResolution: "720p",
    executableDurationSeconds: [6, 10, 12, 16],
    resolutions: ["720p"],
    aspectRatios: ["9:16", "16:9"],
    routes: [{
      routeKey: "route-e2e",
      modelProfileId: "00000000-0000-4000-8000-000000000124",
      modelProfileKey: "video_generation_default",
      modelProfileBindingId: "00000000-0000-4000-8000-000000000125",
      providerModelId: "00000000-0000-4000-8000-000000000126",
      providerAccountId: "00000000-0000-4000-8000-000000000127",
      providerModelKey: "video-e2e",
      priority: 100,
      weight: 100,
      variantKey: "image-to-video",
      capabilitySnapshotHash: "e".repeat(64),
      executableDurationSeconds: [6, 10, 12, 16],
      resolutions: ["720p"],
      aspectRatios: ["9:16", "16:9"],
      inputContract: {
        contractKey: "first_frame",
        requestMode: "async_create",
        slots: [{
          role: "first_frame",
          mediaType: "image",
          semantics: "product_visual_reference",
          min: 1,
          max: 1,
          ordered: true,
        }],
      },
      nativeAudio: { support: "supported", supportsDialogue: true, supportsVoiceover: true },
    }],
  };
}

function directVideoJob(status: string) {
  return {
    id: "00000000-0000-4000-8000-000000000131",
    organizationId,
    projectId,
    productId,
    productVersionId: "00000000-0000-4000-8000-000000000111",
    scriptUnitId,
    scriptUnitRevision: 1,
    requestedDurationSeconds: 6,
    aspectRatio: "9:16",
    resolution: "720p",
    generateAudio: true,
    status,
    attemptGeneration: 1,
    references: [],
    createdAt: now,
    updatedAt: now,
  };
}
