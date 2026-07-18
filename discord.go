package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func validateDiscordWebhook(value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" {
		return errors.New("Discord webhook must use HTTPS")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "discord.com" && host != "discordapp.com" {
		return errors.New("webhook host must be discord.com")
	}
	if !strings.HasPrefix(parsed.Path, "/api/webhooks/") {
		return errors.New("invalid Discord webhook path")
	}
	return nil
}

func redactDiscordWebhook(value string) string {
	parsed, err := url.Parse(value)
	if err != nil {
		return "[redacted webhook]"
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) >= 4 {
		parts[len(parts)-1] = "***"
		parsed.Path = "/" + strings.Join(parts, "/")
	}
	parsed.RawQuery = ""
	return parsed.String()
}

func discordEventEnabled(events []string, event string) bool {
	for _, item := range events {
		if item == "all" || item == event {
			return true
		}
	}
	return false
}

func nonNilStrings(values []string) []string {
	return append([]string{}, values...)
}

func (a *App) GetDiscordWebhookSettings(serverID string) DiscordWebhookSettings {
	config, ok := a.store.DiscordWebhook(serverID)
	return DiscordWebhookSettings{ServerID: serverID, Enabled: ok && config.Enabled, Configured: ok && config.EncryptedURL != "", Events: nonNilStrings(config.Events)}
}

func (a *App) SaveDiscordWebhook(serverID, webhookURL string, enabled bool, events []string) (DiscordWebhookSettings, error) {
	if _, err := a.store.Find(serverID); err != nil {
		return DiscordWebhookSettings{}, err
	}
	config, _ := a.store.DiscordWebhook(serverID)
	config.ServerID, config.Enabled, config.Events = serverID, enabled, nonNilStrings(events)
	if strings.TrimSpace(webhookURL) != "" {
		if err := validateDiscordWebhook(webhookURL); err != nil {
			return DiscordWebhookSettings{}, err
		}
		encrypted, err := protectSecret(strings.TrimSpace(webhookURL))
		if err != nil {
			return DiscordWebhookSettings{}, err
		}
		config.EncryptedURL = encrypted
	}
	if enabled && config.EncryptedURL == "" {
		return DiscordWebhookSettings{}, errors.New("Discord webhook is not configured")
	}
	if err := a.store.SaveDiscordWebhook(config); err != nil {
		return DiscordWebhookSettings{}, err
	}
	return a.GetDiscordWebhookSettings(serverID), nil
}

func (a *App) TestDiscordWebhook(serverID string) error {
	return a.sendDiscord(serverID, "test", "Palserver Launcher", "Discord notification is configured correctly.")
}

func (a *App) notifyDiscord(serverID, event, title, description string) {
	go func() {
		if err := a.sendDiscord(serverID, event, title, description); err != nil && a.ctx != nil {
			a.emit("discord:error", serverID, event, err.Error())
		}
	}()
}

func (a *App) sendDiscord(serverID, event, title, description string) error {
	config, ok := a.store.DiscordWebhook(serverID)
	if !ok || !config.Enabled || (event != "test" && !discordEventEnabled(config.Events, event)) {
		return nil
	}
	webhookURL, err := unprotectSecret(config.EncryptedURL)
	if err != nil {
		return err
	}
	payload := map[string]any{"username": "Palserver Launcher", "embeds": []map[string]any{{"title": title, "description": description, "timestamp": time.Now().UTC().Format(time.RFC3339), "color": 3118670}}}
	data, _ := json.Marshal(payload)
	request, _ := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Discord webhook returned %s", response.Status)
	}
	return nil
}
