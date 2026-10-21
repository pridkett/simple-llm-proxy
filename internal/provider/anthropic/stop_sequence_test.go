package anthropic

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStopSequenceSurvivesInternalTranslation(t *testing.T) {
	p := &Provider{}
	response := p.translateResponse(&anthropicResponse{StopReason: "stop_sequence", StopSequence: "END"}, "alias")
	choice := response.Choices[0]
	if choice.StopSequence == nil || *choice.StopSequence != "END" {
		t.Fatal("matched sequence lost")
	}
	data, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"stop_sequence":`) {
		t.Fatal("internal sequence leaked into OpenAI wire format")
	}
	end := "END"
	chunk := p.translateStreamEvent(&anthropicStreamEvent{Type: "message_delta", Delta: &anthropicDelta{StopReason: "stop_sequence", StopSequence: &end}}, "id", "alias", nil, nil, nil)
	if chunk.Choices[0].StopSequence == nil || *chunk.Choices[0].StopSequence != "END" {
		t.Fatal("stream sequence lost")
	}
}
