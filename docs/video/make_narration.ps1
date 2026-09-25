# Generates one WAV per narration line (docs/video/narration.json) with the built-in Windows
# speech engine, speeding a line up only if it would overrun its scene. Prints each clip's
# length so docs/video/add_narration.py can place them on the video's timeline.
#   powershell -File docs/video/make_narration.ps1 -OutDir <folder> [-Narration access-narration.json]
param([Parameter(Mandatory)] [string]$OutDir, [string]$Narration = "narration.json",
      [string]$Voice = "Microsoft Zira Desktop")

Add-Type -AssemblyName System.Speech
$lines = Get-Content -Raw -Encoding UTF8 (Join-Path $PSScriptRoot $Narration) | ConvertFrom-Json
New-Item -ItemType Directory -Force $OutDir | Out-Null

function Get-WavSeconds([string]$path) {
    $bytes = [IO.File]::ReadAllBytes($path)
    $byteRate = [BitConverter]::ToInt32($bytes, 28)
    return ($bytes.Length - 44) / $byteRate
}

$i = 0
foreach ($l in $lines) {
    $slot = [double]$l.end - [double]$l.start - 0.3
    $out = Join-Path $OutDir ("line{0:D2}.wav" -f $i)
    foreach ($rate in 0, 1, 2, 3) {
        $s = New-Object System.Speech.Synthesis.SpeechSynthesizer
        $s.SelectVoice($Voice)
        $s.Rate = $rate
        $s.SetOutputToWaveFile($out)
        $s.Speak($l.text)
        $s.Dispose()
        $secs = Get-WavSeconds $out
        if ($secs -le $slot) { break }
    }
    "{0}`t{1}`t{2:N2}`t{3:N2}`t{4}" -f $i, $l.start, $secs, $slot, $rate
    $i++
}
