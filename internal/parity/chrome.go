//go:build windows

// Headless-Chrome discovery: the locally installed Chrome the
// recorder drives (the ticket-13 probe's browser; the version is
// recorded in every fixture's record.json).
package parity

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// chromeEnvVar names the environment variable that overrides the
// Chrome path (set it when Chrome lives somewhere the search misses).
const chromeEnvVar = "GPUI_PARITY_CHROME"

// FindChrome returns the path of the locally installed Chrome, or an
// error naming the places it looked. The search order: the
// GPUI_PARITY_CHROME override, PATH, then the standard install
// locations. The version is read from the DevTools endpoint at record
// time (launching chrome.exe --version opens the user's browser, so
// the recorder never does that).
func FindChrome() (string, error) {
	if override := os.Getenv(chromeEnvVar); override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("%s=%q: %w", chromeEnvVar, override, err)
		}
		return override, nil
	}
	if path, err := exec.LookPath("chrome"); err == nil {
		return path, nil
	}
	if path, err := exec.LookPath("chrome.exe"); err == nil {
		return path, nil
	}
	candidates := []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no Chrome found (checked PATH, %s and the standard install locations; set %s to the chrome.exe path)",
		candidates, chromeEnvVar)
}
