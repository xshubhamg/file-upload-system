// Package docs embeds the OpenAPI contract so the running service always
// serves the exact specification it was built from.
package docs

import _ "embed"

//go:embed openapi.yaml
var OpenAPI []byte
