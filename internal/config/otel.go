package config

import (
	"fmt"
	"net/netip"
)

func parseTrustedTraceparentCIDRs(raw any) ([]netip.Prefix, error) {
	values, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("otel_settings.trusted_traceparent_cidrs must be a list of CIDRs")
	}
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		rawPrefix, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("otel_settings.trusted_traceparent_cidrs must contain CIDR strings")
		}
		prefix, err := netip.ParsePrefix(rawPrefix)
		if err != nil {
			return nil, fmt.Errorf("otel_settings.trusted_traceparent_cidrs contains invalid CIDR %q", rawPrefix)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

// MaxMessageCaptureBytes bounds the serialized capture per direction. The proxy
// exports each direction twice (GenAI and Langfuse attributes), so a span can
// carry about four times this much content.
const MaxMessageCaptureBytes = 4 << 20

func parseOTelMessageCapture(raw any, capture *OTelMessageCapture) error {
	fields, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("otel_settings.message_capture must be a mapping")
	}
	for key, value := range fields {
		switch key {
		case "application_ids", "decision_application_ids":
			ids, ok := value.([]any)
			if !ok {
				return fmt.Errorf("otel_settings.message_capture.%s must be a list", key)
			}
			parsed := make([]int64, 0, len(ids))
			for _, rawID := range ids {
				id, ok := rawID.(int)
				if !ok || id <= 0 {
					return fmt.Errorf("otel_settings.message_capture.%s must contain positive integers", key)
				}
				parsed = append(parsed, int64(id))
			}
			if key == "decision_application_ids" {
				capture.DecisionApplicationIDs = parsed
			} else {
				capture.ApplicationIDs = parsed
			}
		case "max_bytes":
			limit, ok := value.(int)
			if !ok || limit < 1024 || limit > MaxMessageCaptureBytes {
				return fmt.Errorf("otel_settings.message_capture.max_bytes must be between 1024 and %d", MaxMessageCaptureBytes)
			}
			capture.MaxBytes = limit
		default:
			return fmt.Errorf("unknown otel_settings.message_capture field %q", key)
		}
	}
	return nil
}
