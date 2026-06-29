# Frontend Spec

The web app is a Next.js App Router application in `apps/web`.

Core frontend choices:

- Next.js / React / TypeScript
- Tailwind CSS
- shadcn/ui-compatible component structure as the UI grows
- TanStack Query for server state
- Zustand for local UI state
- React Hook Form and Zod for forms
- React Flow / xyflow for richer workflow visualization in later phases
- Monaco Editor for richer Provider Manifest editing in later phases

The current Studio dashboard includes:

- Workflow Board MVP: starts `video_production`, shows run status, node status, retry counts, realtime events, and generated artifacts.
- Provider Center MVP: provisions OpenAI-compatible providers, imports Provider Manifests, edits model capability JSON, runs model tests, binds `script_agent_default`, and shows call logs plus usage.
- CineWeave Vault MVP: lists generated artifacts for the active project.

The page remains an operational Studio dashboard shell, not a marketing landing page.
