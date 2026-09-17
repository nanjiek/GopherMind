param(
  [string]$DenseUrl = "http://127.0.0.1:8011",
  [string]$HybridUrl = "http://127.0.0.1:8012",
  [int]$TopK = 5,
  [int]$DistractorsPerTarget = 12,
  [int]$GlobalNoiseCount = 200,
  [string]$OutputDir = ".\tmp\hybrid_eval",
  [string]$ExistingUserId = "",
  [switch]$SkipIngest
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Write-Step {
  param([string]$Msg)
  Write-Host ("`n==> " + $Msg)
}

function Get-ErrorBody {
  param($Exception)
  try {
    if ($Exception.Response) {
      $sr = New-Object IO.StreamReader($Exception.Response.GetResponseStream())
      return $sr.ReadToEnd()
    }
  } catch {
    return ""
  }
  return ""
}

function Invoke-JsonUtf8 {
  param(
    [string]$Uri,
    [string]$Method = "Post",
    [object]$Data,
    [int]$Retries = 2
  )

  $json = $Data | ConvertTo-Json -Depth 20
  $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)

  for ($i = 0; $i -le $Retries; $i++) {
    try {
      return Invoke-RestMethod -Uri $Uri -Method $Method -ContentType "application/json; charset=utf-8" -Body $bytes
    } catch {
      $body = Get-ErrorBody $_.Exception
      if ($i -eq $Retries) {
        throw "HTTP error at $Uri`n$body"
      }
      Start-Sleep -Milliseconds 300
    }
  }
}

function Assert-Health {
  param([string]$BaseUrl)
  try {
    $r = Invoke-RestMethod -Uri "$BaseUrl/healthz" -Method Get -TimeoutSec 5
    if ($r.status -ne "ok") {
      throw "healthz status is not ok"
    }
  } catch {
    throw "Health check failed for ${BaseUrl}: $($_.Exception.Message)"
  }
}

function New-RunId {
  return [guid]::NewGuid().ToString("N").Substring(0, 10)
}

function New-Anchor {
  param([string]$Prefix, [int]$N)
  return ("{0}-{1:0000}" -f $Prefix, $N)
}

Write-Step "Checking endpoints"
Assert-Health -BaseUrl $DenseUrl
Assert-Health -BaseUrl $HybridUrl

$runId = New-RunId
$userId = "u-hybrid-$runId"
if ($SkipIngest) {
  if ([string]::IsNullOrWhiteSpace($ExistingUserId)) {
    throw "-SkipIngest requires -ExistingUserId (the user_id from a previous ingest run)."
  }
  $userId = $ExistingUserId
}

Write-Step "Preparing difficult dataset (run_id=$runId user_id=$userId)"

$targets = @(
  @{
    key = "redis_cluster_disabled"
    anchor = (New-Anchor -Prefix "RDX" -N 1001)
    text = "Runbook for Redis incident code {ANCHOR}. Symptom: ERR This instance has cluster support disabled. Steps: verify redis mode, client config, and fallback policy."
    queries = @(
      "How to fix incident code {ANCHOR}?",
      "ERR This instance has cluster support disabled root cause",
      "Redis cluster disabled playbook {ANCHOR}"
    )
  },
  @{
    key = "kimi_k25_temperature"
    anchor = (New-Anchor -Prefix "KMI" -N 2002)
    text = "Model policy {ANCHOR}: kimi-k2.5 request must not set temperature. Use thinking disabled for low-latency sync calls."
    queries = @(
      "What is policy {ANCHOR} for kimi-k2.5?",
      "kimi-k2.5 cannot set temperature why",
      "thinking disabled setup {ANCHOR}"
    )
  },
  @{
    key = "qdrant_vector_size"
    anchor = (New-Anchor -Prefix "QDV" -N 3003)
    text = "Vector DB standard {ANCHOR}: QDRANT_VECTOR_SIZE is 1024 when embedding model is bge-m3."
    queries = @(
      "Find standard {ANCHOR}",
      "QDRANT_VECTOR_SIZE for bge-m3",
      "vector size policy code {ANCHOR}"
    )
  },
  @{
    key = "jwt_ttl"
    anchor = (New-Anchor -Prefix "JWT" -N 4004)
    text = "Auth baseline {ANCHOR}: Access token short TTL, Refresh token long TTL, and rotate refresh tokens."
    queries = @(
      "Auth baseline {ANCHOR}",
      "access and refresh ttl best practice",
      "refresh token rotation policy {ANCHOR}"
    )
  },
  @{
    key = "rabbitmq_dlq_idempotency"
    anchor = (New-Anchor -Prefix "MQD" -N 5005)
    text = "Queue reliability guideline {ANCHOR}: retry queues plus DLQ and idempotent sink write for consistency."
    queries = @(
      "Queue reliability guideline {ANCHOR}",
      "DLQ retry idempotent sink design",
      "rabbitmq retry dlq policy {ANCHOR}"
    )
  },
  @{
    key = "rag_pipeline"
    anchor = (New-Anchor -Prefix "RAG" -N 6006)
    text = "RAG pipeline card {ANCHOR}: Embedding -> Retrieve -> Rerank -> Generate."
    queries = @(
      "What is RAG card {ANCHOR}",
      "standard rag pipeline steps",
      "embedding retrieve rerank generate {ANCHOR}"
    )
  }
)

$allDocs = @()
$cases = @()

foreach ($t in $targets) {
  $targetDocId = "target-$($t.key)"
  $targetText = $t.text.Replace("{ANCHOR}", $t.anchor)
  $allDocs += [pscustomobject]@{
    id = $targetDocId
    kind = "target"
    text = $targetText
  }

  $qIndex = 0
  foreach ($q in $t.queries) {
    $qIndex++
    $cases += [pscustomobject]@{
      case_id = "$($t.key)-q$qIndex"
      query = $q.Replace("{ANCHOR}", $t.anchor)
      expect = $targetDocId
      topic = $t.key
    }
  }

  for ($i = 1; $i -le $DistractorsPerTarget; $i++) {
    $wrongAnchor = New-Anchor -Prefix "NOISE" -N ($i + (Get-Random -Minimum 100 -Maximum 900))
    $noiseText = $targetText.Replace($t.anchor, $wrongAnchor)
    $noiseText += " This is a nearby but incorrect variant for retrieval stress test."
    $allDocs += [pscustomobject]@{
      id = "noise-$($t.key)-$i"
      kind = "near_noise"
      text = $noiseText
    }
  }
}

for ($i = 1; $i -le $GlobalNoiseCount; $i++) {
  $allDocs += [pscustomobject]@{
    id = "global-noise-$i"
    kind = "global_noise"
    text = "General platform note #$i about redis mysql rabbitmq qdrant jwt tracing metrics and deployment checklists."
  }
}

Write-Host ("Dataset docs: " + $allDocs.Count + ", cases: " + $cases.Count)

if (-not $SkipIngest) {
  Write-Step "Ingesting dataset into shared Qdrant (via dense endpoint)"
  $n = 0
  foreach ($d in $allDocs) {
    $n++
    Invoke-JsonUtf8 -Uri "$DenseUrl/ingest" -Data @{
      user_id = $userId
      document_id = $d.id
      text = $d.text
      metadata = @{
        source = "hybrid_eval"
        run_id = $runId
        kind = $d.kind
      }
    } | Out-Null
    if ($n % 50 -eq 0) {
      Write-Host ("  ingested: " + $n + "/" + $allDocs.Count)
    }
  }
}

function Eval-Endpoint {
  param(
    [string]$BaseUrl,
    [string]$Label,
    [array]$InputCases,
    [string]$EvalUserId,
    [int]$EvalTopK
  )
  $rows = @()
  foreach ($c in $InputCases) {
    $r = Invoke-JsonUtf8 -Uri "$BaseUrl/retrieve" -Data @{
      user_id = $EvalUserId
      query = $c.query
      top_k = $EvalTopK
    }
    $ids = @($r.documents | ForEach-Object { $_.doc_id })
    $idx = [array]::IndexOf($ids, $c.expect)
    $hit = $idx -ge 0
    $rank = if ($hit) { $idx + 1 } else { 0 }
    $rr = if ($rank -gt 0) { [double](1.0 / $rank) } else { 0.0 }
    $rows += [pscustomobject]@{
      mode = $Label
      case_id = $c.case_id
      topic = $c.topic
      query = $c.query
      expect = $c.expect
      hit = $hit
      rank = $rank
      rr = $rr
      top1 = $(if ($ids.Count -gt 0) { $ids[0] } else { "" })
    }
  }
  return $rows
}

Write-Step "Evaluating dense"
$denseRows = Eval-Endpoint -BaseUrl $DenseUrl -Label "dense" -InputCases $cases -EvalUserId $userId -EvalTopK $TopK

Write-Step "Evaluating hybrid"
$hybridRows = Eval-Endpoint -BaseUrl $HybridUrl -Label "hybrid" -InputCases $cases -EvalUserId $userId -EvalTopK $TopK

$all = @($denseRows + $hybridRows)

$summary = $all | Group-Object mode | ForEach-Object {
  $groupItems = @($_.Group)
  $total = $groupItems.Count
  $hitAt1 = @($groupItems | Where-Object { $_.rank -eq 1 }).Count
  $hitAtK = @($groupItems | Where-Object { $_.hit }).Count

  $rrSum = 0.0
  $rrCount = 0
  foreach ($g in $groupItems) {
    $rrSum += [double]$g.rr
    $rrCount++
  }
  $mrr = if ($rrCount -gt 0) { $rrSum / $rrCount } else { 0.0 }

  $hitItems = @($groupItems | Where-Object hit)
  $rankSum = 0.0
  $rankCount = 0
  foreach ($h in $hitItems) {
    $rankSum += [double]$h.rank
    $rankCount++
  }
  $avgRank = if ($rankCount -gt 0) { $rankSum / $rankCount } else { 0.0 }

  [pscustomobject]@{
    mode = $_.Name
    total = $total
    hit_at_1 = $hitAt1
    hit_at_k = $hitAtK
    hit_rate_at_k = [math]::Round($hitAtK * 1.0 / [math]::Max(1, $total), 4)
    mrr = [math]::Round([double]$mrr, 4)
    avg_rank_of_hits = [math]::Round([double]$(if ($avgRank) { $avgRank } else { 0 }), 4)
  }
}

$denseMap = @{}
foreach ($r in $denseRows) { $denseMap[$r.case_id] = $r }
$hybridMap = @{}
foreach ($r in $hybridRows) { $hybridMap[$r.case_id] = $r }

$diffRows = @()
foreach ($k in $denseMap.Keys) {
  $d = $denseMap[$k]
  $h = $hybridMap[$k]
  $diffRows += [pscustomobject]@{
    case_id = $k
    topic = $d.topic
    query = $d.query
    dense_rank = $d.rank
    hybrid_rank = $h.rank
    dense_hit = $d.hit
    hybrid_hit = $h.hit
    improved = ($h.rank -gt 0 -and ($d.rank -eq 0 -or $h.rank -lt $d.rank))
    regressed = ($d.rank -gt 0 -and ($h.rank -eq 0 -or $h.rank -gt $d.rank))
  }
}

$improvedCount = @($diffRows | Where-Object improved).Count
$regressedCount = @($diffRows | Where-Object regressed).Count

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
$summary | Export-Csv -NoTypeInformation -Encoding UTF8 -Path (Join-Path $OutputDir "summary.csv")
$all | Export-Csv -NoTypeInformation -Encoding UTF8 -Path (Join-Path $OutputDir "details.csv")
$diffRows | Export-Csv -NoTypeInformation -Encoding UTF8 -Path (Join-Path $OutputDir "diff.csv")

Write-Step "Summary"
$summary | Format-Table -AutoSize

Write-Step "Delta"
Write-Host ("improved_cases=" + $improvedCount + ", regressed_cases=" + $regressedCount)

Write-Step "Top changed cases"
$diffRows |
  Where-Object { $_.improved -or $_.regressed } |
  Sort-Object topic, case_id |
  Select-Object case_id, topic, dense_rank, hybrid_rank, improved, regressed |
  Format-Table -AutoSize

Write-Step "Artifacts"
Write-Host ("summary: " + (Join-Path $OutputDir "summary.csv"))
Write-Host ("details: " + (Join-Path $OutputDir "details.csv"))
Write-Host ("diff   : " + (Join-Path $OutputDir "diff.csv"))
