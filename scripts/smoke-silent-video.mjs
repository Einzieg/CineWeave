import { readFile } from "node:fs/promises";
import path from "node:path";

const apiBase = trimTrailingSlash(process.env.CINEWEAVE_API_BASE_URL ?? "http://localhost:8080");
const demoEmail = process.env.CINEWEAVE_DEMO_EMAIL ?? "demo@cineweave.local";
const demoPassword = process.env.CINEWEAVE_DEMO_PASSWORD ?? "Password123!";
const demoOrg = process.env.CINEWEAVE_DEMO_ORG ?? "CineWeave Demo";
const mockProviderBaseUrl = trimTrailingSlash(process.env.CINEWEAVE_MOCK_PROVIDER_BASE_URL ?? "http://mock-provider:19180");
const timeoutSeconds = Number(process.env.CINEWEAVE_SMOKE_TIMEOUT_SECONDS ?? "300");

const workspaceName = "Silent Video MVP Workspace";
const projectName = "Silent Video MVP Project";

async function main() {
  const deadline = Date.now() + timeoutSeconds * 1000;
  step(`waiting for API at ${apiBase}`);
  await waitForSystemStatus(deadline);

  const session = await authenticate();
  const workspace = await ensureWorkspace(session);
  const project = await ensureProject(session, workspace.id);

  const openAIAccount = await ensureAccount(session, {
    connectorKey: "openai_compatible",
    name: "CineWeave Mock OpenAI Compatible",
    baseUrl: `${mockProviderBaseUrl}/v1`,
    authType: "bearer",
    credential: { apiKey: "sk-cineweave-mock" },
    config: {
      modelsEndpoint: "/models",
      chatCompletionsEndpoint: "/chat/completions",
      imagesGenerationsEndpoint: "/images/generations",
      timeoutMs: 10000,
    },
  });
  const textModel = await ensureModel(session, openAIAccount.id, {
    modelKey: "cw-mock-text",
    displayName: "CineWeave Mock Text",
    modality: "text",
    taskTypes: ["text.generate", "text.stream"],
  });
  const imageModel = await ensureModel(session, openAIAccount.id, {
    modelKey: "cw-mock-image",
    displayName: "CineWeave Mock Image",
    modality: "image",
    taskTypes: ["image.generate"],
  });
  await ensureProfileBinding(session, "script_agent_default", "Script Agent Default", "script", textModel.id);
  await ensureProfileBinding(session, "image_generation_default", "Image Generation Default", "image_generation", imageModel.id);

  const manifestText = await loadMockVideoManifest();
  await importConnector(session, manifestText);
  const videoAccount = await ensureAccount(session, {
    connectorKey: "cineweave-mock-video",
    name: "CineWeave Mock Video",
    baseUrl: mockProviderBaseUrl,
    authType: "bearer",
    credential: { apiKey: "sk-cineweave-mock" },
    config: {
      videoCreateEndpointKey: "video_create",
      videoPollEndpointKey: "video_poll",
      videoCancelEndpointKey: "video_cancel",
    },
  });
  const videoModel = await ensureModel(session, videoAccount.id, {
    modelKey: "cw-mock-video",
    displayName: "CineWeave Mock Video",
    modality: "video",
    taskTypes: ["video.generate"],
  });
  await ensureProfileBinding(session, "video_generation_default", "Video Generation Default", "video_generation", videoModel.id);

  const run = await startWorkflow(session, project.id);
  const completed = await pollWorkflow(session, run.id, deadline);
  assert(completed.status === "succeeded", `workflow did not succeed: ${completed.status} ${completed.errorCode ?? ""} ${completed.errorMessage ?? ""}`);

  const shots = await listShots(session, completed.id);
  assert(shots.length === 2, `expected 2 shots, got ${shots.length}`);
  for (const shot of shots) {
    assert(shot.imageArtifactId && shot.imagePreviewUrl, `shot ${shot.shotNo} is missing image artifact preview`);
    assert(shot.videoArtifactId && shot.videoPreviewUrl, `shot ${shot.shotNo} is missing video artifact preview`);
  }

  const artifacts = await listArtifacts(session, project.id);
  const finalArtifactId = stringValue(completed.output?.finalVideoArtifactId) ?? artifacts.find((item) => item.type === "final_video")?.id;
  const finalStorageKey = stringValue(completed.output?.finalVideoStorageKey) ?? artifacts.find((item) => item.type === "final_video")?.storageKey;
  assert(finalArtifactId, "workflow output is missing finalVideoArtifactId");
  assert(finalStorageKey, "workflow output is missing finalVideoStorageKey");
  const finalPreview = await createArtifactPreview(session, finalArtifactId);
  assert(finalPreview.url, "final video preview URL was not returned");

  console.log("");
  console.log("Silent Video MVP smoke test passed");
  console.log(
    JSON.stringify(
      {
        workflowRunId: completed.id,
        projectId: project.id,
        finalVideoArtifactId: finalArtifactId,
        finalVideoStorageKey: finalStorageKey,
        finalVideoPreviewUrl: finalPreview.url,
        shots: shots.map((shot) => ({
          shotNo: shot.shotNo,
          imageArtifactId: shot.imageArtifactId,
          videoArtifactId: shot.videoArtifactId,
        })),
      },
      null,
      2,
    ),
  );
}

async function waitForSystemStatus(deadline) {
  let lastError = "";
  while (Date.now() < deadline) {
    try {
      const status = await request("/api/system/status", { step: "system status" });
      if (status.status === "ok") {
        step(`system status ok: ${JSON.stringify(status.services)}`);
        return status;
      }
    } catch (cause) {
      lastError = cause.message;
    }
    await sleep(2000);
  }
  throw new Error(`API did not become ready before timeout: ${lastError}`);
}

async function authenticate() {
  step(`authenticating ${demoEmail}`);
  try {
    const auth = await request("/api/auth/register", {
      method: "POST",
      body: {
        email: demoEmail,
        password: demoPassword,
        displayName: "CineWeave Demo",
        organizationName: demoOrg,
      },
      step: "register demo user",
    });
    return toSession(auth);
  } catch (cause) {
    if (!(cause instanceof ApiError) || cause.status !== 409) {
      throw cause;
    }
    const auth = await request("/api/auth/login", {
      method: "POST",
      body: { email: demoEmail, password: demoPassword },
      step: "login demo user",
    });
    return toSession(auth);
  }
}

async function ensureWorkspace(session) {
  step("ensuring workspace");
  const listed = await request("/api/workspaces", authOptions(session, { step: "list workspaces" }));
  const existing = listed.items.find((item) => item.name === workspaceName) ?? listed.items[0];
  if (existing?.name === workspaceName) {
    return existing;
  }
  return request(
    "/api/workspaces",
    authOptions(session, {
      method: "POST",
      body: { organizationId: session.organizationId, name: workspaceName },
      step: "create workspace",
    }),
  );
}

async function ensureProject(session, workspaceId) {
  step("ensuring project");
  const listed = await request(`/api/projects?filter%5BworkspaceId%5D=${encodeURIComponent(workspaceId)}`, authOptions(session, { step: "list projects" }));
  const existing = listed.items.find((item) => item.name === projectName);
  if (existing) {
    session.projectId = existing.id;
    return existing;
  }
  const project = await request(
    "/api/projects",
    authOptions(session, {
      method: "POST",
      body: {
        workspaceId,
        name: projectName,
        projectType: "short_video",
        aspectRatio: "16:9",
        settings: {},
      },
      step: "create project",
    }),
  );
  session.projectId = project.id;
  return project;
}

async function importConnector(session, manifestText) {
  step("importing mock video manifest");
  await request(
    "/api/providers/connectors/import",
    authOptions(session, {
      method: "POST",
      body: { manifestText, isOfficial: false },
      step: "import mock video connector",
    }),
  );
}

async function ensureAccount(session, input) {
  step(`ensuring provider account ${input.name}`);
  const listed = await request("/api/providers/accounts?limit=100", authOptions(session, { step: "list provider accounts" }));
  const existing = listed.items.find(
    (item) =>
      item.connectorKey === input.connectorKey &&
      item.name === input.name &&
      trimTrailingSlash(item.baseUrl ?? "") === trimTrailingSlash(input.baseUrl),
  );
  if (existing) {
    return existing;
  }
  return request(
    "/api/providers/accounts",
    authOptions(session, {
      method: "POST",
      body: {
        organizationId: session.organizationId,
        connectorKey: input.connectorKey,
        name: input.name,
        baseUrl: input.baseUrl,
        authType: input.authType,
        credential: input.credential,
        config: input.config,
      },
      step: `create provider account ${input.name}`,
    }),
  );
}

async function ensureModel(session, accountId, input) {
  step(`ensuring provider model ${input.modelKey}`);
  const listed = await request(`/api/providers/accounts/${accountId}/models`, authOptions(session, { step: `list models ${input.modelKey}` }));
  const existing = listed.items.find((item) => item.modelKey === input.modelKey && item.modality === input.modality);
  const body = {
    modelKey: input.modelKey,
    displayName: input.displayName,
    modality: input.modality,
    status: "active",
    capabilities: capability(input.taskTypes, input.modality),
  };
  if (existing) {
    return request(
      `/api/providers/models/${existing.id}`,
      authOptions(session, {
        method: "PATCH",
        body,
        step: `update provider model ${input.modelKey}`,
      }),
    );
  }
  return request(
    `/api/providers/accounts/${accountId}/models`,
    authOptions(session, {
      method: "POST",
      body,
      step: `create provider model ${input.modelKey}`,
    }),
  );
}

async function ensureProfileBinding(session, profileKey, name, purpose, providerModelId) {
  step(`ensuring model profile ${profileKey}`);
  let profiles = await request("/api/model-profiles", authOptions(session, { step: "list model profiles" }));
  let profile = profiles.items.find((item) => item.profileKey === profileKey);
  if (!profile) {
    profile = await request(
      "/api/model-profiles",
      authOptions(session, {
        method: "POST",
        body: {
          profileKey,
          name,
          purpose,
          routingStrategy: "priority_with_fallback",
          fallbackStrategy: { enabled: true, maxAttempts: 3 },
        },
        step: `create model profile ${profileKey}`,
      }),
    );
  }
  if (profile.bindings.some((binding) => binding.providerModelId === providerModelId && binding.enabled)) {
    return profile;
  }
  return request(
    `/api/model-profiles/${profile.id}/bindings`,
    authOptions(session, {
      method: "POST",
      body: { providerModelId, priority: 100, weight: 100, enabled: true },
      step: `bind model profile ${profileKey}`,
    }),
  );
}

async function startWorkflow(session, projectId) {
  step("starting video_production workflow");
  return request(
    "/api/workflow-runs",
    authOptions(session, {
      method: "POST",
      body: {
        projectId,
        workflowType: "video_production",
        prompt: "Create a two-shot silent cinematic demo of a sunrise train station.",
        input: {
          maxShots: 2,
          duration: 2,
          aspectRatio: "16:9",
          resolution: "720p",
          pollIntervalSeconds: 1,
          maxPolls: 30,
          skipCompose: false,
        },
      },
      step: "create workflow run",
    }),
  );
}

async function pollWorkflow(session, workflowRunId, deadline) {
  step(`polling workflow ${workflowRunId}`);
  let latest;
  while (Date.now() < deadline) {
    latest = await request(`/api/workflow-runs/${workflowRunId}`, authOptions(session, { step: "get workflow run" }));
    process.stdout.write(`workflow ${latest.status}\r`);
    if (["succeeded", "failed", "cancelled"].includes(latest.status)) {
      console.log(`workflow ${latest.status}        `);
      return latest;
    }
    await sleep(2000);
  }
  throw new Error(`workflow did not finish before timeout; last status=${latest?.status ?? "unknown"}`);
}

async function listShots(session, workflowRunId) {
  step("fetching storyboard shots with previews");
  const data = await request(
    `/api/workflow-runs/${workflowRunId}/shots?includePreviewUrl=true&previewExpiresSeconds=900`,
    authOptions(session, { step: "list workflow shots" }),
  );
  return data.items;
}

async function listArtifacts(session, projectId) {
  step("fetching artifacts with previews");
  const data = await request(
    `/api/artifacts?filter%5BprojectId%5D=${encodeURIComponent(projectId)}&includePreviewUrl=true&previewExpiresSeconds=900`,
    authOptions(session, { step: "list artifacts" }),
  );
  return data.items;
}

async function createArtifactPreview(session, artifactId) {
  step("creating final video preview URL");
  return request(
    `/api/artifacts/${artifactId}/preview-url`,
    authOptions(session, {
      method: "POST",
      body: { expiresSeconds: 900 },
      step: "create final video preview URL",
    }),
  );
}

async function loadMockVideoManifest() {
  const manifestPath = path.join(process.cwd(), "examples", "providers", "mock-video-provider.yaml");
  const text = await readFile(manifestPath, "utf8");
  return text.replace(/^baseUrl:\s*.*$/m, `baseUrl: ${mockProviderBaseUrl}`);
}

async function request(pathname, options = {}) {
  const url = `${apiBase}${pathname}`;
  const headers = new Headers();
  if (options.body !== undefined) {
    headers.set("Content-Type", "application/json");
  }
  if (options.token) {
    headers.set("Authorization", `Bearer ${options.token}`);
  }
  if (options.organizationId) {
    headers.set("X-Organization-Id", options.organizationId);
  }
  let response;
  let bodyText = "";
  try {
    response = await fetch(url, {
      method: options.method ?? "GET",
      headers,
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    });
    bodyText = await response.text();
  } catch (cause) {
    throw new Error(`${options.step ?? "request"} failed: ${cause.message}`);
  }
  let envelope;
  try {
    envelope = bodyText ? JSON.parse(bodyText) : {};
  } catch {
    throw new ApiError(response.status, `${options.step ?? "request"} failed with non-JSON response: ${bodyText.slice(0, 1000)}`);
  }
  if (!response.ok || envelope.error) {
    throw new ApiError(
      response.status,
      `${options.step ?? "request"} failed (${response.status}) ${envelope.error?.code ?? ""}: ${envelope.error?.message ?? bodyText}`,
      envelope.error,
      bodyText,
    );
  }
  return envelope.data;
}

function authOptions(session, options = {}) {
  return { ...options, token: session.accessToken, organizationId: session.organizationId };
}

function toSession(auth) {
  return {
    email: auth.user?.email ?? demoEmail,
    accessToken: auth.accessToken,
    organizationId: auth.organizationId,
    workspaceId: "",
    projectId: "",
  };
}

function capability(taskTypes, modality) {
  return {
    taskTypes,
    inputLimits: modality === "video" ? { maxPromptChars: 4000 } : { maxTokens: 8192 },
    outputLimits: modality === "video" ? { maxDurationSeconds: 15 } : { maxTokens: 2048 },
    qualityTiers: ["standard"],
    providerOptionsSchema: { type: "object" },
    pricingPolicy:
      modality === "video"
        ? { currency: "USD", videoCostFlat: "0.0000" }
        : { currency: "USD", inputTokenPer1K: "0.0000", outputTokenPer1K: "0.0000", imageCostFlat: "0.0000" },
  };
}

function stringValue(value) {
  return typeof value === "string" && value.trim() ? value : null;
}

function trimTrailingSlash(value) {
  return value.replace(/\/+$/, "");
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function step(message) {
  console.log(`[smoke] ${message}`);
}

function assert(condition, message) {
  if (!condition) {
    throw new Error(message);
  }
}

class ApiError extends Error {
  constructor(status, message, error, bodyText) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.error = error;
    this.bodyText = bodyText;
  }
}

main().catch((cause) => {
  console.error("");
  console.error(`Silent Video MVP smoke test failed: ${cause.message}`);
  if (cause instanceof ApiError && cause.bodyText) {
    console.error(cause.bodyText);
  }
  process.exit(1);
});
