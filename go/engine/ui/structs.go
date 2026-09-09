package ui

import (
	"net/http"

	"github.com/safing/portmaster-android/go/engine"
)

type PluginCall = engine.PluginCall

var dbCall PluginCall

type Request = struct {
	Method  string              `json:"method"`
	Url     string              `json:"url"`
	Headers map[string][]string `json:"headers"`
	Body    string              `json:"body"`
}

type ResponseWriter struct {
	body       string
	statusCode int
	header     http.Header
}

func NewResponseWriter() *ResponseWriter {
	return &ResponseWriter{
		header: http.Header{},
	}
}

func (w *ResponseWriter) Header() http.Header {
	return w.header
}

func (w *ResponseWriter) Write(b []byte) (int, error) {
	w.body += string(b)
	return len(b), nil
}

func (w *ResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

