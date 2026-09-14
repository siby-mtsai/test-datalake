# Verifies per-consumer IAM isolation for the mtsai-datalake Athena workgroups/roles.
#
# Does NOT just switch Athena workgroups (that doesn't test anything - it doesn't change which
# IAM identity is authorizing the query). Actually assumes each consumer role via STS and runs a
# real Athena query as that role, matching what should and shouldn't be allowed.
#
# Usage:
#   .\runbooks\isolation-check.ps1                      # test environment, default account
#   .\runbooks\isolation-check.ps1 -Environment test -AccountId 690293068614

param(
    [string]$Environment = "test",
    [string]$AccountId = "690293068614",
    [string]$Region = "ap-south-1",
    [string]$RawDatabase = "test_raw",
    [string]$RawTable = "test_table",
    [string]$CuratedDatabase = "test_curated",
    [string]$CuratedTable = "test_table_curated"
)

# key: role name -> whether it SHOULD be able to read the raw zone
$Consumers = @{
    "analytics"   = $false
    "forecasting" = $false
    "audit"       = $true
}

function Invoke-AthenaQueryAs {
    param($RoleArn, $Database, $Table, $WorkGroup)

    $creds = aws sts assume-role --role-arn $RoleArn --role-session-name "isolation-check" `
        --region $Region --output json | ConvertFrom-Json

    $env:AWS_ACCESS_KEY_ID = $creds.Credentials.AccessKeyId
    $env:AWS_SECRET_ACCESS_KEY = $creds.Credentials.SecretAccessKey
    $env:AWS_SESSION_TOKEN = $creds.Credentials.SessionToken

    $qid = aws athena start-query-execution `
        --query-string "SELECT * FROM $Table LIMIT 1" `
        --query-execution-context Database=$Database `
        --work-group $WorkGroup `
        --region $Region --query "QueryExecutionId" --output text 2>&1

    Start-Sleep -Seconds 6

    $status = aws athena get-query-execution --query-execution-id $qid --region $Region `
        --query "QueryExecution.Status.State" --output text 2>&1

    Remove-Item Env:\AWS_ACCESS_KEY_ID, Env:\AWS_SECRET_ACCESS_KEY, Env:\AWS_SESSION_TOKEN -ErrorAction SilentlyContinue

    return $status
}

$failures = 0

foreach ($name in $Consumers.Keys) {
    $roleArn = "arn:aws:iam::${AccountId}:role/mtsai-datalake-${Environment}-${name}"
    $workgroup = "mtsai-datalake-${Environment}-${name}"
    $shouldHaveRaw = $Consumers[$name]

    Write-Host "=== $name ($roleArn) ===" -ForegroundColor Cyan

    $curatedStatus = Invoke-AthenaQueryAs -RoleArn $roleArn -Database $CuratedDatabase -Table $CuratedTable -WorkGroup $workgroup
    $curatedOk = $curatedStatus -eq "SUCCEEDED"
    Write-Host "  curated: $curatedStatus $(if ($curatedOk) {'[OK]'} else {'[FAIL - expected SUCCEEDED]'})" -ForegroundColor $(if ($curatedOk) {"Green"} else {"Red"})
    if (-not $curatedOk) { $failures++ }

    $rawStatus = Invoke-AthenaQueryAs -RoleArn $roleArn -Database $RawDatabase -Table $RawTable -WorkGroup $workgroup
    $rawExpected = if ($shouldHaveRaw) { "SUCCEEDED" } else { "FAILED" }
    $rawOk = $rawStatus -eq $rawExpected
    Write-Host "  raw:      $rawStatus $(if ($rawOk) {'[OK]'} else {"[FAIL - expected $rawExpected]"})" -ForegroundColor $(if ($rawOk) {"Green"} else {"Red"})
    if (-not $rawOk) { $failures++ }
}

if ($failures -eq 0) {
    Write-Host "`nAll isolation checks passed." -ForegroundColor Green
} else {
    Write-Host "`n$failures check(s) failed - investigate before trusting isolation." -ForegroundColor Red
    exit 1
}
