package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	nexusGraphQLEndpoint  = "https://api-router.nexusmods.com/graphql"
	serverModMetadataFile = ".palserver-launcher.json"
)

type nexusModInfo struct {
	ModID     int
	Version   string
	UpdatedAt string
}

type serverModMetadata struct {
	CatalogID string `json:"catalogId"`
	Version   string `json:"version"`
	UpdatedAt string `json:"updatedAt"`
}

func nexusCatalogModID(entry ServerModCatalogEntry) (int, error) {
	value := strings.TrimPrefix(entry.NexusURL, "https://www.nexusmods.com/palworld/mods/")
	if value == entry.NexusURL || strings.Contains(value, "/") {
		return 0, errors.New("invalid Nexus mod URL")
	}
	return strconv.Atoi(value)
}

func fetchNexusModInfo(client *http.Client, endpoint string, modID int) (nexusModInfo, error) {
	query := `query ModsListing($count: Int = 0, $filter: ModsFilter) {
  mods(count: $count, filter: $filter, viewUserBlockedContent: false) {
    nodes { modId name updatedAt version }
  }
}`
	payload := map[string]any{
		"query": query,
		"variables": map[string]any{
			"count": 5,
			"filter": map[string]any{
				"gameDomainName": []map[string]string{{"op": "EQUALS", "value": "palworld"}},
				"gameId":         []map[string]string{{"op": "EQUALS", "value": "6063"}},
				"modId":          []map[string]string{{"op": "EQUALS", "value": strconv.Itoa(modID)}},
			},
		},
		"operationName": "ModsListing",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nexusModInfo{}, err
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nexusModInfo{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GraphQL-OperationName", "GameModsListing")
	request.Header.Set("User-Agent", "palserver-launcher")
	response, err := client.Do(request)
	if err != nil {
		return nexusModInfo{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nexusModInfo{}, fmt.Errorf("Nexus update check failed: %s", response.Status)
	}
	var result struct {
		Data struct {
			Mods struct {
				Nodes []struct {
					ModID     int    `json:"modId"`
					Name      string `json:"name"`
					UpdatedAt string `json:"updatedAt"`
					Version   string `json:"version"`
				} `json:"nodes"`
			} `json:"mods"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return nexusModInfo{}, err
	}
	if len(result.Errors) > 0 {
		return nexusModInfo{}, errors.New(result.Errors[0].Message)
	}
	for _, node := range result.Data.Mods.Nodes {
		if node.ModID != modID {
			continue
		}
		updated, err := time.Parse(time.RFC3339, node.UpdatedAt)
		if err != nil {
			return nexusModInfo{}, err
		}
		chinaTime := time.FixedZone("CST", 8*60*60)
		return nexusModInfo{ModID: node.ModID, Version: strings.TrimSpace(node.Version), UpdatedAt: updated.In(chinaTime).Format("2006-01-02 15:04")}, nil
	}
	return nexusModInfo{}, errors.New("Nexus mod was not found")
}

func readServerModMetadata(modPath string) (serverModMetadata, error) {
	data, err := os.ReadFile(filepath.Join(modPath, serverModMetadataFile))
	if os.IsNotExist(err) {
		return serverModMetadata{}, nil
	}
	if err != nil {
		return serverModMetadata{}, err
	}
	var metadata serverModMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return serverModMetadata{}, err
	}
	return metadata, nil
}

func writeServerModMetadata(modPath string, metadata serverModMetadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(modPath, serverModMetadataFile), data, 0o600)
}

func applyNexusUpdateInfo(entry ServerModCatalogEntry, metadata serverModMetadata, latest nexusModInfo) ServerModCatalogEntry {
	entry.InstalledVersion = metadata.Version
	entry.InstalledUpdatedAt = metadata.UpdatedAt
	entry.LatestVersion = latest.Version
	entry.LatestUpdatedAt = latest.UpdatedAt
	entry.UpdateCheckError = ""
	entry.UpdateAvailable = false
	if !entry.Installed {
		return entry
	}
	if metadata.Version != "" {
		entry.UpdateAvailable = latest.Version != "" && latest.Version != metadata.Version
		if !entry.UpdateAvailable && metadata.UpdatedAt != "" {
			entry.UpdateAvailable = latest.UpdatedAt > metadata.UpdatedAt
		}
		return entry
	}
	entry.UpdateAvailable = latest.UpdatedAt > entry.UpdatedAt
	return entry
}

func (a *App) rememberNexusModInfo(catalogID string, info nexusModInfo) {
	a.serverModUpdateMu.Lock()
	defer a.serverModUpdateMu.Unlock()
	if a.serverModUpdates == nil {
		a.serverModUpdates = map[string]nexusModInfo{}
	}
	a.serverModUpdates[catalogID] = info
}

func (a *App) cachedNexusModInfo(catalogID string) (nexusModInfo, bool) {
	a.serverModUpdateMu.RLock()
	defer a.serverModUpdateMu.RUnlock()
	info, ok := a.serverModUpdates[catalogID]
	return info, ok
}

func (a *App) CheckServerModUpdates(serverID string) ([]ServerModCatalogEntry, error) {
	entries, err := a.ListServerModCatalog(serverID)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	type checkResult struct {
		index int
		info  nexusModInfo
		err   error
	}
	results := make(chan checkResult, len(entries))
	var wait sync.WaitGroup
	for index, entry := range entries {
		wait.Add(1)
		go func(index int, entry ServerModCatalogEntry) {
			defer wait.Done()
			modID, idErr := nexusCatalogModID(entry)
			if idErr != nil {
				results <- checkResult{index: index, err: idErr}
				return
			}
			info, fetchErr := fetchNexusModInfo(client, nexusGraphQLEndpoint, modID)
			results <- checkResult{index: index, info: info, err: fetchErr}
		}(index, entry)
	}
	wait.Wait()
	close(results)
	for result := range results {
		entry := entries[result.index]
		if result.err != nil {
			entry.UpdateCheckError = result.err.Error()
			entries[result.index] = entry
			continue
		}
		metadata := serverModMetadata{}
		if entry.InstalledPath != "" {
			metadata, _ = readServerModMetadata(entry.InstalledPath)
		}
		a.rememberNexusModInfo(entry.ID, result.info)
		entries[result.index] = applyNexusUpdateInfo(entry, metadata, result.info)
	}
	return entries, nil
}
