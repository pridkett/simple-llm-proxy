package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
)

type mediaBody struct {
	model     string
	sessionID string
	open      func(model string) (io.ReadCloser, string, error)
	cleanup   func()
}

// readMediaBody resolves the model before routing, with files spooled to disk by
// net/http. Each attempt gets a fresh body, so a failover can resend an upload.
func readMediaBody(w http.ResponseWriter, req *http.Request, path string) (mediaBody, error) {
	contentType, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if req.Header.Get("Content-Type") == "" {
		contentType, err = "application/json", nil
	}
	if err != nil {
		return mediaBody{}, fmt.Errorf("invalid Content-Type: %w", err)
	}
	nested := path == "/realtime/client_secrets" || path == "/realtime/calls"
	if contentType == "application/json" && path != "/realtime/calls" {
		req.Body = http.MaxBytesReader(w, req.Body, 32<<20)
		var fields map[string]json.RawMessage
		sessionID, err := decodeSessionRequest(req, &fields, true)
		if err != nil {
			return mediaBody{}, err
		}
		modelFields := fields
		if nested {
			modelFields, err = mediaSession(fields["session"])
			if err != nil {
				return mediaBody{}, err
			}
		}
		alias, err := mediaModel(modelFields["model"])
		if err != nil {
			return mediaBody{}, err
		}
		return mediaBody{model: alias, sessionID: sessionID, open: func(actual string) (io.ReadCloser, string, error) {
			modelFields["model"], _ = json.Marshal(actual)
			if nested {
				fields["session"], _ = json.Marshal(modelFields)
			}
			body, err := json.Marshal(fields)
			return io.NopCloser(bytes.NewReader(body)), "application/json", err
		}}, nil
	}
	if contentType != "multipart/form-data" {
		return mediaBody{}, fmt.Errorf("expected application/json or multipart/form-data")
	}
	var maxBytes int64 = 32 << 20
	if path == "/images/edits" {
		maxBytes = 1 << 30
	}
	if path == "/realtime/calls" {
		maxBytes = 1 << 20
	}
	req.Body = http.MaxBytesReader(w, req.Body, maxBytes)
	if err := req.ParseMultipartForm(1 << 20); err != nil {
		return mediaBody{}, err
	}
	form := req.MultipartForm
	cleanup := func() { _ = form.RemoveAll() }
	field := "model"
	if nested {
		field = "session"
	}
	values := form.Value[field]
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" || len(form.File[field]) != 0 {
		cleanup()
		return mediaBody{}, fmt.Errorf("exactly one %s field is required", field)
	}
	alias := values[0]
	var session map[string]json.RawMessage
	if nested {
		session, err = mediaSession([]byte(alias))
		if err == nil {
			alias, err = mediaModel(session["model"])
		}
		if err != nil {
			cleanup()
			return mediaBody{}, err
		}
	}
	return mediaBody{
		model: alias, sessionID: req.Header.Get("X-Session-ID"), cleanup: cleanup,
		open: func(actual string) (io.ReadCloser, string, error) {
			value := actual
			if nested {
				session["model"], _ = json.Marshal(actual)
				data, err := json.Marshal(session)
				if err != nil {
					return nil, "", err
				}
				value = string(data)
			}
			reader, writer := io.Pipe()
			formWriter := multipart.NewWriter(writer)
			done := make(chan struct{})
			go func() {
				defer close(done)
				err := writeMediaForm(formWriter, form, field, value)
				if err == nil {
					err = formWriter.Close()
				}
				_ = writer.CloseWithError(err)
			}()
			return &mediaUpload{PipeReader: reader, done: done}, formWriter.FormDataContentType(), nil
		},
	}, nil
}

func mediaSession(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var session map[string]json.RawMessage
	if json.Unmarshal(raw, &session) != nil || session == nil {
		return nil, fmt.Errorf("session must be an object containing a model")
	}
	return session, nil
}

func mediaModel(raw json.RawMessage) (string, error) {
	var alias string
	if json.Unmarshal(raw, &alias) != nil || strings.TrimSpace(alias) == "" {
		return "", fmt.Errorf("model is required")
	}
	return alias, nil
}

func writeMediaForm(w *multipart.Writer, form *multipart.Form, modelField, value string) error {
	if err := w.WriteField(modelField, value); err != nil {
		return err
	}
	for field, values := range form.Value {
		if field == modelField {
			continue
		}
		for _, value := range values {
			if err := w.WriteField(field, value); err != nil {
				return err
			}
		}
	}
	for _, files := range form.File {
		for _, header := range files {
			part, err := w.CreatePart(header.Header)
			if err != nil {
				return err
			}
			file, err := header.Open()
			if err != nil {
				return err
			}
			_, err = io.Copy(part, file)
			_ = file.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Close unblocks the multipart writer on early upstream errors before the
// request handler removes temporary upload files.
type mediaUpload struct {
	*io.PipeReader
	done <-chan struct{}
}

func (upload *mediaUpload) Close() error {
	err := upload.PipeReader.Close()
	<-upload.done
	return err
}
