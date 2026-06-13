package tools

import (
	"go.opentelemetry.io/otel/attribute"
)

// attrString returns the string value of attr key from the given KV slice.
func attrString(attrs []attribute.KeyValue, key string) string {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}
	return ""
}

// attrBool returns the bool value of attr key from the given KV slice.
func attrBool(attrs []attribute.KeyValue, key string) bool {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.AsBool()
		}
	}
	return false
}

// attrInt returns the int value of attr key (0 if missing).
func attrInt(attrs []attribute.KeyValue, key string) int64 {
	for _, kv := range attrs {
		if string(kv.Key) == key {
			return kv.Value.AsInt64()
		}
	}
	return 0
}
