package config

import "fmt"

func parseOTelMessageCapture(raw any, capture *OTelMessageCapture) error {
	fields, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("otel_settings.message_capture must be a mapping")
	}
	for key, value := range fields {
		switch key {
		case "application_ids":
			ids, ok := value.([]any)
			if !ok {
				return fmt.Errorf("otel_settings.message_capture.application_ids must be a list")
			}
			for _, rawID := range ids {
				id, ok := rawID.(int)
				if !ok || id <= 0 {
					return fmt.Errorf("otel_settings.message_capture.application_ids must contain positive integers")
				}
				capture.ApplicationIDs = append(capture.ApplicationIDs, int64(id))
			}
		case "max_bytes":
			limit, ok := value.(int)
			if !ok || limit < 1024 || limit > 65536 {
				return fmt.Errorf("otel_settings.message_capture.max_bytes must be between 1024 and 65536")
			}
			capture.MaxBytes = limit
		default:
			return fmt.Errorf("unknown otel_settings.message_capture field %q", key)
		}
	}
	return nil
}
