package provider

import (
	"errors"
	"github.com/pwagstro/simple_llm_proxy/internal/model"
	"io"
	"strings"
	"testing"
)

func TestSSERecords(t *testing.T) {
	r := NewSSEReader(strings.NewReader(": keepalive\r\nevent: chunk\r\ndata:{\r\ndata: \"a\": 1}\r\n\r\ndata: [DONE]"))
	event, data, err := r.Next()
	if err != nil || event != "chunk" || string(data) != "{\n\"a\": 1}" {
		t.Fatalf("%s %s %v", event, data, err)
	}
	_, data, err = r.Next()
	if err != nil || string(data) != "[DONE]" {
		t.Fatal(string(data), err)
	}
	if _, _, err = r.Next(); err != io.EOF {
		t.Fatal(err)
	}
}
func TestSSELargeRecord(t *testing.T) {
	want := strings.Repeat("x", 100000)
	_, data, err := NewSSEReader(strings.NewReader("data:" + want + "\n\n")).Next()
	if err != nil || string(data) != want {
		t.Fatal("large record truncated", err)
	}
}
func TestClosedStreamRetainsTransportError(t *testing.T) {
	for i := 0; i < 100; i++ {
		chunks := make(chan *model.StreamChunk)
		errs := make(chan error, 1)
		errs <- io.ErrUnexpectedEOF
		close(chunks)
		_, err := NewStream(chunks, errs, nil).Recv()
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("lost error: %v", err)
		}
		events := make(chan *model.ResponsesStreamEvent)
		responseErrs := make(chan error, 1)
		responseErrs <- io.ErrUnexpectedEOF
		close(events)
		_, err = NewResponsesStream(events, responseErrs, nil).Recv()
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("lost Responses error: %v", err)
		}
	}
}
