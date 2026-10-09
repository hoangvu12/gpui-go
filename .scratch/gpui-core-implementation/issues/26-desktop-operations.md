# Implement remaining Windows app and desktop operations

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: implement-spec wave-2b worker (background subagent, model dashscope/glm-5.3; evidence: ../../evidence/ticket26-desktop-operations.json)
Blocked by: 05, 13

## Question

A Go app uses the reference window/app controls and receives desktop settings, power and lifecycle changes.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [conformance contract](../../../docs/conformance-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Implement the per-symbol ledger's remaining menus/jump lists, cursor/window controls, displays, URL/path/reveal, notifications, restart and related app callbacks where applicable.
- [x] Match settings/theme/power/display changes, suspend/resume, minimize/restore and window/app activation semantics through the foreground dispatcher. Test restart's deferred launch/reentry path, cursor hide-until-move and character-palette modifier release/restore with partial-send cleanup.
- [x] Reproduce verified Windows unsupported/no-op outcomes including native-popup rejection/fallback, auxiliary-executable lookup, hide/unhide-other-apps and nominal thermal-state behavior, rather than returning fake success for missing work.
- [x] Cover bounds/activation/appearance/button-layout observers and set_background_appearance for opaque, blurred and transparent modes; verify both DWM behavior and the renderer clear path under the same mode.
- [x] Trace every remaining Platform/PlatformWindow row to a test or a source-supported unavailable outcome; route any unexpectedly large capability to an explicit bounded follow-up before resolving this ticket.
- [x] Exercise real operations using temporary local resources and controlled events; external launches, restart or user-visible system changes run only when authorized for implementation validation.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Open and close a DPI-aware window on the foreground thread](05-win32-window.md)
- [Route keyboard input through focus, actions and key contexts](13-focus-keyboard.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

- 2026-10-09 — Implemented by the ticket26 worker (background subagent). Files: `gpui/desktop.go` (platform-neutral types + the clear-color mapping), `gpui/desktop_windows.go`, `gpui/desktop_shell_windows.go`, `gpui/desktop_input_windows.go` (the Win32 desktop driver), `gpui/desktop_other.go` (non-windows typed-error stubs), `gpui/desktop_windows_test.go`, `internal/desktopspec/desktop_real_test.go`; edits inside the owned files `gpui/win32host_windows.go` (Host/hostWindow desktop state, new message policies in both window procedures, quit-after-loop, shutdown unregistration), `gpui/window.go` (WindowOptions.Kind/OnAppearanceChanged/OnHoverStatusChange, Window desktop methods, OpenWindow wiring), `gpui/app.go` (the App-level desktop surface).

  Per-symbol source mapping (pinned CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a): set_cursor_style/hide_cursor_until_mouse_moves/is_cursor_visible + WM_SETCURSOR/WM_GPUI_CURSOR_STYLE_CHANGED/WM_MOUSE(MOVE|LEAVE)/WM_NCMOUSE(MOVE|LEAVE) cursor policies (platform.rs, events.rs, util.rs load_cursor); minimize/zoom/restore/title/active/request_attention/mouse_position/is_hovered (window.rs); displays/primary_display + WM_DISPLAYCHANGE + display-on-move (display.rs, events.rs); open_url/open_with_system/reveal_path via ShellExecuteW + SHGetDesktopFolder/ParseDisplayName/SHOpenFolderAndSelectItems with the ERROR_FILE_NOT_FOUND fallback (platform.rs open_target/open_target_in_explorer); restart with encode_restart_arguments and the deferred foreground launch (platform.rs, gpui_ce_util get_powershell/new_std_command); WM_SETTINGCHANGE (system settings refresh + ImmersiveColorSet theme path with configure_dwm_dark_mode), WM_POWERBROADCAST/PBT_APMRESUMEAUTOMATIC + RegisterSuspendResumeNotification, WM_ENDSESSION/WM_GPUI_END_SESSION (platform.rs handle_msg family, events.rs, system_settings.rs); set_background_appearance for all five modes (SetWindowCompositionAttribute accent policy + DwmSetWindowAttribute(DWMWA_SYSTEMBACKDROP_TYPE)) with the renderer clear path (ClearColorForBackground, directx_renderer.rs render); show_character_palette with modifier release/restore and partial-send cleanup (window.rs); menus/dock-menu/jump-list state machine with perform_dock_menu_action dispatch through WM_GPUI_DOCK_MENU_ACTION (destination_list.rs, platform.rs); set_app_identity via SetCurrentProcessExplicitAppUserModelID + GetCurrentPackageFullName; thermal Nominal, empty activate/hide, unimplemented!() hide/unhide other apps, "not yet implemented" auxiliary executable, "register_url_scheme unimplemented", popup rejection (PopupNotSupportedError).

  Executed: `CGO_ENABLED=0 go test ./internal/desktopspec ./gpui -count=1` — ok (18 desktopspec tests, 146 gpui tests). Real windows were opened, minimized, maximized, restored, retitled and closed; controlled events were delivered synthetically to this test's own windows (WM_SETTINGCHANGE with ImmersiveColorSet and SPI_GETWHEELSCROLLLINES, WM_DISPLAYCHANGE, WM_POWERBROADCAST resume/suspend, WM_ENDSESSION, WM_MOUSEMOVE/WM_MOUSELEAVE, WM_SETCURSOR); RegisterSuspendResumeNotification/Unregister, DwmSetWindowAttribute/DwmGetWindowAttribute, SystemParametersInfoW, EnumDisplayMonitors/GetMonitorInfoW/GetDpiForMonitor, SetCurrentProcessExplicitAppUserModelID and registry theme reads really ran. NOT executed (guarded off behind seams whose defaults are the real syscalls, per the ticket's no-external-launch rule): no URL opened, no Explorer reveal, no PowerShell restart spawn, no SendInput injection (the palette's inactive-window guard and the pure input planning were tested instead), no toast shown, no taskbar jump list written, no MessageBeep.

  Bounded follow-ups (typed errors, never fake success): `ErrJumpListShellCommitUnavailable` — the ICustomDestinationList shell commit (BeginList/AppendCategory/AddUserTasks/CommitList) is not ported to the pure-stdlib host; the in-process jump-list state and dock-action dispatch are real. `ErrToastNotifierUnavailable` — the WinRT ToastNotificationManager activation is not ported; the source-supported no-identity outcome (recorded warning, no notification, success) is reproduced exactly, and with an identity the explicit pending row surfaces.

  Documented deviations: (1) system_appearance reads the Personalize registry value instead of the WinRT UISettings foreground color (WinRT activation is unported; the provider is a seam and the ImmersiveColorSet observer routing is fully tested with an injected appearance — the machine's real read is exercised read-only); (2) ShellExecuteW/reveal run on the host (STA) thread instead of a background thread, after the current update unwinds — the same pumping discipline the reference applies to restart; (3) WM_ACTIVATE's synthesized ModifiersChanged is posted to the foreground queue instead of invoked in the message handler, because a WM_ACTIVATE inside CreateWindowExW/ShowWindow would re-enter App.OpenWindow before the logical window exists (the reference's GPUI window pre-exists its platform window); (4) the pin does NOT handle WM_DWMCOLORIZATIONCOLOR — the ticket's mention is stale; the pinned theme mechanism is WM_SETTINGCHANGE/ImmersiveColorSet only, which is what this port implements; (5) should_auto_hide_scrollbars (WinRT UISettings.AutoHideScrollBars) is not ported and stays an open ledger row for the follow-up.

## Answer

Resolved 2026-10-09. Implemented by the wave-2 desktop implementer subagent (session impl-26-desktop, clean completion) and verified by the orchestrator (desktopspec 18 real-window tests, gpui 146 tests, winhostspec/focusspec/imespec/dialogspec regressions, vet+gofmt clean). The per-symbol ledger's remaining Windows app/desktop operations: cursor style/visibility policies with hide-until-move, window controls (minimize/zoom/pending-maximize/attention/titles), display enumeration and observers, open_url/open_with/reveal through real ShellExecuteW/IShellFolder paths with argument marshaling verified and external launches guarded off, restart's deferred launch/reentry path, settings/theme (WM_SETTINGCHANGE/ImmersiveColorSet with the DWM dark-mode read-back), power/suspend/resume/end-session, all five background-appearance modes (DWM + the renderer clear path), the character palette's modifier release/restore with partial-send cleanup, menus/dock-menu dispatch, app identity, notifications, and the verified unsupported outcomes as typed errors (jump-list shell commit, WinRT toast, hide/unhide-other-apps panic, thermal Nominal). Executed evidence: [evidence/ticket26-desktop-operations.json](../../../evidence/ticket26-desktop-operations.json).
