Add-Type @'
using System;
using System.Runtime.InteropServices;
public class HitTest {
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr CreateWindowExW(int ex, string cls, string name, int style, int x, int y, int w, int h, IntPtr parent, IntPtr menu, IntPtr inst, IntPtr param);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
  [DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h, IntPtr after, int x, int y, int w, int hh, uint flags);
  [DllImport("user32.dll")] public static extern IntPtr WindowFromPoint(int x, int y);
  [DllImport("user32.dll")] public static extern int GetClassName(IntPtr h, System.Text.StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern IntPtr DefWindowProcW(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern ushort RegisterClassW(ref WNDCLASSW wc);
  [StructLayout(LayoutKind.Sequential)] public struct WNDCLASSW {
    public uint style; public IntPtr proc; public int clsExtra; public int wndExtra;
    public IntPtr inst; public IntPtr icon; public IntPtr cursor; public IntPtr bg;
    [MarshalAs(UnmanagedType.LPWStr)] public string menu;
    [MarshalAs(UnmanagedType.LPWStr)] public string name;
  }
}
'@
$proc = Get-Process -Id $PID
$wc = New-Object HitTest+WNDCLASSW
$wc.proc = [IntPtr]::Zero
$wc.name = 'TestHitClass'
$wc.inst = [IntPtr]::Zero
[HitTest]::RegisterClassW([ref]$wc) | Out-Null
$WS_VISIBLE = 0x10000000
$WS_OVERLAPPED = 0x00000000
$WS_CAPTION = 0x00C00000
$WS_SYSMENU = 0x00080000
$style = $WS_VISIBLE -bor $WS_OVERLAPPED -bor $WS_CAPTION -bor $WS_SYSMENU
$h = [HitTest]::CreateWindowExW(0, 'TestHitClass', 'hit', [int]$style, 760, 386, 400, 260, [IntPtr]::Zero, [IntPtr]::Zero, [IntPtr]::Zero, [IntPtr]::Zero)
[HitTest]::ShowWindow($h, 5) | Out-Null
Start-Sleep -Milliseconds 500
$at = [HitTest]::WindowFromPoint(960, 516)
$c = New-Object System.Text.StringBuilder 256; [HitTest]::GetClassName($at, $c, 256) | Out-Null
Write-Host ("fresh window=" + $h + " WindowFromPoint(960,516)=" + $at + " class=" + $c + " same=" + ($at -eq $h))
[HitTest]::SetWindowPos($h, [IntPtr](-1), 0, 0, 0, 0, 0x0003) | Out-Null
Start-Sleep -Milliseconds 300
$at2 = [HitTest]::WindowFromPoint(960, 516)
Write-Host ("after topmost: WindowFromPoint=" + $at2 + " same=" + ($at2 -eq $h))
