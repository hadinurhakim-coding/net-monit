package main

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"
)

type LibreSpeedServer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Server  string `json:"server"`
	DlURL   string `json:"dlURL"`
	UlURL   string `json:"ulURL"`
	PingURL string `json:"pingURL"`
	Country string `json:"country"`
}

const lsListURL = "https://librespeed.org/servers-cli.json"

func normalizeServerURL(u string) string {
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return strings.TrimRight(u, "/")
}

func FetchAndFilterLibreSpeedServers() ([]LibreSpeedServer, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	resp, err := client.Get(lsListURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw []LibreSpeedServer
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	for i := range raw {
		raw[i].Server = normalizeServerURL(raw[i].Server)
	}

	sort.Slice(raw, func(i, j int) bool {
		if raw[i].Country != raw[j].Country {
			return raw[i].Country < raw[j].Country
		}
		return raw[i].Name < raw[j].Name
	})

	return raw, nil
}
