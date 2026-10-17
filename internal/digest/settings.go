package digest

import (
	"encoding/json"
	"strings"

	"github.com/replypen/replypen-cli/internal/client"
)

// Typed reads of one settings-bag key. Each takes the field's effective value (value-or-default), falling
// back to the stored override, so the Effective-then-Value ladder is decoded in one place. A missing or
// mistyped key reads as the zero value.

// SettingBool reads a boolean key.
func SettingBool(settings client.Settings, key string) bool {
	var value bool
	_ = json.Unmarshal(settingValue(settings[key]), &value)
	return value
}

// SettingString reads a string key.
func SettingString(settings client.Settings, key string) string {
	var value string
	_ = json.Unmarshal(settingValue(settings[key]), &value)
	return value
}

// SettingStrings reads a string-list key; never nil.
func SettingStrings(settings client.Settings, key string) []string {
	value := []string{}
	_ = json.Unmarshal(settingValue(settings[key]), &value)
	return value
}

// SettingConfigured reports whether a key holds a meaningful value: true, a non-blank string, or any
// other non-null JSON value.
func SettingConfigured(settings client.Settings, key string) bool {
	raw := settingValue(settings[key])
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return false
	}
	var value interface{}
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.TrimSpace(v) != ""
	default:
		return value != nil
	}
}

func settingValue(field client.SettingField) json.RawMessage {
	if len(field.Effective) > 0 {
		return field.Effective
	}
	return field.Value
}
