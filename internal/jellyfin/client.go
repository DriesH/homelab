// Package jellyfin talks to the Jellyfin server: now playing, library stats and the theme.
package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", `MediaBrowser Token="`+c.apiKey+`"`)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("jellyfin: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("jellyfin: the API key was not accepted")
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("jellyfin %s %s: %s", method, path, response.Status)
	}
	if out == nil {
		return nil
	}

	return json.NewDecoder(response.Body).Decode(out)
}

type SystemInfo struct {
	ServerName string `json:"ServerName"`
	Version    string `json:"Version"`
}

// Info needs a valid API key, so it also checks the key.
func (c *Client) Info(ctx context.Context) (SystemInfo, error) {
	var info SystemInfo
	err := c.do(ctx, http.MethodGet, "/System/Info", nil, &info)

	return info, err
}

type Counts struct {
	MovieCount   int `json:"MovieCount"`
	SeriesCount  int `json:"SeriesCount"`
	EpisodeCount int `json:"EpisodeCount"`
}

func (c *Client) Counts(ctx context.Context) (Counts, error) {
	var counts Counts
	err := c.do(ctx, http.MethodGet, "/Items/Counts", nil, &counts)

	return counts, err
}

type Item struct {
	ID             string `json:"Id"`
	Name           string `json:"Name"`
	Type           string `json:"Type"`
	SeriesName     string `json:"SeriesName"`
	SeasonNumber   int    `json:"ParentIndexNumber"`
	EpisodeNumber  int    `json:"IndexNumber"`
	ProductionYear int    `json:"ProductionYear"`
	RunTimeTicks   int64  `json:"RunTimeTicks"`
	SeriesID       string `json:"SeriesId"`
}

func (c *Client) RecentlyAdded(ctx context.Context, limit int) ([]Item, error) {
	query := url.Values{
		"Recursive":        {"true"},
		"IncludeItemTypes": {"Movie,Series"},
		"SortBy":           {"DateCreated"},
		"SortOrder":        {"Descending"},
		"Fields":           {"ProductionYear"},
		"Limit":            {fmt.Sprint(limit)},
	}

	var result struct {
		Items []Item `json:"Items"`
	}
	err := c.do(ctx, http.MethodGet, "/Items?"+query.Encode(), nil, &result)

	return result.Items, err
}

type Session struct {
	ID             string `json:"Id"`
	UserName       string `json:"UserName"`
	Client         string `json:"Client"`
	DeviceName     string `json:"DeviceName"`
	NowPlayingItem *Item  `json:"NowPlayingItem"`
	PlayState      struct {
		PositionTicks int64  `json:"PositionTicks"`
		IsPaused      bool   `json:"IsPaused"`
		PlayMethod    string `json:"PlayMethod"`
	} `json:"PlayState"`
	TranscodingInfo *struct {
		VideoCodec       string   `json:"VideoCodec"`
		AudioCodec       string   `json:"AudioCodec"`
		IsVideoDirect    bool     `json:"IsVideoDirect"`
		IsAudioDirect    bool     `json:"IsAudioDirect"`
		Bitrate          int64    `json:"Bitrate"`
		TranscodeReasons []string `json:"TranscodeReasons"`
	} `json:"TranscodingInfo"`
}

// Playing returns the sessions that are playing something right now.
func (c *Client) Playing(ctx context.Context) ([]Session, error) {
	var sessions []Session
	if err := c.do(ctx, http.MethodGet, "/Sessions?activeWithinSeconds=960", nil, &sessions); err != nil {
		return nil, err
	}

	playing := []Session{}
	for _, session := range sessions {
		if session.NowPlayingItem != nil {
			playing = append(playing, session)
		}
	}

	return playing, nil
}

// Image streams an item image. Jellyfin serves images without a login.
func (c *Client) Image(ctx context.Context, itemID, imageType string, maxWidth int) (*http.Response, error) {
	path := fmt.Sprintf("%s/Items/%s/Images/%s?maxWidth=%d&quality=85", c.baseURL, url.PathEscape(itemID), url.PathEscape(imageType), maxWidth)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	return c.http.Do(request)
}
