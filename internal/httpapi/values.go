package httpapi

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// flexibleTime accepts RFC3339 timestamps and the `datetime-local` value the web
// client submits, so content creation follows the same input contract as the
// update endpoint.
type flexibleTime struct{ time.Time }

// UnmarshalJSON parses the supported timestamp layouts.
func (t *flexibleTime) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		t.Time = time.Time{}
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("时间格式必须为 RFC3339 或 YYYY-MM-DDTHH:MM：%q", raw)
}

// intField reads an integer column from a QueryMaps row, tolerating the numeric
// types the PostgreSQL driver may return and treating missing values as zero.
func intField(row map[string]any, key string) int64 {
	value, ok := row[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int64:
		return typed
	case int32:
		return int64(typed)
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case float32:
		return int64(typed)
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0
		}
		return int64(parsed)
	default:
		return 0
	}
}

// floatField reads a floating point column, treating missing values as zero.
func floatField(row map[string]any, key string) float64 {
	value, ok := row[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int64:
		return float64(typed)
	case int32:
		return float64(typed)
	case int:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(typed, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}

// stringField reads a text column, treating missing values as an empty string.
func stringField(row map[string]any, key string) string {
	value, ok := row[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}

// boolField reads a boolean column, treating missing or null values as false.
func boolField(row map[string]any, key string) bool {
	value, ok := row[key]
	if !ok || value == nil {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, err := strconv.ParseBool(typed)
		return err == nil && parsed
	default:
		return false
	}
}

// timestampField renders a timestamp column as RFC3339, or an empty string when
// the column is missing or not a timestamp.
func timestampField(row map[string]any, key string) string {
	value, ok := row[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case time.Time:
		return typed.Format(time.RFC3339)
	case string:
		return typed
	default:
		return ""
	}
}

// timeField reads a timestamp column as time.Time, reporting false when the
// column is missing or not a timestamp.
func timeField(row map[string]any, key string) (time.Time, bool) {
	value, ok := row[key]
	if !ok || value == nil {
		return time.Time{}, false
	}
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case string:
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
			if parsed, err := time.Parse(layout, typed); err == nil {
				return parsed, true
			}
		}
	}
	return time.Time{}, false
}

// roundTo rounds a value to the given number of decimal places.
func roundTo(value float64, decimals int) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	factor := math.Pow(10, float64(decimals))
	return math.Round(value*factor) / factor
}
