param(
  [switch]$MigrationOnly,
  [switch]$ProviderModelDeleteOnly,
  [switch]$SourceToScriptOnly,
  [switch]$DerivedAssetOnly,
  [switch]$CommerceOnly,
  [switch]$KeepEnvironment
)

$ErrorActionPreference = 'Stop'
$PSNativeCommandUseErrorActionPreference = $true

$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = $utf8NoBom
$OutputEncoding = $utf8NoBom

$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$suffix = [Guid]::NewGuid().ToString('N').Substring(0, 12)
$networkName = "cineweave-runtime-test-$suffix"
$databaseContainer = "cineweave-runtime-postgres-$suffix"
$databaseName = 'cineweave_runtime_test'
$databaseUser = 'cineweave_test'
$databasePassword = 'cineweave_test_password'
$databaseUrl = "postgres://${databaseUser}:${databasePassword}@${databaseContainer}:5432/${databaseName}?sslmode=disable"
$postgresImage = 'postgres:16@sha256:5a65324fe84dc41709ff914e90b07f3e2f577073ed27bf917d4873aca0c9ec51'
$goImage = 'golang:1.26@sha256:079e59808d2d252516e27e3f3a9c003740dee7f75e55aa71528766d52bcfc16a'
$networkCreated = $false
$databaseStarted = $false

function Invoke-GoContainer {
  param(
    [Parameter(Mandatory = $true)]
    [string[]]$Arguments,
    [switch]$Integration
  )

  $dockerArguments = @(
    'run', '--rm',
    '--network', $networkName,
    '-e', "DATABASE_URL=$databaseUrl",
    '-e', 'CINEWEAVE_ENV=test',
    '-e', 'CINEWEAVE_RELEASE_ID=runtime-hardening-test',
    '-v', "${repoRoot}:/workspace",
    '-v', 'cineweave_go_mod_cache:/go/pkg/mod',
    '-v', 'cineweave_go_build_cache:/root/.cache/go-build',
    '-w', '/workspace'
  )
  if ($Integration) {
    $dockerArguments += @(
      '-e', 'CINEWEAVE_INTEGRATION_TEST=1',
      '-e', 'CINEWEAVE_COMMERCE_INTEGRATION_TEST=1'
    )
  }
  $dockerArguments += $goImage
  $dockerArguments += $Arguments
  & docker @dockerArguments
  if ($LASTEXITCODE -ne 0) {
    throw "Go container command failed with exit code $LASTEXITCODE"
  }
}

Push-Location $repoRoot
try {
  & docker network create $networkName | Out-Null
  $networkCreated = $true
  & docker run --detach --name $databaseContainer --network $networkName `
    -e "POSTGRES_DB=$databaseName" `
    -e "POSTGRES_USER=$databaseUser" `
    -e "POSTGRES_PASSWORD=$databasePassword" `
    --health-cmd "pg_isready -U $databaseUser -d $databaseName" `
    --health-interval 1s --health-timeout 3s --health-retries 30 `
    $postgresImage | Out-Null
  $databaseStarted = $true

  $healthy = $false
  for ($attempt = 1; $attempt -le 45; $attempt++) {
    $status = (& docker inspect --format '{{.State.Health.Status}}' $databaseContainer).Trim()
    if ($status -eq 'healthy') {
      $healthy = $true
      break
    }
    if ($status -eq 'unhealthy') {
      throw 'Isolated PostgreSQL container became unhealthy'
    }
    Start-Sleep -Seconds 1
  }
  if (-not $healthy) {
    throw 'Timed out waiting for isolated PostgreSQL container'
  }

  Invoke-GoContainer -Integration -Arguments @(
    'go', 'test', '-count=1', './internal/dbmigrate',
    '-run', '^(TestEmptyDatabaseUpDownUpProducesStableSchema|TestConsolidatedBaselineMatchesMigrationChain)$'
  )
  if ($MigrationOnly) {
    Write-Host 'Migration roundtrip passed in an isolated PostgreSQL container.'
    return
  }

  Invoke-GoContainer -Arguments @('go', 'run', './cmd/cineweave-migrate', 'up')
  Invoke-GoContainer -Arguments @('go', 'run', './cmd/cineweave-seed', 'apply')
  Invoke-GoContainer -Arguments @('go', 'run', './cmd/cineweave-migrate', 'verify')
  Invoke-GoContainer -Arguments @('go', 'run', './cmd/cineweave-seed', 'verify')

  if ($ProviderModelDeleteOnly) {
    Invoke-GoContainer -Integration -Arguments @(
      'go', 'test', '-count=1', './internal/provider',
      '-run', '^(TestDeleteModelHardDeletesAndAllowsRecreate|TestDeleteModelRejectsActiveRuntimeWork)$'
    )
    Write-Host 'Provider model hard-delete migration and integration tests passed.'
    return
  }

  if ($SourceToScriptOnly) {
    $sourceToScriptTests = '^(TestSourceToScriptRetryCreatesNewGenerationForFailedEpisodes|TestPrepareSourceToScriptAppendsSecondChapterToCurrentScriptIntegration|TestFinalizeSourceToScriptKeepsPreviousProjectScriptWhenAllEpisodesFailIntegration|TestSourceToScriptMixedFailureKeepsFallbackEpisodeAndActivatesCompleteVersionIntegration|TestSourceToScriptMissingFailedEpisodeCreatesUnactivatedPartialVersionIntegration|TestSourceToScriptRejectsSourceAndScriptChangesBeforePublicationIntegration|TestSourceToScriptManifestReindexesAfterChapterDeletionWithoutDroppingRetainedEpisodeIntegration)$'
    Invoke-GoContainer -Integration -Arguments @(
      'go', 'test', '-count=1', './internal/api', './internal/commerce', './internal/workflows',
      '-run', $sourceToScriptTests
    )
    Write-Host 'Source-to-script migration and integration tests passed.'
    return
  }

  if ($DerivedAssetOnly) {
    $derivedAssetTests = '^(TestDerivedAssetBatchCommandPersistsFullWorksetAndRetry|TestDerivedAssetBatchAggregateApprovedAndReviewRequiredIsPartialIntegration|TestDerivedAssetExecutionRejectsStaleIdentityBeforeProviderIntegration|TestDerivedAssetLateProviderResultAfterGenerationSwitchIsDiscardedIntegration|TestDerivedAssetExecutionLeaseCASDoesNotStealUnexpiredLeaseIntegration|TestDerivedAssetExecutionFailureSynchronizesImmutableRequestOutcomeIntegration|TestDerivedAssetReconcilerRecoversEveryDurableStageIntegration|TestDerivedAssetCommitIsIdempotentAndLateResultCannotOverwriteRequirementIntegration|TestDerivedAssetBatchTerminalAggregateCountsNotFoundDuplicateAndSkippedIntegration|TestDerivedAssetRetryLineageKeepsOriginalFailureImmutableIntegration)$'
    Invoke-GoContainer -Integration -Arguments @(
      'go', 'test', '-count=1', './internal/api', './internal/workflows',
      '-run', $derivedAssetTests
    )
    Write-Host 'Derived-asset V2 migration and integration tests passed.'
    return
  }

  if ($CommerceOnly) {
    $commerceTests = '^(TestCommerce|TestInvokeAgentCommerce|TestValidateCommerce|TestNormalizeCommerce|TestSelectRetryableCommerce).*'
    Invoke-GoContainer -Integration -Arguments @(
      'go', 'test', '-count=1', './internal/api', './internal/workflows',
      '-run', $commerceTests
    )
    Invoke-GoContainer -Integration -Arguments @(
      'go', 'test', '-count=1', './internal/commerce'
    )
    $commerceProviderTests = '^(TestNormalizeCapabilityInput|TestValidateModelLanguageCapabilities|TestFilterRoutingCandidatesByLanguage|TestVideoVariantMetadataDerivesLanguageProvenance|TestInjectGatewayTextImageReferences|TestGatewayTextRequestSnapshot).*'
    Invoke-GoContainer -Arguments @(
      'go', 'test', '-count=1', './internal/provider',
      '-run', $commerceProviderTests
    )
    Write-Host 'Commerce migration, API, workflow, Gateway contract, and language capability tests passed.'
    return
  }

$integrationTests = '^(TestProjectEventLogSharesOutboxTransactionAndSupportsDurableCatchup|TestProviderRequestIdempotencyIntegration|TestProviderTextStreamV2RetryAndReplayIdentityIntegration|TestProviderVideoCreateIdempotencyIntegration|TestGatewayRoutingIntegration|TestDeleteModelHardDeletesAndAllowsRecreate|TestUpdateModelProfileBinding|TestDeleteModelRejectsActiveRuntimeWork|TestAssetBatchCreateRetryAndRevisionConflict|TestDerivedAssetBatchCommandPersistsFullWorksetAndRetry|TestDerivedAssetBatchAggregateApprovedAndReviewRequiredIsPartialIntegration|TestDerivedAssetExecutionRejectsStaleIdentityBeforeProviderIntegration|TestDerivedAssetLateProviderResultAfterGenerationSwitchIsDiscardedIntegration|TestDerivedAssetExecutionLeaseCASDoesNotStealUnexpiredLeaseIntegration|TestDerivedAssetExecutionFailureSynchronizesImmutableRequestOutcomeIntegration|TestDerivedAssetReconcilerRecoversEveryDurableStageIntegration|TestDerivedAssetCommitIsIdempotentAndLateResultCannotOverwriteRequirementIntegration|TestDerivedAssetBatchTerminalAggregateCountsNotFoundDuplicateAndSkippedIntegration|TestDerivedAssetRetryLineageKeepsOriginalFailureImmutableIntegration|TestSourceToScriptRetryCreatesNewGenerationForFailedEpisodes|TestPrepareSourceToScriptAppendsSecondChapterToCurrentScriptIntegration|TestFinalizeSourceToScriptKeepsPreviousProjectScriptWhenAllEpisodesFailIntegration|TestRuntimeOperationReconciliationIntegration|TestCreateWorkflowRunPersistsIdempotencyWithOutbox|TestWorkflowStartOutboxRecoversExpiredLeaseAndAlreadyStarted|TestRestartedNodeRotatesExecutionTokenAndFencesStaleAttempt|TestCancelledWorkflowRunRejectsLateTerminalTransition|TestCancellationReconcilerTerminalizesStuckProviderTask)$'
  Invoke-GoContainer -Integration -Arguments @(
    'go', 'test', '-count=1',
    './apps/realtime', './internal/provider', './internal/api', './internal/workflows',
    '-run', $integrationTests
  )

  $faultTests = '^(TestAssetBatchHundredItemsContinuesAsNewIsolatesFailuresAndRetriesFailedOnly|TestAssetBatchCancellationDrainsAllStartedChildrenBeforeFinalizing|TestSourceToScriptWorkflowContinueAsNewCarriesCompactStableCheckpoint|TestSeventyMinuteVideoBatchUsesBoundedContinueAsNewCheckpoint|TestProviderTimeoutRemainsRetryable|TestGatewayClientStreamTextV2CollectsAndDeduplicatesDeltas|TestGatewayClientStreamTextV2RejectsFallbackAfterDelta|TestRealtimeReturnsGoneForExpiredCursor|TestDrainProjectEventsPaginatesBeyondTwoHundred)$'
  Invoke-GoContainer -Arguments @(
    'go', 'test', '-race', '-count=1',
    './apps/realtime', './internal/provider', './internal/workflows',
    '-run', $faultTests
  )

  Write-Host 'Runtime hardening migration and fault matrix passed.'
} finally {
  Pop-Location
  if ($KeepEnvironment) {
    if ($databaseStarted) {
      Write-Host "Kept PostgreSQL container: $databaseContainer"
    }
    if ($networkCreated) {
      Write-Host "Kept Docker network: $networkName"
    }
  } else {
    if ($databaseStarted) {
      & docker rm --force $databaseContainer | Out-Null
    }
    if ($networkCreated) {
      & docker network rm $networkName | Out-Null
    }
  }
}
