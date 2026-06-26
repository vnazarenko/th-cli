//go:build tools

// Package tools pins build-time-only tool dependencies so `go generate` resolves
// a reproducible oapi-codegen version recorded in go.mod/go.sum. The `tools`
// build tag keeps this file (and the heavy codegen dependency tree) out of the
// shipped `th` binary — it is never compiled into a normal build.
package tools

import (
	// oapi-codegen generates the typed API client from public-api.openapi.yaml.
	_ "github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen"
)
