// Package api holds the OpenAPI 3.1 contract of the HTTP API. The server publishes it at
// GET /openapi.yaml, and a test checks that it describes exactly the routes the router serves.
package api

import _ "embed"

// OpenAPI is the OpenAPI document of the HTTP API in YAML.
//
//go:embed openapi.yaml
var OpenAPI string
