package provider

import (
	"bufio"
	"io"
	"strings"
)

// SSEReader reads complete SSE records, including multiline data and optional
// whitespace after the field separator. There is no Scanner token-size limit.
type SSEReader struct {
	reader *bufio.Reader
	eof    bool
}

func NewSSEReader(r io.Reader) *SSEReader { return &SSEReader{reader: bufio.NewReader(r)} }
func (r *SSEReader) Next() (string, []byte, error) {
	var event string
	var data []string
	for {
		if r.eof {
			return "", nil, io.EOF
		}
		line, err := r.reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", nil, err
		}
		if err == io.EOF {
			r.eof = true
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			if len(data) > 0 {
				return event, []byte(strings.Join(data, "\n")), nil
			}
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			value = ""
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			data = append(data, value)
		}
		if r.eof && len(data) > 0 {
			return event, []byte(strings.Join(data, "\n")), nil
		}
	}
}
