package arr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type bazarr struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// bazarrAPIKey reads auth.apikey from Bazarr's config.yaml, which Bazarr
// writes on first start.
func bazarrAPIKey(configPath string) (string, error) {
	file, err := os.Open(configPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	inAuth := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, " ") {
			inAuth = line == "auth:"
			continue
		}
		if key, found := strings.CutPrefix(line, "  apikey: "); inAuth && found {
			return strings.Trim(key, `'"`), nil
		}
	}

	return "", errors.New("bazarr: no auth.apikey in " + configPath)
}

func (b *bazarr) ready(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+"/api/system/status", nil)
	if err != nil {
		return err
	}
	request.Header.Set("X-API-KEY", b.apiKey)

	response, err := b.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("bazarr: %s", response.Status)
	}

	return nil
}

// saveSettings posts to the same endpoint Bazarr's settings page uses.
func (b *bazarr) saveSettings(ctx context.Context, form url.Values) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/system/settings", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("X-API-KEY", b.apiKey)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := b.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	if response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("bazarr settings: %s %s", response.Status, message)
	}

	return nil
}

type bazarrProfileItem struct {
	ID               int    `json:"id"`
	Language         string `json:"language"`
	AudioExclude     string `json:"audio_exclude"`
	AudioOnlyInclude string `json:"audio_only_include"`
	HI               string `json:"hi"`
	Forced           string `json:"forced"`
}

type bazarrProfile struct {
	ProfileID      int                 `json:"profileId"`
	Name           string              `json:"name"`
	Items          []bazarrProfileItem `json:"items"`
	Cutoff         *int                `json:"cutoff"`
	MustContain    []string            `json:"mustContain"`
	MustNotContain []string            `json:"mustNotContain"`
	OriginalFormat bool                `json:"originalFormat"`
	Tag            *string             `json:"tag"`
}

func bazarrLanguageProfile(languages []string) (string, error) {
	profile := bazarrProfile{ProfileID: 1, Name: "Default", MustContain: []string{}, MustNotContain: []string{}}
	for index, language := range languages {
		profile.Items = append(profile.Items, bazarrProfileItem{
			ID: index + 1, Language: language,
			AudioExclude: "False", AudioOnlyInclude: "False", HI: "False", Forced: "False",
		})
	}

	data, err := json.Marshal([]bazarrProfile{profile})

	return string(data), err
}
