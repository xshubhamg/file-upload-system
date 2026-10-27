// Package response defines the stable JSON envelope used by every endpoint.
package response

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/xshubhamg/file-upload/internal/apperr"
)

const RequestIDKey = "request_id"

type ErrorEnvelope struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Details   map[string]string `json:"details,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
}

func RequestID(c *gin.Context) string {
	if value, ok := c.Get(RequestIDKey); ok {
		if id, ok := value.(string); ok {
			return id
		}
	}
	return ""
}

func JSON(c *gin.Context, status int, payload any) {
	c.JSON(status, payload)
}

// Error converts any error into the public envelope. Unknown errors are
// intentionally opaque; the cause stays in logs.
func Error(c *gin.Context, err error) {
	appErr := apperr.From(err)
	if appErr == nil {
		appErr = apperr.Internal(errors.New("nil error"))
	}

	_ = c.Error(err)

	if c.Writer.Written() {
		c.Abort()
		return
	}

	c.AbortWithStatusJSON(appErr.Status, ErrorEnvelope{
		Error: ErrorDetail{
			Code:      appErr.Code,
			Message:   appErr.Message,
			Details:   appErr.Details,
			RequestID: RequestID(c),
		},
	})
}

// BindJSON binds a JSON body and writes a 400 or 413 envelope on failure.
func BindJSON(c *gin.Context, dst any) bool {
	if err := c.ShouldBindJSON(dst); err != nil {
		if IsBodyTooLarge(err) {
			Error(c, apperr.PayloadTooLarge("request body is too large"))
			return false
		}
		Error(c, apperr.Validation("invalid JSON body").
			WithDetail("body", "must be valid JSON matching the documented schema"))
		return false
	}
	return true
}

func IsBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return true
	}
	// Gin's validator wraps some transport errors as plain strings.
	return err != nil && (errors.Is(err, http.ErrBodyReadAfterClose) ||
		contains(err.Error(), "request body too large") ||
		contains(err.Error(), "http: request body too large"))
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
