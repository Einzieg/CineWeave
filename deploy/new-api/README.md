# CineWeave New API patch set

The production New API image must be built from the exact upstream commit recorded by the release manifest. Local server checkouts are not release inputs.

## Grok video adapter

- Upstream repository: `https://github.com/QuantumNous/new-api.git`
- Upstream commit: `bc14c18f6024e79cba1c08d02cd007796e12d668`
- Patch: `patches/bc14c18-grok2api-v3-video.patch`
- Upstream target: grok2api v3 `/v1/videos/generations` and `/v1/videos/{requestId}`

Apply and verify in a clean detached checkout:

```powershell
$ErrorActionPreference = 'Stop'
git clone --filter=blob:none --no-checkout https://github.com/QuantumNous/new-api.git <temporary-path>
git -C <temporary-path> checkout --detach bc14c18f6024e79cba1c08d02cd007796e12d668
git -C <temporary-path> apply --check <cineweave-root>/deploy/new-api/patches/bc14c18-grok2api-v3-video.patch
git -C <temporary-path> apply <cineweave-root>/deploy/new-api/patches/bc14c18-grok2api-v3-video.patch
go test ./relay/channel/task/sora -count=1
```

Build the immutable New API image from that patched clean checkout. Bind the upstream commit, patch SHA-256, image digest, and runtime labels in the Combined Release evidence. Never build from `/soft/new-api` when that operational checkout is dirty.
