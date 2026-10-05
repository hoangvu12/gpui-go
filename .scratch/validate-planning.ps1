$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
Set-Location -LiteralPath $repoRoot
$errors = [Collections.Generic.List[string]]::new()
$docPaths = @(rg --files docs .scratch/gpui-core .scratch/gpui-core-implementation -g '*.md')
$docPaths += @('AGENTS.md','CLAUDE.md','PROJECT.md','README.md','CONTEXT.md','handoff/resume.md','evidence/README.md','research/19-conformance-update-policy.md','research/20-conformance-spec-coverage-review.md','.scratch/signature-validation/README.md')
$docPaths = @($docPaths | Sort-Object -Unique)
$docRecords = [Collections.Generic.List[object]]::new()
$linkCount = 0
$tick = [string][char]96
foreach ($relative in $docPaths) {
    $absolute = Join-Path $repoRoot $relative
    $body = [IO.File]::ReadAllText($absolute)
    $fences = [regex]::Matches($body, '(?m)^\s*' + $tick + '{3}').Count
    if ($fences % 2 -ne 0) { $errors.Add("Unbalanced fences: $relative") }
    $plain = [regex]::Replace($body, '(?ms)^' + $tick + '{3}[^\r\n]*\r?\n.*?^' + $tick + '{3}[^\r\n]*', '')
    $plain = [regex]::Replace($plain, $tick + '[^' + $tick + '\r\n]*' + $tick, '')
    foreach ($m in [regex]::Matches($plain, '\[[^\]\r\n]*\]\(([^)\r\n]+)\)')) {
        $target = $m.Groups[1].Value.Trim()
        if ($target -match '^(https?://|mailto:|app://|#)') { continue }
        $target = ($target -split '#',2)[0].Trim('<','>')
        if (-not $target) { continue }
        $target = [Uri]::UnescapeDataString($target)
        $linkCount++
        $resolved = if ([IO.Path]::IsPathRooted($target)) { $target } else { Join-Path (Split-Path $absolute) $target }
        if (-not (Test-Path -LiteralPath $resolved)) { $errors.Add("Missing local link: $relative -> $target") }
    }
    $docRecords.Add(@{path=$relative.Replace('\','/'); sha256=(Get-FileHash -LiteralPath $absolute -Algorithm SHA256).Hash.ToLowerInvariant()})
}
function Read-Tracker([string]$folder, [string]$expectedStatus, [int]$expectedCount) {
    $graph = @{}
    $records = [Collections.Generic.List[object]]::new()
    foreach ($file in Get-ChildItem -LiteralPath $folder -Filter '*.md' | Sort-Object Name) {
        $body = [IO.File]::ReadAllText($file.FullName)
        $id = $file.Name.Substring(0,2)
        $status = [regex]::Match($body,'(?m)^Status: ([^\r\n]+)').Groups[1].Value
        $assignee = [regex]::Match($body,'(?m)^Assignee: ([^\r\n]+)').Groups[1].Value
        $blockersText = [regex]::Match($body,'(?m)^Blocked by: ([^\r\n]+)').Groups[1].Value
        $blockers = if ($blockersText -eq 'none') { @() } else { @($blockersText -split ',\s*') }
        if ($status -ne $expectedStatus) { $errors.Add("Unexpected status: $folder/$id = $status") }
        if ($expectedStatus -eq 'open' -and $assignee -ne 'unassigned') { $errors.Add("Unexpected claim: $folder/$id") }
        if ($expectedStatus -eq 'resolved' -and $body -notmatch '(?m)^## Answer') { $errors.Add("Missing answer: $folder/$id") }
        $parent = [regex]::Match($body,'(?m)^Parent: ([^\r\n]+)').Groups[1].Value
        if (-not (Test-Path -LiteralPath (Join-Path $file.DirectoryName $parent))) { $errors.Add("Missing parent: $folder/$id") }
        $graph[$id] = $blockers
        $records.Add(@{id=$id;file=$file.Name;status=$status;blocked_by=$blockers})
    }
    if ($records.Count -ne $expectedCount) { $errors.Add("Unexpected ticket count: $folder = $($records.Count)") }
    foreach ($id in $graph.Keys) {
        foreach ($dep in $graph[$id]) {
            if (-not $graph.ContainsKey($dep)) { $errors.Add("Missing blocker: $folder/$id -> $dep") }
            if ($folder -like '*implementation*' -and [int]$dep -ge [int]$id) { $errors.Add("Nonpreceding blocker: $folder/$id -> $dep") }
        }
    }
    $visiting = [Collections.Generic.HashSet[string]]::new()
    $visited = [Collections.Generic.HashSet[string]]::new()
    function Visit-Node([string]$node) {
        if ($visited.Contains($node)) { return }
        if (-not $visiting.Add($node)) { $errors.Add("Dependency cycle: $folder/$node"); return }
        foreach ($dep in $graph[$node]) { if ($graph.ContainsKey($dep)) { Visit-Node $dep } }
        [void]$visiting.Remove($node)
        [void]$visited.Add($node)
    }
    foreach ($id in $graph.Keys) { Visit-Node $id }
    $reachable = [Collections.Generic.HashSet[string]]::new()
    function Reach-Node([string]$node) {
        foreach ($dep in $graph[$node]) { if ($reachable.Add($dep)) { Reach-Node $dep } }
    }
    if ($folder -like '*implementation*') {
        Reach-Node '33'
        if ($reachable.Count -ne 32) { $errors.Add("Final acceptance omits tickets: reachable $($reachable.Count)/32") }
    }
    return @{tickets=$records.ToArray();count=$records.Count;acyclic=($visiting.Count -eq 0);final_acceptance_prerequisite_count=$reachable.Count}
}
$wayfinder = Read-Tracker '.scratch/gpui-core/issues' 'resolved' 10
$implementation = Read-Tracker '.scratch/gpui-core-implementation/issues' 'open' 33
$mapBody = Get-Content -LiteralPath '.scratch/gpui-core/map.md' -Raw
if ($mapBody -notmatch '(?m)^Status: resolved') { $errors.Add('Wayfinder map not resolved') }
$snapshotRecords = [Collections.Generic.List[object]]::new()
foreach ($name in @('core','windows-platform','renderer','distribution')) {
    $manifestPath = Join-Path $repoRoot "evidence/$name/manifest.json"
    $entries = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
    $verified = 0
    foreach ($entry in $entries) {
        $filePath = if ($entry.local -like 'evidence/*') { Join-Path $repoRoot $entry.local } else { Join-Path (Split-Path $manifestPath) $entry.local }
        if ((Get-FileHash -LiteralPath $filePath -Algorithm SHA256).Hash -ne $entry.sha256) { $errors.Add("Snapshot hash mismatch: $filePath") } else { $verified++ }
    }
    $snapshotRecords.Add(@{manifest="evidence/$name/manifest.json";entries=@($entries).Count;verified=$verified})
}
$signature = Get-Content -LiteralPath 'evidence/authoring-signature-validation.json' -Raw | ConvertFrom-Json
if (@($signature.failures).Count -ne 0) { $errors.Add('Signature evidence contains failures') }
if (@($signature.negative_cases).Count -ne 29) { $errors.Add('Signature case count not 29') }
foreach ($check in $signature.checks) { if ($check.exit_code -ne 0) { $errors.Add("Compiler check failed: $($check.name)") } }
foreach ($case in $signature.negative_cases) {
    if (-not $case.passed -or $case.control.exit_code -ne 0 -or $case.negative.exit_code -eq 0 -or $case.negative.output -notmatch $case.expected_diagnostic) { $errors.Add("Negative/control mismatch: $($case.name)") }
}
$fixtureVerified = 0
foreach ($entry in $signature.fixture_sources) {
    $filePath = Join-Path $repoRoot ".scratch/signature-validation/$($entry.path)"
    if ((Get-FileHash -LiteralPath $filePath -Algorithm SHA256).Hash -ne $entry.sha256) { $errors.Add("Fixture hash mismatch: $($entry.path)") } else { $fixtureVerified++ }
}
foreach ($file in Get-ChildItem -LiteralPath 'authoring' -Filter '*.go') {
    $copy = Join-Path $repoRoot ".scratch/signature-validation/authoring/$($file.Name)"
    if ((Get-FileHash -LiteralPath $file.FullName).Hash -ne (Get-FileHash -LiteralPath $copy).Hash) { $errors.Add("Authoring copy mismatch: $($file.Name)") }
}
$record = [ordered]@{
    date='2026-10-05'
    kind='Wayfinder completion documentation and evidence validation'
    passed=($errors.Count -eq 0)
    document_count=$docPaths.Count
    local_link_count=$linkCount
    documents=$docRecords.ToArray()
    wayfinder=$wayfinder
    implementation=$implementation
    snapshots=$snapshotRecords.ToArray()
    signature_evidence=@{toolchain=$signature.toolchain;negative_control_pairs=29;fixture_sources_verified=$fixtureVerified;failures=@($signature.failures).Count}
    errors=$errors.ToArray()
    limits='Checks document links, tracker/DAG/coverage reachability and recorded source hashes. Does not rerun compiler tests or execute framework/native/oracle/visual/consumer gates. Compiler execution is separately recorded in authoring-signature-validation.json.'
}
$json = ConvertTo-Json -InputObject $record -Depth 12
[IO.File]::WriteAllText((Join-Path $repoRoot 'evidence/wayfinder-completion-validation.json'),$json + [Environment]::NewLine,[Text.UTF8Encoding]::new($false))
[pscustomobject]$record | Select-Object date,passed,document_count,local_link_count,@{n='wayfinder_count';e={$wayfinder.count}},@{n='implementation_count';e={$implementation.count}},@{n='fixture_sources_verified';e={$fixtureVerified}},errors | ConvertTo-Json -Depth 5
if ($errors.Count -ne 0) { exit 1 }
