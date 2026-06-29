"use client";

import {
  Activity,
  Boxes,
  CheckCircle2,
  CircleDollarSign,
  Clapperboard,
  Database,
  FileCode2,
  Gauge,
  KeyRound,
  Layers3,
  Library,
  ListChecks,
  Loader2,
  PlugZap,
  Play,
  Radio,
  RefreshCw,
  Send,
  Workflow,
  XCircle,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";

const apiBase = trimTrailingSlash(process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://127.0.0.1:19092");
const realtimeUrl = process.env.NEXT_PUBLIC_REALTIME_URL ?? "http://127.0.0.1:19093/api/realtime/events";
const defaultPrompt = "A quiet train station at sunrise with cinematic lighting.";
const defaultProviderBaseUrl = "http://127.0.0.1:19180/v1";
const defaultProviderApiKey = "sk-mock-phase3";
const defaultTestPrompt = "Write one cinematic sentence for a sunrise train station.";
const defaultImageTestPrompt = "A cinematic sunrise train station, high detail";
const defaultImageTestSize = "1024x1024";
const defaultVideoTestDuration = "5";
const defaultVideoTestAspectRatio = "16:9";
const defaultVideoTestResolution = "720p";
const defaultCapabilityText = JSON.stringify(
  {
    taskTypes: ["text.generate", "text.stream"],
    inputLimits: { maxTokens: 8192 },
    outputLimits: { maxTokens: 2048 },
    qualityTiers: ["standard"],
    providerOptionsSchema: { type: "object" },
    pricingPolicy: { currency: "USD", inputTokenPer1K: "0.0000", outputTokenPer1K: "0.0000" },
  },
  null,
  2,
);
const defaultManifestText = `kind: ProviderConnector
version: v1
id: browser-demo-http
name: Browser Demo HTTP
transport: http
baseUrl: http://127.0.0.1:19181
auth:
  type: bearer
  header: Authorization
  valueTemplate: "Bearer {{ credential.apiKey }}"
models:
  - id: browser-image
    displayName: Browser Image
    modality: image
    capabilities:
      taskTypes: ["image.generate"]
endpoints:
  image_generate:
    endpointType: sync
    method: POST
    pathTemplate: /images
    requestTemplate:
      prompt: "{{ input.prompt }}"
    responseMapping:
      imageUrl: "$.data.url"
    timeoutMs: 3000`;

const navItems = [
  { label: "Dashboard", shortLabel: "Home", href: "#dashboard", icon: Gauge, active: true },
  { label: "Project Studio", shortLabel: "Project", href: "#project-studio", icon: Clapperboard },
  { label: "Workflow Board", shortLabel: "Flow", href: "#workflow-board", icon: Workflow },
  { label: "Provider Center", shortLabel: "Gateway", href: "#provider-center", icon: KeyRound },
  { label: "Vault", shortLabel: "Vault", href: "#vault", icon: Library },
];

const realtimeEventTypes = [
  "queue.updated",
  "workflow.node.started",
  "artifact.created",
  "workflow.run.completed",
  "workflow.run.failed",
] as const;

type ApiEnvelope<TData = unknown> = {
  requestId: string;
  data?: TData;
  error?: {
    code: string;
    message: string;
    retryable: boolean;
  };
};

type AuthToken = {
  accessToken: string;
  organizationId: string;
};

type Workspace = {
  id: string;
};

type Project = {
  id: string;
};

type WorkflowRun = {
  id: string;
  organizationId: string;
  projectId: string;
  temporalWorkflowId: string;
  status: "queued" | "running" | "succeeded" | "failed" | "cancelled";
  input: Record<string, unknown>;
  output: Record<string, unknown>;
  errorCode?: string;
  errorMessage?: string;
  createdAt: string;
  startedAt?: string;
  completedAt?: string;
};

type WorkflowNodeRun = {
  id: string;
  workflowRunId: string;
  nodeKey: string;
  nodeType: string;
  status: "pending" | "queued" | "running" | "succeeded" | "failed" | "cancelled" | "skipped" | "waiting_review";
  output: Record<string, unknown>;
  retryCount: number;
  errorCode?: string;
  errorMessage?: string;
  startedAt?: string;
  completedAt?: string;
};

type Artifact = {
  id: string;
  workflowRunId?: string;
  type: string;
  storageKey?: string;
  mimeType?: string;
  metadata: Record<string, unknown>;
  createdAt: string;
};

type ProviderConnector = {
  id: string;
  connectorKey: string;
  name: string;
  type: string;
  isOfficial: boolean;
};

type ProviderAccount = {
  id: string;
  connectorKey: string;
  name: string;
  baseUrl?: string;
  authType: string;
  status: string;
  credentialPreview?: string;
};

type ProviderCapability = {
  id: string;
  taskTypes: unknown;
  inputLimits: unknown;
  outputLimits: unknown;
  qualityTiers: unknown;
  providerOptionsSchema: unknown;
  pricingPolicy: unknown;
};

type ProviderModel = {
  id: string;
  providerAccountId: string;
  modelKey: string;
  displayName: string;
  modality: string;
  status: string;
  capabilities: ProviderCapability[];
};

type DiscoveredModel = {
  modelKey: string;
  displayName: string;
  modality: string;
  status: string;
};

type ModelDiscoveryResult = {
  models: DiscoveredModel[];
};

type ModelProfile = {
  id: string;
  profileKey: string;
  name: string;
  purpose: string;
  routingStrategy: string;
  bindings: Array<{
    id: string;
    providerModelId: string;
    priority: number;
    weight: number;
    enabled: boolean;
  }>;
};

type ProviderTestResult = {
  testRunId: string;
  providerCallId: string;
  status: string;
  latencyMs: number;
  errorCode?: string;
  errorMessage?: string;
  normalizedOutput: unknown;
};

type ProviderCallLog = {
  id: string;
  providerAccountId: string;
  providerModelId?: string;
  taskType: string;
  executionMode: string;
  status: string;
  latencyMs?: number;
  errorCode?: string;
  createdAt: string;
};

type ProviderUsageSummary = {
  totalCalls: number;
  failedCalls: number;
  totalCost: string;
  currency: string;
};

type ManifestValidationResult = {
  valid: boolean;
  errors?: Array<{ path: string; message: string }> | null;
};

type SessionState = {
  email: string;
  accessToken: string;
  organizationId: string;
  workspaceId: string;
  projectId: string;
};

type RealtimeEvent = {
  id: string;
  type: string;
  data: Record<string, unknown>;
  createdAt: string;
};

type BusyState = "bootstrap" | "workflow" | null;
type ConnectionState = "idle" | "connecting" | "live" | "reconnecting";
type ProviderTestType = "text_generation_test" | "streaming_test" | "image_generation_test" | "video_generation_test";
type WorkflowType = "text_to_storyboard" | "video_production" | "script_to_storyboard";

const workflowTypes: Array<{ value: WorkflowType; label: string }> = [
  { value: "text_to_storyboard", label: "Text to Storyboard" },
  { value: "video_production", label: "Video Production" },
  { value: "script_to_storyboard", label: "Script to Storyboard" },
];

export function CineWeaveConsole() {
  const [session, setSession] = useState<SessionState | null>(null);
  const [workflowRun, setWorkflowRun] = useState<WorkflowRun | null>(null);
  const [workflowNodes, setWorkflowNodes] = useState<WorkflowNodeRun[]>([]);
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);
  const [events, setEvents] = useState<RealtimeEvent[]>([]);
  const [prompt, setPrompt] = useState(defaultPrompt);
  const [workflowType, setWorkflowType] = useState<WorkflowType>("text_to_storyboard");
  const [busy, setBusy] = useState<BusyState>(null);
  const [error, setError] = useState<string | null>(null);
  const [connection, setConnection] = useState<ConnectionState>("idle");
  const [providerBusy, setProviderBusy] = useState<BusyState | "provider" | "manifest" | "test" | "profile" | null>(null);
  const [providerError, setProviderError] = useState<string | null>(null);
  const [providerNotice, setProviderNotice] = useState<string | null>(null);
  const [connectors, setConnectors] = useState<ProviderConnector[]>([]);
  const [providerAccounts, setProviderAccounts] = useState<ProviderAccount[]>([]);
  const [providerModels, setProviderModels] = useState<ProviderModel[]>([]);
  const [modelProfiles, setModelProfiles] = useState<ModelProfile[]>([]);
  const [providerLogs, setProviderLogs] = useState<ProviderCallLog[]>([]);
  const [providerUsage, setProviderUsage] = useState<ProviderUsageSummary | null>(null);
  const [selectedAccountId, setSelectedAccountId] = useState("");
  const [selectedModelId, setSelectedModelId] = useState("");
  const [providerBaseUrl, setProviderBaseUrl] = useState(defaultProviderBaseUrl);
  const [providerApiKey, setProviderApiKey] = useState(defaultProviderApiKey);
  const [capabilityText, setCapabilityText] = useState(defaultCapabilityText);
  const [providerTestType, setProviderTestType] = useState<ProviderTestType>("text_generation_test");
  const [testPrompt, setTestPrompt] = useState(defaultTestPrompt);
  const [imageTestSize, setImageTestSize] = useState(defaultImageTestSize);
  const [videoTestDuration, setVideoTestDuration] = useState(defaultVideoTestDuration);
  const [videoTestAspectRatio, setVideoTestAspectRatio] = useState(defaultVideoTestAspectRatio);
  const [videoTestResolution, setVideoTestResolution] = useState(defaultVideoTestResolution);
  const [providerTestResult, setProviderTestResult] = useState<ProviderTestResult | null>(null);
  const [manifestText, setManifestText] = useState(defaultManifestText);
  const [manifestValidation, setManifestValidation] = useState<ManifestValidationResult | null>(null);

  const metrics = useMemo(
    () => [
      {
        label: "Active workflows",
        value: workflowRun ? workflowRun.status : "0",
        detail: workflowRun?.temporalWorkflowId ?? "Temporal queue",
        tone: workflowRun?.status === "failed" ? "rose" : "teal",
      },
      { label: "Artifacts", value: String(artifacts.length), detail: "Stored in MinIO", tone: "blue" },
      { label: "Realtime", value: connection, detail: "SSE outbox stream", tone: "amber" },
      { label: "Errors", value: error ? "1" : "0", detail: error ?? "No visible failures", tone: "rose" },
    ],
    [artifacts.length, connection, error, workflowRun],
  );

  const createDemoSession = useCallback(async () => {
    const suffix = Date.now().toString(36);
    const email = `studio+${suffix}@example.com`;
    const auth = await apiRequest<AuthToken>("/api/auth/register", {
      method: "POST",
      body: {
        email,
        password: "Password123!",
        displayName: "Studio Operator",
        organizationName: "CineWeave Studio Demo",
      },
    });
    const workspace = await apiRequest<Workspace>("/api/workspaces", {
      method: "POST",
      token: auth.accessToken,
      organizationId: auth.organizationId,
      body: {
        organizationId: auth.organizationId,
        name: "Default Workspace",
      },
    });
    const project = await apiRequest<Project>("/api/projects", {
      method: "POST",
      token: auth.accessToken,
      organizationId: auth.organizationId,
      body: {
        workspaceId: workspace.id,
        name: "Realtime Storyboard",
        projectType: "short_video",
        aspectRatio: "16:9",
        settings: {},
      },
    });
    const nextSession = {
      email,
      accessToken: auth.accessToken,
      organizationId: auth.organizationId,
      workspaceId: workspace.id,
      projectId: project.id,
    };
    setConnection("connecting");
    setSession(nextSession);
    return nextSession;
  }, []);

  const loadArtifacts = useCallback(async (activeSession: SessionState) => {
    const data = await apiRequest<{ items: Artifact[] }>(
      `/api/artifacts?filter%5BprojectId%5D=${encodeURIComponent(activeSession.projectId)}`,
      {
        token: activeSession.accessToken,
        organizationId: activeSession.organizationId,
      },
    );
    setArtifacts(data.items);
  }, []);

  const loadWorkflowNodes = useCallback(async (activeSession: SessionState, workflowRunId: string) => {
    const data = await apiRequest<{ items: WorkflowNodeRun[] }>(`/api/workflow-runs/${workflowRunId}/nodes`, {
      token: activeSession.accessToken,
      organizationId: activeSession.organizationId,
    });
    setWorkflowNodes(data.items);
  }, []);

  const refreshProviderCenter = useCallback(
    async (activeSession: SessionState) => {
      const [connectorData, accountData, profileData, logData, usageData] = await Promise.all([
        apiRequest<{ items: ProviderConnector[] }>("/api/providers/connectors", {
          token: activeSession.accessToken,
          organizationId: activeSession.organizationId,
        }),
        apiRequest<{ items: ProviderAccount[] }>("/api/providers/accounts?limit=20", {
          token: activeSession.accessToken,
          organizationId: activeSession.organizationId,
        }),
        apiRequest<{ items: ModelProfile[] }>("/api/model-profiles", {
          token: activeSession.accessToken,
          organizationId: activeSession.organizationId,
        }),
        apiRequest<{ items: ProviderCallLog[] }>("/api/provider-call-logs?limit=10", {
          token: activeSession.accessToken,
          organizationId: activeSession.organizationId,
        }),
        apiRequest<ProviderUsageSummary>("/api/provider-usage/summary", {
          token: activeSession.accessToken,
          organizationId: activeSession.organizationId,
        }),
      ]);
      setConnectors(connectorData.items);
      setProviderAccounts(accountData.items);
      setModelProfiles(profileData.items);
      setProviderLogs(logData.items);
      setProviderUsage(usageData);

      const accountID =
        selectedAccountId && accountData.items.some((account) => account.id === selectedAccountId)
          ? selectedAccountId
          : accountData.items[0]?.id ?? "";
      setSelectedAccountId(accountID);
      if (!accountID) {
        setProviderModels([]);
        setSelectedModelId("");
        return;
      }
      const modelData = await apiRequest<{ items: ProviderModel[] }>(`/api/providers/accounts/${accountID}/models`, {
        token: activeSession.accessToken,
        organizationId: activeSession.organizationId,
      });
      setProviderModels(modelData.items);
      setSelectedModelId((current) =>
        current && modelData.items.some((model) => model.id === current) ? current : modelData.items[0]?.id ?? "",
      );
    },
    [selectedAccountId],
  );

  const pollWorkflowRun = useCallback(
    async (activeSession: SessionState, workflowRunId: string) => {
      for (let attempt = 0; attempt < 30; attempt += 1) {
        const run = await apiRequest<WorkflowRun>(`/api/workflow-runs/${workflowRunId}`, {
          token: activeSession.accessToken,
          organizationId: activeSession.organizationId,
        });
        setWorkflowRun(run);
        await loadWorkflowNodes(activeSession, workflowRunId);
        if (isTerminal(run.status)) {
          break;
        }
        await sleep(600);
      }
      await loadWorkflowNodes(activeSession, workflowRunId);
      await loadArtifacts(activeSession);
    },
    [loadArtifacts, loadWorkflowNodes],
  );

  const bootstrap = useCallback(async () => {
    setBusy("bootstrap");
    setError(null);
    setEvents([]);
    setWorkflowRun(null);
    setWorkflowNodes([]);
    setArtifacts([]);
    setConnection("idle");
    try {
      const nextSession = await createDemoSession();
      await loadArtifacts(nextSession);
      await refreshProviderCenter(nextSession);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(null);
    }
  }, [createDemoSession, loadArtifacts, refreshProviderCenter]);

  const runWorkflow = useCallback(async () => {
    setBusy("workflow");
    setError(null);
    setEvents([]);
    setWorkflowNodes([]);
    try {
      const activeSession = session ?? (await createDemoSession());
      const run = await apiRequest<WorkflowRun>("/api/workflow-runs", {
        method: "POST",
        token: activeSession.accessToken,
        organizationId: activeSession.organizationId,
        body: {
          projectId: activeSession.projectId,
          workflowType,
          prompt,
        },
      });
      setWorkflowRun(run);
      await pollWorkflowRun(activeSession, run.id);
    } catch (cause) {
      setError(errorMessage(cause));
    } finally {
      setBusy(null);
    }
  }, [createDemoSession, pollWorkflowRun, prompt, session, workflowType]);

  const refreshProviders = useCallback(async () => {
    setProviderBusy("provider");
    setProviderError(null);
    try {
      const activeSession = session ?? (await createDemoSession());
      await refreshProviderCenter(activeSession);
      setProviderNotice("Provider Center refreshed.");
    } catch (cause) {
      setProviderError(errorMessage(cause));
    } finally {
      setProviderBusy(null);
    }
  }, [createDemoSession, refreshProviderCenter, session]);

  const importManifest = useCallback(async () => {
    setProviderBusy("manifest");
    setProviderError(null);
    setProviderNotice(null);
    try {
      const activeSession = session ?? (await createDemoSession());
      const validation = await apiRequest<ManifestValidationResult>("/api/providers/manifests/validate", {
        method: "POST",
        token: activeSession.accessToken,
        organizationId: activeSession.organizationId,
        body: { manifestText },
      });
      setManifestValidation(validation);
      if (!validation.valid) {
        setProviderNotice("Manifest validation returned errors.");
        return;
      }
      await apiRequest<ProviderConnector>("/api/providers/connectors/import", {
        method: "POST",
        token: activeSession.accessToken,
        organizationId: activeSession.organizationId,
        body: {
          manifestText,
          isOfficial: false,
        },
      });
      await refreshProviderCenter(activeSession);
      setProviderNotice("Manifest imported.");
    } catch (cause) {
      setProviderError(errorMessage(cause));
    } finally {
      setProviderBusy(null);
    }
  }, [createDemoSession, manifestText, refreshProviderCenter, session]);

  const provisionOpenAIProvider = useCallback(async () => {
    setProviderBusy("provider");
    setProviderError(null);
    setProviderNotice(null);
    try {
      const activeSession = session ?? (await createDemoSession());
      const account = await apiRequest<ProviderAccount>("/api/providers/accounts", {
        method: "POST",
        token: activeSession.accessToken,
        organizationId: activeSession.organizationId,
        body: {
          organizationId: activeSession.organizationId,
          connectorKey: "openai_compatible",
          name: "Local OpenAI Compatible",
          baseUrl: providerBaseUrl,
          authType: "bearer",
          credential: { apiKey: providerApiKey },
          config: {
            modelsEndpoint: "/models",
            chatCompletionsEndpoint: "/chat/completions",
            imagesGenerationsEndpoint: "/images/generations",
            timeoutMs: 3000,
          },
        },
      });
      setSelectedAccountId(account.id);
      const discovery = await apiRequest<ModelDiscoveryResult>(`/api/providers/accounts/${account.id}/discover-models`, {
        method: "POST",
        token: activeSession.accessToken,
        organizationId: activeSession.organizationId,
      });
      const discovered = discovery.models[0] ?? {
        modelKey: "mock-gpt-4o-mini",
        displayName: "Mock GPT 4o Mini",
        modality: "text",
        status: "active",
      };
      const model = await apiRequest<ProviderModel>(`/api/providers/accounts/${account.id}/models`, {
        method: "POST",
        token: activeSession.accessToken,
        organizationId: activeSession.organizationId,
        body: {
          modelKey: discovered.modelKey,
          displayName: discovered.displayName,
          modality: discovered.modality || "text",
          status: discovered.status || "active",
          capabilities: parseCapabilityInput(capabilityText),
        },
      });
      setSelectedModelId(model.id);
      await ensureScriptProfileBinding(activeSession, model.id);
      await refreshProviderCenter(activeSession);
      setProviderNotice("Provider, model, capability, and script_agent_default binding are ready.");
    } catch (cause) {
      setProviderError(errorMessage(cause));
    } finally {
      setProviderBusy(null);
    }
  }, [capabilityText, createDemoSession, providerApiKey, providerBaseUrl, refreshProviderCenter, session]);

  const saveCapability = useCallback(async () => {
    if (!session || !selectedModelId) {
      setProviderError("Select a model before saving capability.");
      return;
    }
    setProviderBusy("provider");
    setProviderError(null);
    try {
      const model = providerModels.find((item) => item.id === selectedModelId);
      if (!model) {
        throw new Error("Selected model is not loaded.");
      }
      await apiRequest<ProviderModel>(`/api/providers/models/${selectedModelId}`, {
        method: "PATCH",
        token: session.accessToken,
        organizationId: session.organizationId,
        body: {
          modelKey: model.modelKey,
          displayName: model.displayName,
          modality: model.modality,
          status: model.status,
          capabilities: parseCapabilityInput(capabilityText),
        },
      });
      await refreshProviderCenter(session);
      setProviderNotice("Model capability saved.");
    } catch (cause) {
      setProviderError(errorMessage(cause));
    } finally {
      setProviderBusy(null);
    }
  }, [capabilityText, providerModels, refreshProviderCenter, selectedModelId, session]);

  const runProviderTest = useCallback(async () => {
    if (!session || !selectedModelId) {
      setProviderError("Select a model before running a test.");
      return;
    }
    setProviderBusy("test");
    setProviderError(null);
    setProviderTestResult(null);
    try {
      const input =
        providerTestType === "image_generation_test"
          ? { prompt: testPrompt || defaultImageTestPrompt, size: imageTestSize, quality: "standard", projectId: session.projectId }
          : providerTestType === "video_generation_test"
            ? {
                prompt: testPrompt || defaultImageTestPrompt,
                duration: Number(videoTestDuration) || Number(defaultVideoTestDuration),
                aspectRatio: videoTestAspectRatio || defaultVideoTestAspectRatio,
                resolution: videoTestResolution || defaultVideoTestResolution,
                projectId: session.projectId,
              }
            : { prompt: testPrompt };
      const result = await apiRequest<ProviderTestResult>(`/api/providers/models/${selectedModelId}/test`, {
        method: "POST",
        token: session.accessToken,
        organizationId: session.organizationId,
        body: {
          testType: providerTestType,
          input,
        },
      });
      setProviderTestResult(result);
      await refreshProviderCenter(session);
      await loadArtifacts(session);
      setProviderNotice("Provider test completed.");
    } catch (cause) {
      setProviderError(errorMessage(cause));
    } finally {
      setProviderBusy(null);
    }
  }, [
    imageTestSize,
    loadArtifacts,
    providerTestType,
    refreshProviderCenter,
    selectedModelId,
    session,
    testPrompt,
    videoTestAspectRatio,
    videoTestDuration,
    videoTestResolution,
  ]);

  const bindScriptProfile = useCallback(async () => {
    if (!session || !selectedModelId) {
      setProviderError("Select a model before binding a profile.");
      return;
    }
    setProviderBusy("profile");
    setProviderError(null);
    try {
      await ensureScriptProfileBinding(session, selectedModelId);
      await refreshProviderCenter(session);
      setProviderNotice("Model bound to script_agent_default.");
    } catch (cause) {
      setProviderError(errorMessage(cause));
    } finally {
      setProviderBusy(null);
    }
  }, [refreshProviderCenter, selectedModelId, session]);

  const changeProviderAccount = useCallback(
    async (accountID: string) => {
      setSelectedAccountId(accountID);
      setProviderError(null);
      if (!session || !accountID) {
        setProviderModels([]);
        setSelectedModelId("");
        return;
      }
      try {
        const modelData = await apiRequest<{ items: ProviderModel[] }>(`/api/providers/accounts/${accountID}/models`, {
          token: session.accessToken,
          organizationId: session.organizationId,
        });
        setProviderModels(modelData.items);
        setSelectedModelId(modelData.items[0]?.id ?? "");
      } catch (cause) {
        setProviderError(errorMessage(cause));
      }
    },
    [session],
  );

  const projectId = session?.projectId ?? "";
  useEffect(() => {
    if (!projectId) {
      return;
    }
    const source = new EventSource(`${realtimeUrl}?projectId=${encodeURIComponent(projectId)}`);
    source.onopen = () => setConnection("live");
    source.onerror = () => setConnection("reconnecting");
    const listeners: Array<[string, EventListener]> = [];
    for (const type of realtimeEventTypes) {
      const listener = ((event: MessageEvent<string>) => {
        setEvents((current) =>
          [
            {
              id: `${type}-${Date.now()}-${current.length}`,
              type,
              data: parseEventPayload(event.data),
              createdAt: new Date().toISOString(),
            },
            ...current,
          ].slice(0, 12),
        );
      }) as EventListener;
      source.addEventListener(type, listener);
      listeners.push([type, listener]);
    }
    return () => {
      for (const [type, listener] of listeners) {
        source.removeEventListener(type, listener);
      }
      source.close();
    };
  }, [projectId]);

  const selectedAccount = providerAccounts.find((account) => account.id === selectedAccountId);
  const selectedModel = providerModels.find((model) => model.id === selectedModelId);
  const scriptProfile = modelProfiles.find((profile) => profile.profileKey === "script_agent_default" || profile.purpose === "script");

  return (
    <main className="min-h-screen">
      <aside className="fixed inset-y-0 left-0 hidden w-64 border-r border-[var(--line)] bg-white px-4 py-5 lg:block">
        <div className="flex items-center gap-3 border-b border-[var(--line)] pb-5">
          <div className="grid size-10 place-items-center rounded bg-[var(--foreground)] text-white">
            <Layers3 size={20} aria-hidden="true" />
          </div>
          <div>
            <p className="text-sm font-semibold">CineWeave Studio</p>
            <p className="text-xs text-[var(--muted)]">CineWeave production platform</p>
          </div>
        </div>

        <nav className="mt-5 space-y-1" aria-label="Primary">
          {navItems.map((item) => (
            <a
              key={item.label}
              href={item.href}
              aria-current={item.active ? "page" : undefined}
              className={`flex h-10 items-center gap-3 rounded px-3 text-sm ${
                item.active
                  ? "bg-[var(--panel-soft)] font-medium text-[var(--foreground)]"
                  : "text-[var(--muted)] hover:bg-[var(--panel-soft)]"
              }`}
            >
              <item.icon size={17} aria-hidden="true" />
              {item.label}
            </a>
          ))}
        </nav>
      </aside>

      <section className="lg:pl-64">
        <header className="border-b border-[var(--line)] bg-white px-5 py-4 lg:px-8">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h1 className="text-xl font-semibold">Dashboard</h1>
              <p className="mt-1 text-sm text-[var(--muted)]">
                Local Workflow, Artifact, and Provider Gateway control surfaces.
              </p>
            </div>
            <div className="flex items-center gap-2 rounded border border-[var(--line)] bg-white px-3 py-2 text-sm text-[var(--muted)]">
              <Radio size={16} aria-hidden="true" />
              {apiBase}
            </div>
          </div>
          <nav className="mt-4 grid grid-cols-5 gap-1 lg:hidden" aria-label="Primary">
            {navItems.map((item) => (
              <a
                key={item.label}
                href={item.href}
                title={item.label}
                aria-current={item.active ? "page" : undefined}
                className={`grid min-h-14 place-items-center rounded border px-1 text-[11px] ${
                  item.active
                    ? "border-[var(--foreground)] bg-[var(--panel-soft)] font-medium"
                    : "border-[var(--line)] text-[var(--muted)]"
                }`}
              >
                <item.icon size={17} aria-hidden="true" />
                <span className="mt-1 max-w-full truncate">{item.shortLabel}</span>
              </a>
            ))}
          </nav>
        </header>

        <div id="dashboard" className="px-5 py-6 lg:px-8">
          <section className="grid gap-4 md:grid-cols-2 xl:grid-cols-4" aria-label="Metrics">
            {metrics.map((metric) => (
              <div
                key={metric.label}
                className={`min-h-32 rounded border border-[var(--line)] border-l-4 bg-white p-4 ${toneClass[metric.tone]}`}
              >
                <p className="text-sm text-[var(--muted)]">{metric.label}</p>
                <p className="mt-3 truncate text-2xl font-semibold">{metric.value}</p>
                <p className="mt-2 truncate text-xs text-[var(--muted)]">{metric.detail}</p>
              </div>
            ))}
          </section>

          <section className="mt-6 grid gap-5 xl:grid-cols-[1.25fr_0.75fr]">
            <div id="workflow-board" className="rounded border border-[var(--line)] bg-white">
              <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--line)] px-4 py-3">
                <div className="flex items-center gap-3">
                  <Activity size={18} aria-hidden="true" />
                  <h2 className="text-sm font-semibold">Workflow Board</h2>
                </div>
                <StatusPill status={workflowRun?.status ?? connection} />
              </div>

              <div className="grid gap-4 p-4 lg:grid-cols-[0.95fr_1.05fr]">
                <div className="space-y-3">
                  <label className="block text-xs font-medium text-[var(--muted)]" htmlFor="workflow-prompt">
                    Prompt
                  </label>
                  <textarea
                    id="workflow-prompt"
                    value={prompt}
                    onChange={(event) => setPrompt(event.target.value)}
                    className="min-h-32 w-full resize-y rounded border border-[var(--line)] bg-white px-3 py-2 text-sm outline-none focus:border-[var(--foreground)]"
                  />
                  <label className="grid gap-1 text-xs font-medium text-[var(--muted)]" htmlFor="workflow-type">
                    Workflow Type
                    <select
                      id="workflow-type"
                      value={workflowType}
                      onChange={(event) => setWorkflowType(event.target.value as WorkflowType)}
                      className="h-10 rounded border border-[var(--line)] bg-white px-3 text-sm text-[var(--foreground)] outline-none focus:border-[var(--foreground)]"
                    >
                      {workflowTypes.map((item) => (
                        <option key={item.value} value={item.value}>
                          {item.label}
                        </option>
                      ))}
                    </select>
                  </label>
                  <div className="grid gap-2 sm:grid-cols-2">
                    <button
                      type="button"
                      onClick={bootstrap}
                      disabled={busy !== null}
                      className="inline-flex h-10 items-center justify-center gap-2 rounded border border-[var(--line)] bg-white px-3 text-sm font-medium disabled:opacity-60"
                    >
                      {busy === "bootstrap" ? <Loader2 size={16} className="animate-spin" /> : <CheckCircle2 size={16} />}
                      Initialize
                    </button>
                    <button
                      type="button"
                      onClick={runWorkflow}
                      disabled={busy !== null}
                      aria-label="Run Workflow"
                      title="Run Workflow"
                      className="inline-flex h-10 items-center justify-center gap-2 rounded bg-[var(--foreground)] px-3 text-sm font-medium text-white disabled:opacity-60"
                    >
                      {busy === "workflow" ? <Loader2 size={16} className="animate-spin" /> : <Play size={16} />}
                      Run
                    </button>
                  </div>
                  {error ? (
                    <div className="flex gap-2 rounded border border-[var(--rose)] bg-white px-3 py-2 text-sm text-[var(--rose)]">
                      <XCircle size={16} className="mt-0.5 shrink-0" />
                      <span>{error}</span>
                    </div>
                  ) : null}
                </div>

                <div className="grid gap-3">
                  <InfoRow label="User" value={session?.email ?? "Not initialized"} />
                  <InfoRow label="Project" value={session?.projectId ?? "Pending"} />
                  <InfoRow label="Type" value={workflowType} />
                  <InfoRow label="Workflow" value={workflowRun?.id ?? "No run"} />
                  <InfoRow label="Temporal" value={workflowRun?.temporalWorkflowId ?? "No workflow"} />
                  <InfoRow label="Output" value={artifactSummary(artifacts)} />
                </div>

                <div className="lg:col-span-2">
                  <div className="rounded border border-[var(--line)]">
                    <div className="flex items-center justify-between gap-3 border-b border-[var(--line)] px-3 py-2">
                      <p className="text-xs font-medium text-[var(--muted)]">Node Runs</p>
                      <p className="text-xs text-[var(--muted)]">{workflowNodes.length} nodes</p>
                    </div>
                    <div className="grid gap-0 divide-y divide-[var(--line)]">
                      {workflowNodes.length > 0 ? (
                        workflowNodes.map((node) => (
                          <div key={node.id} className="grid gap-2 px-3 py-2 md:grid-cols-[1fr_auto_auto] md:items-center">
                            <div>
                              <p className="text-sm font-medium">{nodeLabel(node.nodeKey)}</p>
                              <p className="text-xs text-[var(--muted)]">{node.nodeType}</p>
                            </div>
                            <StatusPill status={node.status} />
                            <p className="text-xs text-[var(--muted)]">retry {node.retryCount}</p>
                          </div>
                        ))
                      ) : (
                        <p className="px-3 py-5 text-sm text-[var(--muted)]">No node runs yet.</p>
                      )}
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <div id="system-planes" className="rounded border border-[var(--line)] bg-white">
              <div className="flex items-center gap-3 border-b border-[var(--line)] px-4 py-3">
                <Boxes size={18} aria-hidden="true" />
                <h2 className="text-sm font-semibold">Core Planes</h2>
              </div>
              <div className="grid gap-3 p-4">
                <Plane label="Control" value="Go API Server" />
                <Plane label="Provider" value="CineWeave Gateway" />
                <Plane label="Workflow" value="Temporal" />
                <Plane label="Data" value="PostgreSQL / Redis / S3 / NATS" />
                <Plane label="Cost" value="Provider call logs" icon={<CircleDollarSign size={16} />} />
              </div>
            </div>
          </section>

          <section id="provider-center" className="mt-5 rounded border border-[var(--line)] bg-white">
            <div className="flex flex-wrap items-center justify-between gap-3 border-b border-[var(--line)] px-4 py-3">
              <div className="flex items-center gap-3">
                <KeyRound size={18} aria-hidden="true" />
                <h2 className="text-sm font-semibold">Provider Center</h2>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <StatusPill status={`${providerAccounts.length} accounts`} />
                <button
                  type="button"
                  onClick={refreshProviders}
                  disabled={providerBusy !== null}
                  className="inline-flex h-9 items-center justify-center gap-2 rounded border border-[var(--line)] bg-white px-3 text-sm font-medium disabled:opacity-60"
                >
                  <RefreshCw size={15} />
                  Refresh
                </button>
              </div>
            </div>

            <div className="grid gap-5 p-4">
              {(providerError || providerNotice) && (
                <div
                  className={`rounded border px-3 py-2 text-sm ${
                    providerError ? "border-[var(--rose)] text-[var(--rose)]" : "border-[var(--teal)] text-[var(--teal)]"
                  }`}
                >
                  {providerError ?? providerNotice}
                </div>
              )}

              <div className="grid gap-4 lg:grid-cols-[0.95fr_1.05fr]">
                <div className="rounded border border-[var(--line)] p-4">
                  <div className="flex items-center gap-2">
                    <PlugZap size={17} />
                    <h3 className="text-sm font-semibold">Add Provider</h3>
                  </div>
                  <div className="mt-4 grid gap-3 md:grid-cols-2">
                    <label className="grid gap-1 text-xs font-medium text-[var(--muted)]">
                      Base URL
                      <input
                        value={providerBaseUrl}
                        onChange={(event) => setProviderBaseUrl(event.target.value)}
                        className="h-10 rounded border border-[var(--line)] px-3 text-sm text-[var(--foreground)] outline-none focus:border-[var(--foreground)]"
                      />
                    </label>
                    <label className="grid gap-1 text-xs font-medium text-[var(--muted)]">
                      API Key
                      <input
                        value={providerApiKey}
                        onChange={(event) => setProviderApiKey(event.target.value)}
                        type="password"
                        className="h-10 rounded border border-[var(--line)] px-3 text-sm text-[var(--foreground)] outline-none focus:border-[var(--foreground)]"
                      />
                    </label>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-2">
                    <button
                      type="button"
                      onClick={provisionOpenAIProvider}
                      disabled={providerBusy !== null}
                      className="inline-flex h-10 items-center justify-center gap-2 rounded bg-[var(--foreground)] px-3 text-sm font-medium text-white disabled:opacity-60"
                    >
                      {providerBusy === "provider" ? <Loader2 size={16} className="animate-spin" /> : <CheckCircle2 size={16} />}
                      Provision OpenAI-compatible
                    </button>
                  </div>
                  <div className="mt-4 grid gap-2 md:grid-cols-3">
                    <InfoRow label="Connectors" value={String(connectors.length)} />
                    <InfoRow label="Selected Account" value={selectedAccount?.name ?? "None"} />
                    <InfoRow label="Usage" value={providerUsage ? `${providerUsage.totalCalls} calls` : "No data"} />
                  </div>
                </div>

                <div className="rounded border border-[var(--line)] p-4">
                  <div className="flex items-center gap-2">
                    <ListChecks size={17} />
                    <h3 className="text-sm font-semibold">Models and Profiles</h3>
                  </div>
                  <div className="mt-4 grid gap-3 md:grid-cols-2">
                    <label className="grid gap-1 text-xs font-medium text-[var(--muted)]">
                      Account
                      <select
                        value={selectedAccountId}
                        onChange={(event) => {
                          void changeProviderAccount(event.target.value);
                        }}
                        className="h-10 rounded border border-[var(--line)] bg-white px-3 text-sm text-[var(--foreground)] outline-none focus:border-[var(--foreground)]"
                      >
                        <option value="">No account</option>
                        {providerAccounts.map((account) => (
                          <option key={account.id} value={account.id}>
                            {account.name}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="grid gap-1 text-xs font-medium text-[var(--muted)]">
                      Model
                      <select
                        value={selectedModelId}
                        onChange={(event) => setSelectedModelId(event.target.value)}
                        className="h-10 rounded border border-[var(--line)] bg-white px-3 text-sm text-[var(--foreground)] outline-none focus:border-[var(--foreground)]"
                      >
                        <option value="">No model</option>
                        {providerModels.map((model) => (
                          <option key={model.id} value={model.id}>
                            {model.displayName}
                          </option>
                        ))}
                      </select>
                    </label>
                  </div>
                  <div className="mt-3 grid gap-2 md:grid-cols-3">
                    <InfoRow label="Model Key" value={selectedModel?.modelKey ?? "None"} />
                    <InfoRow label="Capability Rows" value={String(selectedModel?.capabilities.length ?? 0)} />
                    <InfoRow label="Profile" value={scriptProfile?.profileKey ?? "Unbound"} />
                  </div>
                  <div className="mt-3 flex flex-wrap gap-2">
                    <button
                      type="button"
                      onClick={saveCapability}
                      disabled={providerBusy !== null || !selectedModelId}
                      className="inline-flex h-9 items-center justify-center gap-2 rounded border border-[var(--line)] bg-white px-3 text-sm font-medium disabled:opacity-60"
                    >
                      <Database size={15} />
                      Save Capability
                    </button>
                    <button
                      type="button"
                      onClick={bindScriptProfile}
                      disabled={providerBusy !== null || !selectedModelId}
                      className="inline-flex h-9 items-center justify-center gap-2 rounded border border-[var(--line)] bg-white px-3 text-sm font-medium disabled:opacity-60"
                    >
                      <CheckCircle2 size={15} />
                      Bind Profile
                    </button>
                  </div>
                </div>
              </div>

              <div className="grid gap-4 xl:grid-cols-[0.95fr_1.05fr]">
                <div className="rounded border border-[var(--line)] p-4">
                  <div className="flex items-center gap-2">
                    <Database size={17} />
                    <h3 className="text-sm font-semibold">Capability Editor</h3>
                  </div>
                  <textarea
                    value={capabilityText}
                    onChange={(event) => setCapabilityText(event.target.value)}
                    spellCheck={false}
                    className="mt-3 min-h-48 w-full resize-y rounded border border-[var(--line)] bg-white px-3 py-2 font-mono text-xs outline-none focus:border-[var(--foreground)]"
                  />
                </div>

                <div className="grid gap-4">
                  <div className="rounded border border-[var(--line)] p-4">
                    <div className="flex items-center gap-2">
                      <Send size={17} />
                      <h3 className="text-sm font-semibold">Test Center</h3>
                    </div>
                    <div className="mt-3 grid gap-3 md:grid-cols-2 xl:grid-cols-[0.75fr_1fr_0.55fr_0.45fr_0.55fr_0.55fr_auto]">
                      <select
                        value={providerTestType}
                        onChange={(event) => setProviderTestType(event.target.value as ProviderTestType)}
                        className="h-10 rounded border border-[var(--line)] bg-white px-3 text-sm text-[var(--foreground)] outline-none focus:border-[var(--foreground)]"
                      >
                        <option value="text_generation_test">Text</option>
                        <option value="streaming_test">Stream</option>
                        <option value="image_generation_test">Image</option>
                        <option value="video_generation_test">Video</option>
                      </select>
                      <input
                        value={testPrompt}
                        onChange={(event) => setTestPrompt(event.target.value)}
                        className="h-10 rounded border border-[var(--line)] px-3 text-sm outline-none focus:border-[var(--foreground)]"
                      />
                      <input
                        value={imageTestSize}
                        onChange={(event) => setImageTestSize(event.target.value)}
                        disabled={providerTestType !== "image_generation_test"}
                        className="h-10 rounded border border-[var(--line)] px-3 text-sm outline-none focus:border-[var(--foreground)] disabled:bg-[var(--soft)] disabled:text-[var(--muted)]"
                      />
                      <input
                        value={videoTestDuration}
                        onChange={(event) => setVideoTestDuration(event.target.value)}
                        disabled={providerTestType !== "video_generation_test"}
                        aria-label="Video Duration"
                        title="Video Duration"
                        className="h-10 rounded border border-[var(--line)] px-3 text-sm outline-none focus:border-[var(--foreground)] disabled:bg-[var(--soft)] disabled:text-[var(--muted)]"
                      />
                      <input
                        value={videoTestAspectRatio}
                        onChange={(event) => setVideoTestAspectRatio(event.target.value)}
                        disabled={providerTestType !== "video_generation_test"}
                        aria-label="Video Aspect Ratio"
                        title="Video Aspect Ratio"
                        className="h-10 rounded border border-[var(--line)] px-3 text-sm outline-none focus:border-[var(--foreground)] disabled:bg-[var(--soft)] disabled:text-[var(--muted)]"
                      />
                      <input
                        value={videoTestResolution}
                        onChange={(event) => setVideoTestResolution(event.target.value)}
                        disabled={providerTestType !== "video_generation_test"}
                        aria-label="Video Resolution"
                        title="Video Resolution"
                        className="h-10 rounded border border-[var(--line)] px-3 text-sm outline-none focus:border-[var(--foreground)] disabled:bg-[var(--soft)] disabled:text-[var(--muted)]"
                      />
                      <button
                        type="button"
                        onClick={runProviderTest}
                        disabled={providerBusy !== null || !selectedModelId}
                        className="inline-flex h-10 items-center justify-center gap-2 rounded bg-[var(--foreground)] px-3 text-sm font-medium text-white disabled:opacity-60"
                      >
                        {providerBusy === "test" ? <Loader2 size={16} className="animate-spin" /> : <Play size={16} />}
                        Test
                      </button>
                    </div>
                    {providerTestResult && (
                      <div className="mt-3 grid gap-2 md:grid-cols-3">
                        <InfoRow label="Status" value={providerTestResult.status} />
                        <InfoRow label="Latency" value={`${providerTestResult.latencyMs} ms`} />
                        <InfoRow label="Call Log" value={providerTestResult.providerCallId} />
                        <InfoRow label="Async Task" value={normalizedOutputString(providerTestResult.normalizedOutput, "providerAsyncTaskId")} />
                        <InfoRow label="Artifact" value={normalizedOutputString(providerTestResult.normalizedOutput, "artifactId")} />
                        <InfoRow label="Media" value={normalizedOutputString(providerTestResult.normalizedOutput, "mediaFileId")} />
                        <InfoRow label="Storage" value={normalizedOutputString(providerTestResult.normalizedOutput, "storageKey")} />
                      </div>
                    )}
                  </div>

                  <div className="rounded border border-[var(--line)] p-4">
                    <div className="flex items-center gap-2">
                      <CircleDollarSign size={17} />
                      <h3 className="text-sm font-semibold">Logs and Usage</h3>
                    </div>
                    <div className="mt-3 grid gap-2 md:grid-cols-3">
                      <InfoRow label="Total Calls" value={String(providerUsage?.totalCalls ?? 0)} />
                      <InfoRow label="Failed Calls" value={String(providerUsage?.failedCalls ?? 0)} />
                      <InfoRow label="Cost" value={`${providerUsage?.totalCost ?? "0.0000"} ${providerUsage?.currency ?? "USD"}`} />
                    </div>
                    <div className="mt-3 divide-y divide-[var(--line)] rounded border border-[var(--line)]">
                      {providerLogs.length > 0 ? (
                        providerLogs.map((log) => (
                          <div key={log.id} className="grid gap-1 px-3 py-2 md:grid-cols-[1fr_auto]">
                            <p className="truncate text-sm font-medium">{log.taskType}</p>
                            <p className="text-xs text-[var(--muted)]">{log.status}</p>
                            <p className="truncate text-xs text-[var(--muted)]">{log.id}</p>
                            <p className="text-xs text-[var(--muted)]">{log.latencyMs ?? 0} ms</p>
                          </div>
                        ))
                      ) : (
                        <p className="px-3 py-5 text-sm text-[var(--muted)]">No provider logs yet.</p>
                      )}
                    </div>
                  </div>
                </div>
              </div>

              <div className="rounded border border-[var(--line)] p-4">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div className="flex items-center gap-2">
                    <FileCode2 size={17} />
                    <h3 className="text-sm font-semibold">Manifest Import</h3>
                  </div>
                  <button
                    type="button"
                    onClick={importManifest}
                    disabled={providerBusy !== null}
                    className="inline-flex h-9 items-center justify-center gap-2 rounded border border-[var(--line)] bg-white px-3 text-sm font-medium disabled:opacity-60"
                  >
                    {providerBusy === "manifest" ? <Loader2 size={15} className="animate-spin" /> : <FileCode2 size={15} />}
                    Validate and Import
                  </button>
                </div>
                <textarea
                  value={manifestText}
                  onChange={(event) => setManifestText(event.target.value)}
                  spellCheck={false}
                  className="mt-3 min-h-56 w-full resize-y rounded border border-[var(--line)] bg-white px-3 py-2 font-mono text-xs outline-none focus:border-[var(--foreground)]"
                />
                {manifestValidation && (
                  <div className="mt-3 rounded border border-[var(--line)] px-3 py-2 text-sm">
                    <p className={manifestValidation.valid ? "text-[var(--teal)]" : "text-[var(--rose)]"}>
                      {manifestValidation.valid ? "Manifest is valid." : "Manifest has validation errors."}
                    </p>
                    {(manifestValidation.errors?.length ?? 0) > 0 && (
                      <ul className="mt-2 grid gap-1 text-xs text-[var(--muted)]">
                        {manifestValidation.errors?.map((issue) => (
                          <li key={`${issue.path}-${issue.message}`}>
                            {issue.path}: {issue.message}
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                )}
              </div>
            </div>
          </section>

          <section className="mt-5 grid gap-5 lg:grid-cols-[1.1fr_0.9fr]">
            <div id="project-studio" className="rounded border border-[var(--line)] bg-white">
              <div className="flex items-center gap-3 border-b border-[var(--line)] px-4 py-3">
                <Workflow size={18} aria-hidden="true" />
                <h2 className="text-sm font-semibold">Realtime Events</h2>
              </div>
              <div className="divide-y divide-[var(--line)]">
                {events.length > 0 ? (
                  events.map((event) => (
                    <div key={event.id} className="grid gap-1 px-4 py-3">
                      <div className="flex items-center justify-between gap-3">
                        <p className="text-sm font-medium">{event.type}</p>
                        <time className="text-xs text-[var(--muted)]">{new Date(event.createdAt).toLocaleTimeString()}</time>
                      </div>
                      <p className="truncate text-xs text-[var(--muted)]">{eventSummary(event.data)}</p>
                    </div>
                  ))
                ) : (
                  <p className="px-4 py-8 text-sm text-[var(--muted)]">No realtime events yet.</p>
                )}
              </div>
            </div>

            <div id="vault" className="rounded border border-[var(--line)] bg-white">
              <div className="flex items-center gap-3 border-b border-[var(--line)] px-4 py-3">
                <Library size={18} aria-hidden="true" />
                <h2 className="text-sm font-semibold">CineWeave Vault</h2>
              </div>
              <div className="divide-y divide-[var(--line)]">
                {artifacts.length > 0 ? (
                  artifacts.map((artifact) => (
                    <div key={artifact.id} className="grid gap-1 px-4 py-3">
                      <div className="flex items-center justify-between gap-3">
                        <p className="text-sm font-medium">{artifact.type}</p>
                        <p className="text-xs text-[var(--muted)]">{artifact.mimeType ?? "application/json"}</p>
                      </div>
                      <p className="truncate text-xs text-[var(--muted)]">{artifact.storageKey}</p>
                    </div>
                  ))
                ) : (
                  <p className="px-4 py-8 text-sm text-[var(--muted)]">No artifacts yet.</p>
                )}
              </div>
            </div>
          </section>
        </div>
      </section>
    </main>
  );
}

function Plane({ label, value, icon }: { label: string; value: string; icon?: ReactNode }) {
  return (
    <div className="flex min-h-12 items-center justify-between gap-4 rounded border border-[var(--line)] px-3">
      <div>
        <p className="text-xs text-[var(--muted)]">{label}</p>
        <p className="text-sm font-medium">{value}</p>
      </div>
      {icon}
    </div>
  );
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded border border-[var(--line)] px-3 py-2">
      <p className="text-xs text-[var(--muted)]">{label}</p>
      <p className="mt-1 truncate text-sm font-medium">{value}</p>
    </div>
  );
}

function StatusPill({ status }: { status: string }) {
  const isGood = status === "succeeded" || status === "live";
  const isBad = status === "failed" || status === "cancelled";
  const className = isGood
    ? "border-[var(--teal)] text-[var(--teal)]"
    : isBad
      ? "border-[var(--rose)] text-[var(--rose)]"
      : "border-[var(--amber)] text-[var(--amber)]";
  return <span className={`rounded border px-2 py-1 text-xs font-medium ${className}`}>{status}</span>;
}

const toneClass: Record<string, string> = {
  teal: "border-l-[var(--teal)]",
  blue: "border-l-[var(--blue)]",
  amber: "border-l-[var(--amber)]",
  rose: "border-l-[var(--rose)]",
};

async function apiRequest<TData>(
  path: string,
  options: {
    method?: "GET" | "POST" | "PATCH" | "DELETE";
    token?: string;
    organizationId?: string;
    body?: unknown;
  } = {},
): Promise<TData> {
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
  const response = await fetch(`${apiBase}${path}`, {
    method: options.method ?? "GET",
    headers,
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  });
  const envelope = (await response.json()) as ApiEnvelope<TData>;
  if (!response.ok || envelope.error) {
    throw new Error(envelope.error?.message ?? `Request failed with ${response.status}`);
  }
  if (envelope.data === undefined) {
    throw new Error("API response did not include data");
  }
  return envelope.data;
}

async function ensureScriptProfileBinding(activeSession: SessionState, providerModelId: string) {
  let profiles = await apiRequest<{ items: ModelProfile[] }>("/api/model-profiles", {
    token: activeSession.accessToken,
    organizationId: activeSession.organizationId,
  });
  let profile = profiles.items.find((item) => item.profileKey === "script_agent_default" || item.purpose === "script");
  if (!profile) {
    try {
      profile = await apiRequest<ModelProfile>("/api/model-profiles", {
        method: "POST",
        token: activeSession.accessToken,
        organizationId: activeSession.organizationId,
        body: {
          profileKey: "script_agent_default",
          name: "Script Agent Default",
          purpose: "script",
          routingStrategy: "priority",
          fallbackStrategy: {},
        },
      });
    } catch {
      profiles = await apiRequest<{ items: ModelProfile[] }>("/api/model-profiles", {
        token: activeSession.accessToken,
        organizationId: activeSession.organizationId,
      });
      profile = profiles.items.find((item) => item.profileKey === "script_agent_default" || item.purpose === "script");
    }
  }
  if (!profile) {
    throw new Error("script_agent_default profile is not available.");
  }
  if (profile.bindings.some((binding) => binding.providerModelId === providerModelId)) {
    return profile;
  }
  return apiRequest<ModelProfile>(`/api/model-profiles/${profile.id}/bindings`, {
    method: "POST",
    token: activeSession.accessToken,
    organizationId: activeSession.organizationId,
    body: {
      providerModelId,
      priority: 100,
      weight: 100,
      enabled: true,
    },
  });
}

function parseCapabilityInput(value: string) {
  const decoded = JSON.parse(value) as Record<string, unknown>;
  return {
    taskTypes: decoded.taskTypes ?? ["text.generate"],
    inputLimits: decoded.inputLimits ?? {},
    outputLimits: decoded.outputLimits ?? {},
    qualityTiers: decoded.qualityTiers ?? [],
    providerOptionsSchema: decoded.providerOptionsSchema ?? {},
    pricingPolicy: decoded.pricingPolicy ?? {},
  };
}

function trimTrailingSlash(value: string) {
  return value.replace(/\/+$/, "");
}

function isTerminal(status: WorkflowRun["status"]) {
  return status === "succeeded" || status === "failed" || status === "cancelled";
}

function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

function errorMessage(cause: unknown) {
  return cause instanceof Error ? cause.message : "Unexpected error";
}

function parseEventPayload(raw: string) {
  try {
    return JSON.parse(raw) as Record<string, unknown>;
  } catch {
    return { raw };
  }
}

function eventSummary(data: Record<string, unknown>) {
  const artifactId = typeof data.artifactId === "string" ? data.artifactId : "";
  const workflowRunId = typeof data.workflowRunId === "string" ? data.workflowRunId : "";
  const storageKey = typeof data.storageKey === "string" ? data.storageKey : "";
  return artifactId || workflowRunId || storageKey || JSON.stringify(data);
}

function artifactSummary(items: Artifact[]) {
  if (items.length === 0) {
    return "No artifacts";
  }
  return `${items.length} artifact${items.length === 1 ? "" : "s"}`;
}

function normalizedOutputString(output: unknown, key: string) {
  if (!output || typeof output !== "object" || Array.isArray(output)) {
    return "-";
  }
  const value = (output as Record<string, unknown>)[key];
  return typeof value === "string" && value ? value : "-";
}

function nodeLabel(nodeKey: string) {
  const labels: Record<string, string> = {
    script_to_storyboard: "Script to Storyboard",
    storyboard_to_image: "Storyboard to Image",
    storyboard_to_video: "Storyboard to Video",
    video_compose: "Video Compose",
    quality_check: "Quality Check",
    text_to_storyboard: "Text to Storyboard",
    generate_storyboard_text: "Generate Storyboard Text",
    generate_storyboard_image: "Generate Storyboard Image",
  };
  return labels[nodeKey] ?? nodeKey;
}
