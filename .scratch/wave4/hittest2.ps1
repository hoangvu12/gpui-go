Add-Type @'
using System;
using System.Runtime.InteropServices;
public class HT2 {
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr CreateWindowExW(int ex, string cls, string name, int style, int x, int y, int w, int h, IntPtr parent, IntPtr menu, IntPtr inst, IntPtr param);
  [DllImport("user32.dll")] public static extern IntPtr WindowFromPoint(int x, int y);
  [DllImport("user32.dll")] public static extern int GetClassName(IntPtr h, System.Text.StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern bool DestroyWindow(IntPtr h);
}
'@
$style = 0x10000000 -bor 0x00C00000 -bor 0x00080000
$h = [HT2]::CreateWindowExW(0, "STATIC", "hit", $style, 760, 386, 400, 260, [IntPtr]::Zero, [IntPtr]::Zero, [IntPtr]::Zero, [IntPtr]::Zero)
Start-Sleep -Milliseconds 500
$at = [HT2]::WindowFromPoint(960, 516)
$c = New-Object System.Text.StringBuilder 256; [HT2]::GetClassName($at, $c, 256) | Out-Null
Write-Output ("fresh static window=" + $h + " WFP(960,516)=" + $at + " class=" + $c + " same=" + ($at -eq $h))
[HT2]::DestroyWindow($h) | Out-Null
