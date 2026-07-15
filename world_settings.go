package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var quotedWorldSettings = map[string]bool{
	"ServerName": true, "ServerDescription": true, "AdminPassword": true,
	"ServerPassword": true, "PublicIP": true, "Region": true,
	"BanListURL": true, "CustomPalDeathDropItemId": true, "RandomizerSeed": true,
	"AdditionalDropItemWhenPlayerKillingInPvPMode": true,
}

func parseWorldSettingValues(content string) map[string]string {
	values := map[string]string{}
	start, end, err := findOptionSettingsBounds(content)
	if err != nil {
		return values
	}
	for _, setting := range scanWorldSettings(content, start, end) {
		value := strings.TrimSpace(content[setting.valueStart:setting.valueEnd])
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = strings.TrimSuffix(strings.TrimPrefix(value, `"`), `"`)
			value = strings.ReplaceAll(value, `\"`, `"`)
		}
		values[setting.key] = value
	}
	return values
}

func mergeMissingWorldSettingDefaults(values, defaults map[string]string) map[string]string {
	for key, value := range defaults {
		if _, exists := values[key]; !exists {
			values[key] = value
		}
	}
	return values
}

func findOptionSettingsBounds(content string) (int, int, error) {
	marker := "OptionSettings=("
	markerStart := strings.Index(content, marker)
	if markerStart < 0 {
		return 0, 0, errors.New("OptionSettings was not found")
	}
	start := markerStart + len(marker)
	inQuote := false
	depth := 1
	escaped := false
	for index := start; index < len(content); index++ {
		character := content[index]
		if escaped {
			escaped = false
			continue
		}
		if character == '\\' && inQuote {
			escaped = true
			continue
		}
		if character == '"' {
			inQuote = !inQuote
			continue
		}
		if !inQuote {
			switch character {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return start, index, nil
				}
			}
		}
	}
	return 0, 0, errors.New("OptionSettings is incomplete")
}

type worldSettingSpan struct {
	key                  string
	fullStart, fullEnd   int
	valueStart, valueEnd int
}

func scanWorldSettings(content string, start, end int) []worldSettingSpan {
	result := make([]worldSettingSpan, 0)
	for cursor := start; cursor < end; {
		for cursor < end && (content[cursor] == ',' || content[cursor] == ' ' || content[cursor] == '\r' || content[cursor] == '\n' || content[cursor] == '\t') {
			cursor++
		}
		keyStart := cursor
		for cursor < end && ((content[cursor] >= 'A' && content[cursor] <= 'Z') || (content[cursor] >= 'a' && content[cursor] <= 'z') || (cursor > keyStart && content[cursor] >= '0' && content[cursor] <= '9') || content[cursor] == '_') {
			cursor++
		}
		if keyStart == cursor || cursor >= end || content[cursor] != '=' {
			cursor++
			continue
		}
		key := content[keyStart:cursor]
		cursor++
		valueStart := cursor
		inQuote, escaped, depth := false, false, 0
		for cursor < end {
			character := content[cursor]
			if escaped {
				escaped = false
				cursor++
				continue
			}
			if character == '\\' && inQuote {
				escaped = true
				cursor++
				continue
			}
			if character == '"' {
				inQuote = !inQuote
				cursor++
				continue
			}
			if !inQuote {
				if character == '(' {
					depth++
				} else if character == ')' && depth > 0 {
					depth--
				} else if character == ',' && depth == 0 {
					break
				}
			}
			cursor++
		}
		result = append(result, worldSettingSpan{key: key, fullStart: keyStart, fullEnd: cursor, valueStart: valueStart, valueEnd: cursor})
		if cursor < end && content[cursor] == ',' {
			cursor++
		}
	}
	return result
}

func formatWorldSettingValue(key, value, previous string) string {
	value = strings.TrimSpace(value)
	quoted := quotedWorldSettings[key] || strings.HasPrefix(strings.TrimSpace(previous), `"`)
	if quoted {
		value = strings.Trim(value, `"`)
		value = strings.ReplaceAll(value, `"`, `\"`)
		return `"` + value + `"`
	}
	return value
}

func mergeWorldSettingValues(content string, updates map[string]string) (string, error) {
	valueStart, optionEnd, err := findOptionSettingsBounds(content)
	if err != nil {
		return "", err
	}
	settings := scanWorldSettings(content, valueStart, optionEnd)
	var builder strings.Builder
	builder.WriteString(content[:valueStart])
	cursor := valueStart
	seen := map[string]bool{}
	for _, setting := range settings {
		fullStart, fullEnd := setting.fullStart, setting.fullEnd
		key := setting.key
		previous := content[setting.valueStart:setting.valueEnd]
		builder.WriteString(content[cursor:fullStart])
		if value, ok := updates[key]; ok {
			builder.WriteString(key + "=" + formatWorldSettingValue(key, value, previous))
			seen[key] = true
		} else {
			builder.WriteString(content[fullStart:fullEnd])
		}
		cursor = fullEnd
	}
	builder.WriteString(content[cursor:optionEnd])
	missing := make([]string, 0)
	for key := range updates {
		if !seen[key] {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	for _, key := range missing {
		current := builder.String()
		if !strings.HasSuffix(current, "(") && !strings.HasSuffix(current, ",") {
			builder.WriteByte(',')
		}
		builder.WriteString(key + "=" + formatWorldSettingValue(key, updates[key], ""))
	}
	builder.WriteString(content[optionEnd:])
	return builder.String(), nil
}

func syncInstanceWorldSettings(instance ServerInstance) error {
	path, err := worldSettingsPath(instance)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	content, err := mergeWorldSettingValues(string(data), map[string]string{
		"ServerName": instance.Name, "AdminPassword": instance.AdminPassword, "ServerPassword": instance.ServerPassword,
		"PublicIP": instance.PublicIP, "PublicPort": strconv.Itoa(instance.PublicPort), "RCONPort": strconv.Itoa(instance.RCONPort),
		"RESTAPIPort": strconv.Itoa(instance.RESTPort), "RCONEnabled": "True", "RESTAPIEnabled": "True", "bIsMultiplay": "True",
	})
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func (a *App) GetWorldSettingsValues(id string) (map[string]string, error) {
	instance, err := a.store.Find(id)
	if err != nil {
		return nil, err
	}
	content, err := a.ReadWorldSettings(id)
	if err != nil {
		return nil, err
	}
	values := parseWorldSettingValues(content)
	if legacyRandomizer, ok := map[string]string{"1": "Region", "2": "All"}[values["RandomizerType"]]; ok {
		values["RandomizerType"] = legacyRandomizer
	}
	if data, readErr := os.ReadFile(filepath.Join(instance.RootPath, "DefaultPalWorldSettings.ini")); readErr == nil {
		values = mergeMissingWorldSettingDefaults(values, parseWorldSettingValues(string(data)))
	}
	return values, nil
}

func (a *App) SaveWorldSettingsValues(id string, updates map[string]string) error {
	instance, err := a.store.Find(id)
	if err != nil {
		return err
	}
	status, _ := serverStatus(instance)
	if status.Running {
		return errors.New("stop the server before changing world settings")
	}
	if err := validateOfficialWorldSettings(updates); err != nil {
		return err
	}
	path, err := worldSettingsPath(instance)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content, err := mergeWorldSettingValues(string(data), updates)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return err
	}
	instanceChanged := false
	for key, target := range map[string]*string{"ServerName": &instance.Name, "PublicIP": &instance.PublicIP, "AdminPassword": &instance.AdminPassword, "ServerPassword": &instance.ServerPassword} {
		if value, ok := updates[key]; ok {
			*target = value
			instanceChanged = true
		}
	}
	for key, target := range map[string]*int{"PublicPort": &instance.PublicPort, "RCONPort": &instance.RCONPort, "RESTAPIPort": &instance.RESTPort} {
		if value, ok := updates[key]; ok {
			parsed, parseErr := strconv.Atoi(strings.TrimSpace(value))
			if parseErr == nil && parsed > 0 && parsed <= 65535 {
				*target = parsed
				instanceChanged = true
			}
		}
	}
	if instanceChanged {
		_, err = a.store.Upsert(instance)
	}
	return err
}
