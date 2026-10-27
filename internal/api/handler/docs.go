package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/docs"
)

const swaggerHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>file-upload API</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: "/api/v1/openapi.yaml",
      dom_id: "#swagger-ui",
      deepLinking: true,
      persistAuthorization: true
    });
  </script>
</body>
</html>`

func OpenAPI(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", docs.OpenAPI)
}

func SwaggerUI(c *gin.Context) {
	// Swagger UI needs its CDN assets; every other route keeps the strict CSP.
	c.Header("Content-Security-Policy", "default-src 'none'; script-src https://unpkg.com 'unsafe-inline'; style-src https://unpkg.com 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerHTML))
}
