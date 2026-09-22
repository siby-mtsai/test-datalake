# Bundles the manual verification commands scattered across docs/STATUS.md, runbooks/*.md, and
# this session's chat history into one script - a status check across Phases 1-3, read-only by
# default. Nothing destructive runs unless you explicitly opt in via a switch/parameter.
#
# Usage:
#   .\runbooks\verify-all.ps1                              # read-only status check, all phases
#   .\runbooks\verify-all.ps1 -RunCompact                  # also triggers a real cmd/compact run
#   .\runbooks\verify-all.ps1 -RunTrim                     # also triggers a real cmd/trim run
#   .\runbooks\verify-all.ps1 -SendTestAlert               # also publishes a real test SNS message
#   .\runbooks\verify-all.ps1 -EraseIdentifier hash_veh_000067   # DESTRUCTIVE - actually erases that identifier
#
# Any combination of the four action switches/parameters can be passed together.

param(
    [string]$Environment = "test",
    [string]$AccountId = "690293068614",
    [string]$Region = "ap-south-1",
    [switch]$RunCompact,
    [switch]$RunTrim,
    [switch]$SendTestAlert,
    [string]$EraseIdentifier,
    [string]$EraseRequestRef = "manual-verify-$(Get-Date -Format 'yyyyMMdd-HHmmss')"
)

$Cluster = "arn:aws:ecs:${Region}:${AccountId}:cluster/mtsai-datalake-${Environment}"
$Subnets = @("subnet-0266f942977861ddd", "subnet-08064e40f903eceab", "subnet-05639de4c9e0732b7")
$SecurityGroup = "sg-03adcaeb0b6470eb3"
$Bucket = "mtsai-datalake-${Environment}-${AccountId}-${Region}"
$TopicArn = "arn:aws:sns:${Region}:${AccountId}:mtsai-datalake-${Environment}-export-alarms"

function Section($title) {
    Write-Host ""
    Write-Host "=== $title ===" -ForegroundColor Cyan
}

function New-NetworkConfig {
    $subnetList = ($Subnets | ForEach-Object { "`"$_`"" }) -join ","
    return "{`"awsvpcConfiguration`":{`"subnets`":[$subnetList],`"securityGroups`":[`"$SecurityGroup`"],`"assignPublicIp`":`"ENABLED`"}}"
}

# --- Phase 1: consumer isolation ---
Section "Phase 1 - Consumer isolation"
& "$PSScriptRoot\isolation-check.ps1" -Environment $Environment -AccountId $AccountId -Region $Region

# --- Phase 2: nightly schedules ---
Section "Phase 2 - Nightly schedules"
foreach ($name in @("mtsai-datalake-${Environment}-nightly-export", "mtsai-datalake-${Environment}-nightly-curate")) {
    $sched = aws scheduler get-schedule --name $name --group-name default --region $Region | ConvertFrom-Json
    if ($sched) {
        Write-Host "$name : $($sched.State), $($sched.ScheduleExpression) $($sched.ScheduleExpressionTimezone)"
    } else {
        Write-Host "$name : NOT FOUND" -ForegroundColor Red
    }
}

# --- Phase 2/3: recent task runs ---
# ECS only retains stopped-task records for about an hour, so aws ecs list-tasks goes empty fast -
# CloudWatch Logs (30-day retention on this log group) is the actual persistent record. One log
# stream per run, named "<job>/<container>/<task-id>". The AWS API won't let --order-by
# LastEventTime combine with --log-stream-name-prefix (confirmed empirically), so order first,
# filter by prefix client-side after.
Section "Phase 2/3 - Recent runs (from CloudWatch Logs, last 5 per job)"
$allStreams = aws logs describe-log-streams --region $Region --log-group-name "/mtsai-datalake/${Environment}/export" `
    --order-by LastEventTime --descending --max-items 50 `
    --query "logStreams[*].{name:logStreamName,lastEvent:lastEventTimestamp}" --output json | ConvertFrom-Json
foreach ($job in @("export", "curate", "compact", "erasure", "trim")) {
    $matches = $allStreams | Where-Object { $_.name -like "$job/*" } | Select-Object -First 5
    if ($matches) {
        Write-Host "$job :"
        foreach ($s in $matches) {
            $when = [DateTimeOffset]::FromUnixTimeMilliseconds($s.lastEvent).LocalDateTime
            Write-Host "  $when  $($s.name)"
        }
    } else {
        Write-Host "$job : no runs found in the log group" -ForegroundColor DarkYellow
    }
}

# --- Phase 2: manifest evidence trail ---
Section "Phase 2 - Manifest evidence trail (S3)"
$manifestCount = (aws s3 ls "s3://$Bucket/export-manifests/" --recursive --region $Region | Measure-Object -Line).Lines
Write-Host "export-manifests/ object count: $manifestCount"

# --- Phase 3: weekly compact schedule ---
Section "Phase 3 - Weekly compact schedule"
$compactSched = aws scheduler get-schedule --name "mtsai-datalake-${Environment}-weekly-compact" --group-name default --region $Region | ConvertFrom-Json
if ($compactSched) {
    Write-Host "weekly-compact : $($compactSched.State), $($compactSched.ScheduleExpression) $($compactSched.ScheduleExpressionTimezone)"
} else {
    Write-Host "weekly-compact : NOT FOUND" -ForegroundColor Red
}

# --- Phase 3: budgets ---
Section "Phase 3 - Budget alarms"
$budgets = aws budgets describe-budgets --account-id $AccountId --region us-east-1 --query "Budgets[?starts_with(BudgetName, 'mtsai-datalake-${Environment}')].BudgetName" --output json | ConvertFrom-Json
if ($budgets) {
    Write-Host "Budgets found: $($budgets -join ', ')" -ForegroundColor Green
} else {
    Write-Host "No budgets found - check alarm_email is set" -ForegroundColor Red
}

# --- Phase 3: SNS subscriptions ---
Section "Phase 3 - SNS failure-alert subscriptions"
$subs = aws sns list-subscriptions-by-topic --topic-arn $TopicArn --region $Region --output json | ConvertFrom-Json
foreach ($s in $subs.Subscriptions) {
    $confirmed = $s.SubscriptionArn -ne "PendingConfirmation"
    $color = if ($confirmed) { "Green" } else { "DarkYellow" }
    $status = if ($confirmed) { "CONFIRMED" } else { "PENDING" }
    Write-Host "$($s.Protocol) -> $($s.Endpoint) : $status" -ForegroundColor $color
}

# --- Phase 3: cost dashboard + storage metrics ---
Section "Phase 3 - Cost dashboard"
Write-Host "https://$Region.console.aws.amazon.com/cloudwatch/home?region=$Region#dashboards:name=mtsai-datalake-${Environment}-cost"

Section "Phase 3 - Published storage-by-prefix metrics"
$metrics = aws cloudwatch list-metrics --namespace "MTSAiDataLake/Storage" --region $Region --query "Metrics[*].Dimensions[0].Value" --output json | ConvertFrom-Json
if ($metrics) {
    Write-Host "Prefixes with data: $($metrics -join ', ')"
} else {
    Write-Host "No metrics published yet - run with -RunCompact to publish some" -ForegroundColor DarkYellow
}

# --- Phase 4: weekly trim schedule + evidence trail ---
Section "Phase 4 - Weekly Postgres trim schedule"
$trimSched = aws scheduler get-schedule --name "mtsai-datalake-${Environment}-weekly-trim" --group-name default --region $Region | ConvertFrom-Json
if ($trimSched) {
    Write-Host "weekly-trim : $($trimSched.State), $($trimSched.ScheduleExpression) $($trimSched.ScheduleExpressionTimezone)"
} else {
    Write-Host "weekly-trim : NOT FOUND" -ForegroundColor Red
}

Section "Phase 4 - Trim manifest evidence trail (S3)"
$trimManifestCount = (aws s3 ls "s3://$Bucket/export-manifests/trim/" --recursive --region $Region | Measure-Object -Line).Lines
Write-Host "export-manifests/trim/ object count: $trimManifestCount"

# --- Optional live actions (only run if explicitly requested) ---
if ($RunCompact) {
    Section "ACTION: Triggering cmd/compact"
    $result = aws ecs run-task --region $Region --cluster $Cluster --task-definition "mtsai-datalake-${Environment}-compact" `
        --launch-type FARGATE --network-configuration (New-NetworkConfig) --output json | ConvertFrom-Json
    Write-Host "Launched: $($result.tasks[0].taskArn)"
    Write-Host "Poll with: aws ecs describe-tasks --region $Region --cluster $Cluster --tasks $($result.tasks[0].taskArn)"
}

if ($RunTrim) {
    Section "ACTION: Triggering cmd/trim"
    $result = aws ecs run-task --region $Region --cluster $Cluster --task-definition "mtsai-datalake-${Environment}-trim" `
        --launch-type FARGATE --network-configuration (New-NetworkConfig) --output json | ConvertFrom-Json
    Write-Host "Launched: $($result.tasks[0].taskArn)"
    Write-Host "Poll with: aws ecs describe-tasks --region $Region --cluster $Cluster --tasks $($result.tasks[0].taskArn)"
}

if ($SendTestAlert) {
    Section "ACTION: Publishing a real test SNS alert"
    aws sns publish --region $Region --topic-arn $TopicArn `
        --subject "Manual verification test" `
        --message "Test alert sent via runbooks/verify-all.ps1 at $(Get-Date -Format 'u')"
    Write-Host "Sent - check confirmed subscriber inboxes/phones."
}

if ($EraseIdentifier) {
    Section "ACTION: Erasing identifier '$EraseIdentifier' (DESTRUCTIVE - real DELETE against real data)"
    Write-Host "Request ref: $EraseRequestRef" -ForegroundColor Yellow
    $overrides = "{`"containerOverrides`":[{`"name`":`"erasure`",`"environment`":[" +
        "{`"name`":`"ERASURE_REQUEST_REF`",`"value`":`"$EraseRequestRef`"}," +
        "{`"name`":`"ERASURE_IDENTIFIER`",`"value`":`"$EraseIdentifier`"}," +
        "{`"name`":`"ERASURE_JURISDICTION`",`"value`":`"IN`"}]}]}"
    $result = aws ecs run-task --region $Region --cluster $Cluster --task-definition "mtsai-datalake-${Environment}-erasure" `
        --launch-type FARGATE --network-configuration (New-NetworkConfig) --overrides $overrides --output json | ConvertFrom-Json
    Write-Host "Launched: $($result.tasks[0].taskArn)"
    Write-Host "Poll with: aws ecs describe-tasks --region $Region --cluster $Cluster --tasks $($result.tasks[0].taskArn)"
    Write-Host "Manifest will land at: s3://$Bucket/export-manifests/erasure/$(Get-Date -Format 'yyyy-MM-dd')/$EraseRequestRef.json"
}

Write-Host ""
Write-Host "Done. Pass -RunCompact, -RunTrim, -SendTestAlert, or -EraseIdentifier <hash> to also trigger real actions (erasure is destructive)." -ForegroundColor DarkGray
