// pkg/web/response.go
package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Standard API response structure
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   *ErrorInfo  `json:"error,omitempty"`
	Meta    *Meta       `json:"meta,omitempty"`
}

// ErrorInfo contains detailed error information
type ErrorInfo struct {
	Code    int         `json:"code"`              // HTTP status code
	Message string      `json:"message"`           // User-friendly message
	Details interface{} `json:"details,omitempty"` // Additional error details
	Stack   string      `json:"-"`                 // Stack trace (excluded from JSON)
}

// Meta holds pagination metadata
type Meta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// SuccessResponse sends a successful response with data
func Success(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := Response{
		Success: true,
		Data:    data,
	}
	json.NewEncoder(w).Encode(resp)
}

// SuccessWithMeta sends a successful response with pagination metadata
func SuccessWithMeta(w http.ResponseWriter, status int, data interface{}, meta Meta) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	resp := Response{
		Success: true,
		Data:    data,
		Meta:    &meta,
	}
	json.NewEncoder(w).Encode(resp)
}

// Error sends an error response
func Error(w http.ResponseWriter, status int, message string, details ...interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	errInfo := &ErrorInfo{
		Code:    status,
		Message: message,
	}
	if len(details) > 0 && details[0] != nil {
		errInfo.Details = details[0]
	}
	resp := Response{
		Success: false,
		Error:   errInfo,
	}
	json.NewEncoder(w).Encode(resp)
}

// Errorf sends an error response with formatted message
func Errorf(w http.ResponseWriter, status int, format string, args ...interface{}) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	Error(w, status, msg)
}

// ValidationError sends a 422 Unprocessable Entity with validation details
func ValidationError(w http.ResponseWriter, details interface{}) {
	Error(w, http.StatusUnprocessableEntity, "Validation failed", details)
}

// Unauthorized sends a 401 Unauthorized error
func Unauthorized(w http.ResponseWriter, message ...string) {
	msg := "Unauthorized"
	if len(message) > 0 {
		msg = message[0]
	}
	Error(w, http.StatusUnauthorized, msg)
}

// Forbidden sends a 403 Forbidden error
func Forbidden(w http.ResponseWriter, message ...string) {
	msg := "Forbidden"
	if len(message) > 0 {
		msg = message[0]
	}
	Error(w, http.StatusForbidden, msg)
}

// NotFound sends a 404 Not Found error
func NotFound(w http.ResponseWriter, message ...string) {
	msg := "Resource not found"
	if len(message) > 0 {
		msg = message[0]
	}
	Error(w, http.StatusNotFound, msg)
}

// InternalError sends a 500 Internal Server Error
func InternalError(w http.ResponseWriter, err error) {
	// Log the actual error internally (implementation may vary)
	// logger.Error(err)
	Error(w, http.StatusInternalServerError, "Internal server error", err.Error())
}

// NoContent sends a 204 No Content
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// Redirect sends a redirect response
func Redirect(w http.ResponseWriter, r *http.Request, url string, code int) {
	http.Redirect(w, r, url, code)
}

// ---------------------------------------------------------------------
// Pagination helpers
// ---------------------------------------------------------------------

// PaginationParams represents standard pagination query parameters
type PaginationParams struct {
	Page    int
	Limit   int
	SortBy  string
	SortDir string
}

// ParsePagination extracts pagination parameters from request query
func ParsePagination(r *http.Request) PaginationParams {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100 // max limit
	}
	sortBy := r.URL.Query().Get("sort_by")
	sortDir := r.URL.Query().Get("sort_dir")
	if sortDir != "asc" && sortDir != "desc" {
		sortDir = "desc"
	}
	return PaginationParams{
		Page:    page,
		Limit:   limit,
		SortBy:  sortBy,
		SortDir: sortDir,
	}
}

// NewMeta creates a Meta object from pagination parameters and total count
func NewMeta(params PaginationParams, total int64) Meta {
	totalPages := int(total) / params.Limit
	if int(total)%params.Limit != 0 {
		totalPages++
	}
	return Meta{
		Page:       params.Page,
		Limit:      params.Limit,
		Total:      total,
		TotalPages: totalPages,
		HasNext:    params.Page < totalPages,
		HasPrev:    params.Page > 1,
	}
}

// ---------------------------------------------------------------------
// Helper to respond with file download
// ---------------------------------------------------------------------

// File sends a file download response
func File(w http.ResponseWriter, data []byte, filename string, contentType ...string) {
	if len(contentType) > 0 && contentType[0] != "" {
		w.Header().Set("Content-Type", contentType[0])
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

// ---------------------------------------------------------------------
// CORS preflight helpers
// ---------------------------------------------------------------------

// Cors sets CORS headers (can be used as middleware or in handler)
func Cors(w http.ResponseWriter, allowedOrigins ...string) {
	origin := "*"
	if len(allowedOrigins) > 0 {
		origin = strings.Join(allowedOrigins, ",")
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token")
	w.Header().Set("Access-Control-Expose-Headers", "Link")
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Max-Age", "300")
}

// OptionsHandler handles preflight OPTIONS requests
func OptionsHandler(w http.ResponseWriter, r *http.Request) {
	Cors(w)
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------
// Custom response types for common scenarios
// ---------------------------------------------------------------------

// MessageResponse is a simple { "message": "..." } response
type MessageResponse struct {
	Message string `json:"message"`
}

// SuccessMessage sends a success message
func SuccessMessage(w http.ResponseWriter, status int, message string) {
	Success(w, status, MessageResponse{Message: message})
}

// IDResponse is a common response for created resources
type IDResponse struct {
	ID interface{} `json:"id"`
}

// Created sends a 201 response with resource ID
func Created(w http.ResponseWriter, id interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(IDResponse{ID: id})
}

// ---------------------------------------------------------------------
// Streaming responses (Server-Sent Events)
// ---------------------------------------------------------------------

// SSE sends a server-sent event
func SSE(w http.ResponseWriter, event string, data interface{}) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming unsupported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var payload []byte
	var err error
	if str, ok := data.(string); ok {
		payload = []byte(str)
	} else {
		payload, err = json.Marshal(data)
		if err != nil {
			return err
		}
	}
	if event != "" {
		fmt.Fprintf(w, "event: %s\n", event)
	}
	fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
	return nil
}
