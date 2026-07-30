param(
  [switch]$IncludeWorkingTree,
  [switch]$SkipWebBuild,
  [switch]$SkipImageBuild,
  [switch]$KeepArtifacts,
  [switch]$RequireClean,
  [string]$EvidencePath = ''
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$policyPath = Join-Path $repoRoot 'packages/edition/ce-release-policy.v1.json'
$policy = Get-Content -LiteralPath $policyPath -Encoding UTF8 -Raw | ConvertFrom-Json
$auditScript = Join-Path $repoRoot 'scripts/ce_release_audit.py'
$sourceLicensingAuditScript = Join-Path $repoRoot 'scripts/audit-source-licensing.py'
$tempBase = (Resolve-Path -LiteralPath ([System.IO.Path]::GetTempPath())).Path
$tempRoot = Join-Path $tempBase "cineweave-ce-release-$([Guid]::NewGuid().ToString('N'))"
$sourceRoot = Join-Path $tempRoot 'source'
$artifactRoot = Join-Path $tempRoot 'artifacts'
$archivePath = Join-Path $tempRoot 'cineweave-ce-source.zip'
$alternateIndex = Join-Path $tempRoot 'git-index'
$builtImageTags = [System.Collections.Generic.List[string]]::new()

function Invoke-Step {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Name,
    [Parameter(Mandatory = $true)]
    [scriptblock]$Action
  )
  Write-Host "`n==> $Name"
  & $Action
}

function Get-MainBinaryName {
  param(
    [Parameter(Mandatory = $true)]
    [string]$ImportPath
  )
  $relative = $ImportPath -replace '^github\.com/Einzieg/cineweave/', ''
  $name = $relative -replace '[/\\:]', '_'
  if ($IsWindows) {
    return "$name.exe"
  }
  return $name
}

function Assert-CommunityBinaryRejectsCommercialEdition {
  param(
    [Parameter(Mandatory = $true)]
    [string]$BinaryPath
  )
  $previousEdition = $env:CINEWEAVE_EDITION
  $previousNativePreference = $PSNativeCommandUseErrorActionPreference
  try {
    foreach ($requestedEdition in @('cloud', 'enterprise')) {
      $env:CINEWEAVE_EDITION = $requestedEdition
      $PSNativeCommandUseErrorActionPreference = $false
      $output = @(& $BinaryPath 2>&1)
      $exitCode = $LASTEXITCODE
      if ($exitCode -eq 0) {
        throw "CE binary unexpectedly started with CINEWEAVE_EDITION=$requestedEdition`: $BinaryPath"
      }
      $message = $output -join "`n"
      if ($message -notmatch 'feature_not_compiled') {
        throw "CE binary did not fail at the Edition boundary for $requestedEdition`: $BinaryPath`n$message"
      }
    }
  } finally {
    $env:CINEWEAVE_EDITION = $previousEdition
    $PSNativeCommandUseErrorActionPreference = $previousNativePreference
  }
}

function Assert-ImageEdition {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Tag
  )
  $editionLabel = (& docker image inspect --format '{{ index .Config.Labels "org.cineweave.edition" }}' $Tag).Trim()
  if ($editionLabel -ne 'community') {
    throw "Image $Tag has Edition label '$editionLabel', expected 'community'."
  }
}

function Assert-CeImageMatrix {
  param(
    [Parameter(Mandatory = $true)]
    [string]$SourceDirectory,
    [Parameter(Mandatory = $true)]
    [object]$ReleasePolicy
  )
  $composePath = Join-Path $SourceDirectory 'compose.yml'
  $composeJson = docker compose -f $composePath --profile app --profile ops config --format json
  $compose = $composeJson | ConvertFrom-Json
  $actual = @{}
  foreach ($property in $compose.services.PSObject.Properties) {
    if ($null -ne $property.Value.build) {
      $actual[$property.Name] = $property.Value
    }
  }

  $declared = @{}
  foreach ($image in $ReleasePolicy.images) {
    $services = @($image.composeServices)
    if ($services.Count -eq 0) {
      throw "CE image '$($image.name)' does not declare composeServices."
    }
    foreach ($service in $services) {
      if ($declared.ContainsKey([string]$service)) {
        throw "Compose service '$service' is covered by more than one CE image policy entry."
      }
      $declared[[string]$service] = [string]$image.name
      if (-not $actual.ContainsKey([string]$service)) {
        throw "CE image policy references unknown or non-build Compose service '$service'."
      }
      $actualDockerfile = ([string]$actual[[string]$service].build.dockerfile).Replace('\', '/')
      $expectedDockerfile = ([string]$image.dockerfile).Replace('\', '/')
      if ($actualDockerfile -ne $expectedDockerfile) {
        throw "Compose service '$service' uses '$actualDockerfile', policy expects '$expectedDockerfile'."
      }
      $expectedServicePath = $image.buildArgs.SERVICE_PATH
      if ($null -ne $expectedServicePath) {
        $actualServicePath = $actual[[string]$service].build.args.SERVICE_PATH
        if ([string]$actualServicePath -ne [string]$expectedServicePath) {
          throw "Compose service '$service' SERVICE_PATH does not match the CE image policy."
        }
      }
    }
  }

  $uncovered = @($actual.Keys | Where-Object { -not $declared.ContainsKey($_) } | Sort-Object)
  if ($uncovered.Count -gt 0) {
    throw "Compose build services missing from the CE image policy: $($uncovered -join ', ')"
  }
}

function New-DockerSbom {
  param(
    [Parameter(Mandatory = $true)]
    [string]$Tag,
    [Parameter(Mandatory = $true)]
    [string]$OutputPath
  )
  $previousApiVersion = $env:DOCKER_API_VERSION
  try {
    # docker-sbom 0.6 defaults to API 1.41, while current Docker Desktop
    # rejects clients older than 1.44. The plugin supports the newer API
    # when it is selected explicitly.
    $env:DOCKER_API_VERSION = '1.44'
    docker sbom $Tag --layers all --format spdx-json --output $OutputPath --quiet
  } finally {
    $env:DOCKER_API_VERSION = $previousApiVersion
  }
}

function Remove-AuditImages {
  $previousNativePreference = $PSNativeCommandUseErrorActionPreference
  $PSNativeCommandUseErrorActionPreference = $false
  try {
    foreach ($tag in $builtImageTags) {
      if (-not $tag.StartsWith('cineweave-ce-audit-', [System.StringComparison]::Ordinal)) {
        throw "Refusing to remove non-audit image tag: $tag"
      }
      & docker image rm --force $tag 2>$null | Out-Null
    }
  } finally {
    $PSNativeCommandUseErrorActionPreference = $previousNativePreference
  }
}

function Remove-AuditTempRoot {
  if (-not (Test-Path -LiteralPath $tempRoot)) {
    return
  }
  $resolved = (Resolve-Path -LiteralPath $tempRoot).Path
  $prefix = $tempBase.TrimEnd('\', '/') + [System.IO.Path]::DirectorySeparatorChar
  if (-not $resolved.StartsWith($prefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    throw "Refusing to remove audit path outside the system temp directory: $resolved"
  }
  Remove-Item -LiteralPath $resolved -Recurse -Force
}

Push-Location $repoRoot
try {
  if ($RequireClean -and (git status --porcelain)) {
    throw 'CE release audit requires a clean worktree.'
  }

  New-Item -ItemType Directory -Path $sourceRoot -Force | Out-Null
  New-Item -ItemType Directory -Path $artifactRoot -Force | Out-Null

  $sourceLicensingReport = Join-Path $artifactRoot 'source-licensing-audit.json'
  Invoke-Step 'Source and third-party dependency inventory' {
    python $sourceLicensingAuditScript `
      --output $sourceLicensingReport `
      --require-ready
  }

  $coreCommit = (& git rev-parse HEAD).Trim()
  $sourceIdentity = $coreCommit
  $sourceRef = 'HEAD'
  if ($IncludeWorkingTree) {
    $previousIndex = $env:GIT_INDEX_FILE
    try {
      $env:GIT_INDEX_FILE = $alternateIndex
      git read-tree HEAD
      git -c core.autocrlf=false add -A
      $sourceRef = (& git write-tree).Trim()
      $sourceIdentity = "working-tree:$sourceRef"
    } finally {
      $env:GIT_INDEX_FILE = $previousIndex
    }
  }

  foreach ($name in @(
    'CINEWEAVE_SIGNING_PRIVATE_KEY',
    'NEW_API_ADMIN_TOKEN',
    'NEW_API_ADMIN_API_KEY',
    'NEW_API_ADMIN_SECRET'
  )) {
    Remove-Item -LiteralPath "Env:$name" -ErrorAction SilentlyContinue
  }
  $env:CINEWEAVE_EDITION = 'community'
  $env:CINEWEAVE_WEB_EDITION = 'community'
  $env:GOFLAGS = ''
  $env:NODE_OPTIONS = ''
  $env:NEXT_PUBLIC_API_BASE_URL = ''
  $env:NEXT_PUBLIC_REALTIME_URL = ''
  $env:S3_PUBLIC_ENDPOINT = 'http://localhost:19290'

  Invoke-Step 'Reachable Git history leak audit' {
    python $auditScript --policy $policyPath history --repo $repoRoot
  }

  Invoke-Step 'Immutable CE source archive' {
    git archive --format=zip --output=$archivePath $sourceRef
    Expand-Archive -LiteralPath $archivePath -DestinationPath $sourceRoot
    python $auditScript --policy $policyPath tree --root $sourceRoot --scope source-archive
  }

  Invoke-Step 'CE Compose image matrix coverage' {
    Assert-CeImageMatrix -SourceDirectory $sourceRoot -ReleasePolicy $policy
  }

  $goBinaryRoot = Join-Path $artifactRoot 'go'
  New-Item -ItemType Directory -Path $goBinaryRoot -Force | Out-Null
  $mainPackages = [System.Collections.Generic.List[string]]::new()
  Invoke-Step 'Independent CE Go build' {
    Push-Location $sourceRoot
    try {
      go test -tags community ./...
      go build -tags community ./...
      $discoveredMainPackages = @(
        & go list -tags community -f '{{if eq .Name "main"}}{{.ImportPath}}{{end}}' ./... |
          Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
      )
      foreach ($package in $discoveredMainPackages) {
        $mainPackages.Add($package)
        $outputPath = Join-Path $goBinaryRoot (Get-MainBinaryName -ImportPath $package)
        go build -trimpath -tags community -o $outputPath $package
      }
    } finally {
      Pop-Location
    }
    python $auditScript --policy $policyPath tree --root $goBinaryRoot --scope go-binaries
  }

  Invoke-Step 'CE runtime cannot be unlocked by environment variable' {
    $apiBinary = Join-Path $goBinaryRoot (Get-MainBinaryName -ImportPath 'github.com/Einzieg/cineweave/apps/api')
    $agentBinary = Join-Path $goBinaryRoot (Get-MainBinaryName -ImportPath 'github.com/Einzieg/cineweave/workers/agent-worker')
    Assert-CommunityBinaryRejectsCommercialEdition -BinaryPath $apiBinary
    Assert-CommunityBinaryRejectsCommercialEdition -BinaryPath $agentBinary
  }

  $auditState = [ordered]@{
    webBuilt = $false
    dockerServerVersion = $null
    dockerSbomVersion = $null
  }
  if (-not $SkipWebBuild) {
    Invoke-Step 'Independent CE Web build and chunk/source-map audit' {
      Push-Location $sourceRoot
      try {
        pnpm install --frozen-lockfile
        pnpm --filter @cineweave/web build
      } finally {
        Pop-Location
      }
      $webArtifactRoot = Join-Path $sourceRoot 'apps/web/.next'
      python $auditScript --policy $policyPath tree --root $webArtifactRoot --scope web-assets
      $auditState.webBuilt = $true
    }
  }

  $imageEvidence = @()
  if (-not $SkipImageBuild) {
    Invoke-Step 'Docker image audit tool preflight' {
      $auditState.dockerServerVersion = (& docker version --format '{{.Server.Version}}').Trim()
      $auditState.dockerSbomVersion = (@(& docker sbom version) -join "`n").Trim()
      Write-Host $auditState.dockerSbomVersion
    }
    $shortIdentity = $sourceRef.Substring(0, [Math]::Min(12, $sourceRef.Length)).ToLowerInvariant()
    $imageArchiveRoot = Join-Path $artifactRoot 'images'
    $sbomRoot = Join-Path $artifactRoot 'sbom'
    New-Item -ItemType Directory -Path $imageArchiveRoot -Force | Out-Null
    New-Item -ItemType Directory -Path $sbomRoot -Force | Out-Null

    foreach ($image in $policy.images) {
      $tag = "cineweave-ce-audit-$($image.name):$shortIdentity"
      $builtImageTags.Add($tag)
      $dockerfile = Join-Path $sourceRoot ([string]$image.dockerfile)
      $arguments = [System.Collections.Generic.List[string]]::new()
      $arguments.Add('build')
      $arguments.Add('--file')
      $arguments.Add($dockerfile)
      $arguments.Add('--tag')
      $arguments.Add($tag)
      $arguments.Add('--build-arg')
      $arguments.Add('CINEWEAVE_GO_BUILD_TAGS=community')
      foreach ($property in $image.buildArgs.PSObject.Properties) {
        $arguments.Add('--build-arg')
        $arguments.Add("$($property.Name)=$($property.Value)")
      }
      $arguments.Add($sourceRoot)

      Invoke-Step "Build CE image $($image.name)" {
        & docker @arguments
        Assert-ImageEdition -Tag $tag
      }

      $safeName = ([string]$image.name) -replace '[^A-Za-z0-9_.-]', '_'
      $imageArchive = Join-Path $imageArchiveRoot "$safeName.tar"
      $sbomPath = Join-Path $sbomRoot "$safeName.spdx.json"
      Invoke-Step "Scan CE image layers and SBOM $($image.name)" {
        docker image save --output $imageArchive $tag
        python $auditScript --policy $policyPath tar --archive $imageArchive --scope image-layer
        New-DockerSbom -Tag $tag -OutputPath $sbomPath
        python $auditScript --policy $policyPath tree --root $sbomRoot --scope sbom
      }
      $imageId = (& docker image inspect --format '{{.Id}}' $tag).Trim()
      $imageEvidence += [ordered]@{
        name = [string]$image.name
        tag = $tag
        imageId = $imageId
        archiveSha256 = (Get-FileHash -LiteralPath $imageArchive -Algorithm SHA256).Hash.ToLowerInvariant()
        sbomSha256 = (Get-FileHash -LiteralPath $sbomPath -Algorithm SHA256).Hash.ToLowerInvariant()
      }
    }
  }

  $evidence = [ordered]@{
    schemaVersion = 1
    edition = 'community'
    completedAtUtc = [DateTimeOffset]::UtcNow.ToString('O')
    coreCommit = $coreCommit
    sourceIdentity = $sourceIdentity
    workingTreeIncluded = [bool]$IncludeWorkingTree
    cleanWorktreeRequired = [bool]$RequireClean
    policySha256 = (Get-FileHash -LiteralPath $policyPath -Algorithm SHA256).Hash.ToLowerInvariant()
    sourceLicensingInventorySha256 = (
      (Get-Content -LiteralPath $sourceLicensingReport -Encoding UTF8 -Raw | ConvertFrom-Json).inventorySha256
    )
    sourceLicensingReportSha256 = (Get-FileHash -LiteralPath $sourceLicensingReport -Algorithm SHA256).Hash.ToLowerInvariant()
    sourceArchiveSha256 = (Get-FileHash -LiteralPath $archivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    checks = [ordered]@{
      sourceLicensing = 'passed'
      reachableGitHistory = 'passed'
      sourceArchive = 'passed'
      composeImageMatrix = 'passed'
      independentGoBuild = 'passed'
      commercialEnvironmentUnlock = 'rejected'
      webAssets = if ($SkipWebBuild) { 'skipped' } else { 'passed' }
      imageLayers = if ($SkipImageBuild) { 'skipped' } else { 'passed' }
      imageSbom = if ($SkipImageBuild) { 'skipped' } else { 'passed' }
    }
    toolVersions = [ordered]@{
      go = (& go version).Trim()
      node = (& node --version).Trim()
      pnpm = (& pnpm --version).Trim()
      python = (& python --version).Trim()
      dockerServer = $auditState.dockerServerVersion
      dockerSbom = $auditState.dockerSbomVersion
    }
    goMainPackages = @($mainPackages)
    webBuildAudited = [bool]$auditState.webBuilt
    imageCount = @($imageEvidence).Count
    images = @($imageEvidence)
    violations = 0
    paidProviderCalls = 0
  }
  $evidenceFile = Join-Path $artifactRoot 'ce-release-audit.json'
  $evidence | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $evidenceFile -Encoding UTF8

  if (-not [string]::IsNullOrWhiteSpace($EvidencePath)) {
    $resolvedEvidencePath = if ([System.IO.Path]::IsPathRooted($EvidencePath)) {
      $EvidencePath
    } else {
      Join-Path $repoRoot $EvidencePath
    }
    $parent = Split-Path -Parent $resolvedEvidencePath
    if (-not [string]::IsNullOrWhiteSpace($parent)) {
      New-Item -ItemType Directory -Path $parent -Force | Out-Null
    }
    Copy-Item -LiteralPath $evidenceFile -Destination $resolvedEvidencePath -Force
  }

  Write-Host "`nCE release audit passed."
  Write-Host "Source identity: $sourceIdentity"
  Write-Host "Evidence: $evidenceFile"
  if ($KeepArtifacts) {
    Write-Host "Artifacts retained: $tempRoot"
  }
} finally {
  Pop-Location
  if (-not $KeepArtifacts) {
    Remove-AuditImages
    Remove-AuditTempRoot
  }
}
