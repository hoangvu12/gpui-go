//go:build !dev

package main

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui/script"
)

// runDev exists only in the dev build (dev.go, compiled with -tags
// dev — what cmd/tsxdev builds): the release binary never reads
// scripts from disk or links esbuild, so the dev supervisor's
// environment variable gets a clear refusal instead.
func runDev() error {
	return fmt.Errorf("%s is set, but this binary was built without the dev tag: run the app under the dev command (go run github.com/hoangvu12/gpui-go/cmd/tsxdev .) or build it with -tags dev", script.DevEnv)
}
