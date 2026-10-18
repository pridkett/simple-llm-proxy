package model

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
)

// Retain unknown JSON fields while exposing fields needed for routing,
// translation, and accounting. Typed edits (such as the selected model) win;
// provider-specific fields and explicit nulls survive a proxy round trip.
func readObject(data []byte, dst any) (map[string]json.RawMessage, error) {
	if err := json.Unmarshal(data, dst); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	typ := reflect.TypeOf(dst).Elem()
	for i := 0; i < typ.NumField(); i++ {
		name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" && !bytes.Equal(fields[name], []byte("null")) {
			delete(fields, name)
		}
	}
	return fields, nil
}

func writeObject(value any, original map[string]json.RawMessage) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(original) == 0 {
		return encoded, nil
	}
	fields := make(map[string]json.RawMessage, len(original))
	for k, v := range original {
		fields[k] = v
	}
	var edited map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &edited); err != nil {
		return nil, err
	}
	for k, v := range edited {
		fields[k] = v
	}
	return json.Marshal(fields)
}

func (x *Message) UnmarshalJSON(data []byte) error {
	type wire Message
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x Message) MarshalJSON() ([]byte, error) {
	type wire Message
	return writeObject(wire(x), x.rawFields)
}

func (x *ToolCall) UnmarshalJSON(data []byte) error {
	type wire ToolCall
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x ToolCall) MarshalJSON() ([]byte, error) {
	type wire ToolCall
	return writeObject(wire(x), x.rawFields)
}

func (x *FunctionDef) UnmarshalJSON(data []byte) error {
	type wire FunctionDef
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x FunctionDef) MarshalJSON() ([]byte, error) {
	type wire FunctionDef
	return writeObject(wire(x), x.rawFields)
}

func (x *FunctionCall) UnmarshalJSON(data []byte) error {
	type wire FunctionCall
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x FunctionCall) MarshalJSON() ([]byte, error) {
	type wire FunctionCall
	return writeObject(wire(x), x.rawFields)
}

func (x *ResponseFormat) UnmarshalJSON(data []byte) error {
	type wire ResponseFormat
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x ResponseFormat) MarshalJSON() ([]byte, error) {
	type wire ResponseFormat
	return writeObject(wire(x), x.rawFields)
}

func (x *EmbeddingsRequest) UnmarshalJSON(data []byte) error {
	type wire EmbeddingsRequest
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x EmbeddingsRequest) MarshalJSON() ([]byte, error) {
	type wire EmbeddingsRequest
	return writeObject(wire(x), x.rawFields)
}

func (x *ChatCompletionResponse) UnmarshalJSON(data []byte) error {
	type wire ChatCompletionResponse
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x ChatCompletionResponse) MarshalJSON() ([]byte, error) {
	type wire ChatCompletionResponse
	return writeObject(wire(x), x.rawFields)
}

func (x *Choice) UnmarshalJSON(data []byte) error {
	type wire Choice
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x Choice) MarshalJSON() ([]byte, error) {
	type wire Choice
	return writeObject(wire(x), x.rawFields)
}

func (x *Delta) UnmarshalJSON(data []byte) error {
	type wire Delta
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x Delta) MarshalJSON() ([]byte, error) {
	type wire Delta
	return writeObject(wire(x), x.rawFields)
}

func (x *EmbeddingsResponse) UnmarshalJSON(data []byte) error {
	type wire EmbeddingsResponse
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x EmbeddingsResponse) MarshalJSON() ([]byte, error) {
	type wire EmbeddingsResponse
	return writeObject(wire(x), x.rawFields)
}

func (x *EmbeddingData) UnmarshalJSON(data []byte) error {
	type wire EmbeddingData
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x EmbeddingData) MarshalJSON() ([]byte, error) {
	type wire EmbeddingData
	return writeObject(wire(x), x.rawFields)
}

func (x *StreamChunk) UnmarshalJSON(data []byte) error {
	type wire StreamChunk
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x StreamChunk) MarshalJSON() ([]byte, error) {
	type wire StreamChunk
	return writeObject(wire(x), x.rawFields)
}

func (x *StreamOptions) UnmarshalJSON(data []byte) error {
	type wire StreamOptions
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x StreamOptions) MarshalJSON() ([]byte, error) {
	type wire StreamOptions
	return writeObject(wire(x), x.rawFields)
}

func (x *ChatCompletionRequest) UnmarshalJSON(data []byte) error {
	type wire ChatCompletionRequest
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	// OpenAI accepts either a single stop string or an array. Keep []string for
	// native-provider translators and restore the original string on forwarding.
	var stop string
	x.stopString = false
	if v, ok := fields["stop"]; ok && len(v) > 0 && v[0] == '"' {
		if err := json.Unmarshal(v, &stop); err != nil {
			return err
		}
		x.stopString = true
		normalized := make(map[string]json.RawMessage, len(fields))
		for k, v := range fields {
			normalized[k] = v
		}
		normalized["stop"], _ = json.Marshal([]string{stop})
		var err error
		data, err = json.Marshal(normalized)
		if err != nil {
			return err
		}
	}
	unknown, err := readObject(data, (*wire)(x))
	if err != nil {
		return err
	}
	x.rawFields = unknown
	return nil
}
func (x ChatCompletionRequest) MarshalJSON() ([]byte, error) {
	type wire ChatCompletionRequest
	data, err := writeObject(wire(x), x.rawFields)
	if err != nil || !x.stopString || len(x.Stop) != 1 {
		return data, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	fields["stop"], _ = json.Marshal(x.Stop[0])
	return json.Marshal(fields)
}

func (x *Usage) UnmarshalJSON(data []byte) error {
	type wire Usage
	fields, err := readObject(data, (*wire)(x))
	if err != nil {
		return err
	}
	x.rawFields = fields
	var original map[string]json.RawMessage
	if err := json.Unmarshal(data, &original); err != nil {
		return err
	}
	if _, ok := original["prompt_tokens"]; !ok {
		if v, exists := fields["input_tokens"]; exists {
			if err := json.Unmarshal(v, &x.PromptTokens); err != nil {
				return err
			}
		}
	}
	if _, ok := original["completion_tokens"]; !ok {
		if v, exists := fields["output_tokens"]; exists {
			if err := json.Unmarshal(v, &x.CompletionTokens); err != nil {
				return err
			}
		}
	}
	return nil
}
func (x Usage) MarshalJSON() ([]byte, error) {
	type wire Usage
	data, err := writeObject(wire(x), x.rawFields)
	if err != nil {
		return nil, err
	}
	if _, native := x.rawFields["input_tokens"]; !native {
		return data, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	delete(fields, "prompt_tokens")
	delete(fields, "completion_tokens")
	fields["input_tokens"], _ = json.Marshal(x.PromptTokens)
	fields["output_tokens"], _ = json.Marshal(x.CompletionTokens)
	return json.Marshal(fields)
}

func (x *Tool) UnmarshalJSON(data []byte) error {
	type wire Tool
	fields, err := readObject(data, (*wire)(x))
	x.rawFields = fields
	return err
}
func (x Tool) MarshalJSON() ([]byte, error) { type wire Tool; return writeObject(wire(x), x.rawFields) }
