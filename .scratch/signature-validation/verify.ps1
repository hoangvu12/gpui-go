$ErrorActionPreference = 'Continue'
$env:GOTOOLCHAIN = 'local'
$env:GOWORK = 'off'
$env:CGO_ENABLED = '0'
$env:GOPROXY = 'off'
$env:GOSUMDB = 'off'
$fixtureRoot = $PSScriptRoot
$projectRoot = Split-Path (Split-Path $fixtureRoot -Parent) -Parent
$goExecutable = 'C:\Program Files\Go\bin\go.exe'
$results = [Collections.Generic.List[object]]::new()
$failures = [Collections.Generic.List[string]]::new()
$toolVersion = (& $goExecutable version 2>&1 | Out-String).Trim()
if ($LASTEXITCODE -ne 0 -or $toolVersion -notmatch '^go version go1\.27\.') { throw 'Expected installed local Go 1.27 toolchain' }

function Invoke-RecordedGo([string]$label, [string]$directory, [string[]]$arguments) {
    Push-Location -LiteralPath $directory
    try {
        $output = (& $goExecutable @arguments 2>&1 | ForEach-Object { $_.ToString() }) -join "`n"
        $code = $LASTEXITCODE
        return [ordered]@{ name=$label; command=('go ' + ($arguments -join ' ')); exit_code=$code; output=$output }
    } finally { Pop-Location }
}

foreach ($operation in @('test','vet')) {
    $result = Invoke-RecordedGo ('fixture_' + $operation) $fixtureRoot @($operation,'./...')
    $results.Add($result)
    if ($result.exit_code -ne 0) { $failures.Add($result.name) }
    $result = Invoke-RecordedGo ('root_' + $operation) $projectRoot @($operation,'./...')
    $results.Add($result)
    if ($result.exit_code -ne 0) { $failures.Add($result.name) }
}

$caseDefinitions = Get-Content -LiteralPath (Join-Path $fixtureRoot 'cases.json') -Raw | ConvertFrom-Json
$caseResults = [Collections.Generic.List[object]]::new()
foreach ($case in $caseDefinitions) {
    $control = Invoke-RecordedGo ($case.name + '_control') $fixtureRoot @('test',('./' + $case.control))
    $negative = Invoke-RecordedGo $case.name $fixtureRoot @('test',('./' + $case.negative))
    $passed = $control.exit_code -eq 0 -and $negative.exit_code -ne 0 -and $negative.output -match $case.diagnostic_pattern
    $caseResults.Add([ordered]@{name=$case.name;passed=$passed;expected_diagnostic=$case.diagnostic_pattern;control=$control;negative=$negative})
    if (-not $passed) { $failures.Add($case.name) }
    Write-Output ($case.name + ': ' + $(if ($passed) { 'PASS' } else { 'FAIL' }))
}

$sourceHashes = [Collections.Generic.List[object]]::new()
foreach ($file in (Get-ChildItem -LiteralPath $fixtureRoot -Recurse -File | Where-Object { $_.Extension -in @('.go','.mod','.json','.ps1') } | Sort-Object FullName)) {
    $sourceHashes.Add([ordered]@{path=$file.FullName.Substring($fixtureRoot.Length + 1).Replace('\','/');sha256=(Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()})
}
$helperCopies = [Collections.Generic.List[object]]::new()
foreach ($file in (Get-ChildItem -LiteralPath (Join-Path $projectRoot 'authoring') -Filter '*.go')) {
    $originalHash = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash
    $copiedHash = (Get-FileHash -LiteralPath (Join-Path $fixtureRoot ('authoring/' + $file.Name)) -Algorithm SHA256).Hash
    $equal = $originalHash -eq $copiedHash
    $helperCopies.Add([ordered]@{file=$file.Name;identical=$equal;sha256=$originalHash.ToLowerInvariant()})
    if (-not $equal) { $failures.Add('Authoring copy differs: ' + $file.Name) }
}
$record = [ordered]@{
    date='2026-10-05';kind='Bounded Go authoring signature validation';toolchain=$toolVersion;executable=$goExecutable
    environment=[ordered]@{GOTOOLCHAIN='local';GOWORK='off';CGO_ENABLED='0';GOPROXY='off';GOSUMDB='off'}
    checks=$results.ToArray();negative_cases=$caseResults.ToArray();negative_case_count=$caseResults.Count
    helper_copies=$helperCopies.ToArray();fixture_sources=$sourceHashes.ToArray();failures=$failures.ToArray()
    limits='API operation bodies are compile-only stubs. Positive counter, two-window and external-package functions are typechecked, not executed. Existing styling tests execute. No entity/event/action/lifetime behavior, native DLL/ABI, layout, window, GPU, IME/UIA, visual calibration, performance or full parity result is established.'
}
$recordPath = Join-Path $projectRoot 'evidence/authoring-signature-validation.json'
[IO.File]::WriteAllText($recordPath,(ConvertTo-Json -InputObject $record -Depth 9),[Text.UTF8Encoding]::new($false))
Write-Output ('Recorded ' + $caseResults.Count + ' negative/control pairs; failures: ' + $failures.Count)
if ($failures.Count -gt 0) { exit 1 }
