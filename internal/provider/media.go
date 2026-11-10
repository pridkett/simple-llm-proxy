package provider

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
)

// MediaProvider is the optional native OpenAI media transport. The API handler
// supplies an allowlisted path and a body with the selected deployment's model.
type MediaProvider interface {
	ForwardMedia(ctx context.Context, path, contentType string, body io.Reader, headers http.Header) (*http.Response, error)
	ConnectRealtime(ctx context.Context, path string, query url.Values, headers http.Header) (*websocket.Conn, error)
}
