package arr

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// servarr talks to Radarr, Sonarr and Prowlarr, which share one API design.
type servarr struct {
	apiClient
}

func newServarr(name, url, apiVersion, apiKey string, client *http.Client) *servarr {
	return &servarr{apiClient{
		name:    name,
		baseURL: strings.TrimRight(url, "/") + "/api/" + apiVersion,
		headers: map[string]string{"X-Api-Key": apiKey},
		http:    client,
	}}
}

func (s *servarr) ready(ctx context.Context) error {
	return s.do(ctx, http.MethodGet, "/system/status", nil, nil)
}

func (s *servarr) setLogin(ctx context.Context, username, password string) error {
	var host map[string]any
	if err := s.do(ctx, http.MethodGet, "/config/host", nil, &host); err != nil {
		return err
	}

	host["username"] = username
	host["password"] = password
	host["passwordConfirmation"] = password

	return s.do(ctx, http.MethodPut, fmt.Sprintf("/config/host/%v", host["id"]), host, nil)
}

func (s *servarr) ensureRootFolder(ctx context.Context, path string) error {
	var folders []struct {
		Path string `json:"path"`
	}
	if err := s.do(ctx, http.MethodGet, "/rootfolder", nil, &folders); err != nil {
		return err
	}

	for _, folder := range folders {
		if strings.TrimRight(folder.Path, "/") == path {
			return nil
		}
	}

	return s.do(ctx, http.MethodPost, "/rootfolder", map[string]string{"path": path}, nil)
}

func (s *servarr) ensureTag(ctx context.Context, label string) (int, error) {
	var tags []struct {
		ID    int    `json:"id"`
		Label string `json:"label"`
	}
	if err := s.do(ctx, http.MethodGet, "/tag", nil, &tags); err != nil {
		return 0, err
	}

	for _, tag := range tags {
		if tag.Label == label {
			return tag.ID, nil
		}
	}

	var created struct {
		ID int `json:"id"`
	}
	err := s.do(ctx, http.MethodPost, "/tag", map[string]string{"label": label}, &created)

	return created.ID, err
}

// provider is an entry in a schema-based list like /downloadclient,
// /notification or /applications.
type provider struct {
	resource       string
	implementation string
	name           string
	fields         map[string]any
	// settings are top-level properties. Only those the schema knows are set.
	settings map[string]any
}

// ensure creates the provider, or updates it when one with the same name exists.
// It starts from the app's own schema, so we only fill in what we care about.
func (s *servarr) ensure(ctx context.Context, p provider) error {
	var existing []map[string]any
	if err := s.do(ctx, http.MethodGet, "/"+p.resource, nil, &existing); err != nil {
		return err
	}

	for _, item := range existing {
		if item["name"] == p.name {
			apply(item, p)
			return s.do(ctx, http.MethodPut, fmt.Sprintf("/%s/%v", p.resource, item["id"]), item, nil)
		}
	}

	var schemas []map[string]any
	if err := s.do(ctx, http.MethodGet, "/"+p.resource+"/schema", nil, &schemas); err != nil {
		return err
	}

	for _, schema := range schemas {
		if schema["implementation"] == p.implementation {
			schema["name"] = p.name
			apply(schema, p)
			return s.do(ctx, http.MethodPost, "/"+p.resource, schema, nil)
		}
	}

	return fmt.Errorf("%s: no %s schema named %s", s.name, p.resource, p.implementation)
}

func apply(item map[string]any, p provider) {
	for key, value := range p.settings {
		if _, known := item[key]; known {
			item[key] = value
		}
	}

	fields, _ := item["fields"].([]any)
	for _, raw := range fields {
		field, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if value, ok := p.fields[field["name"].(string)]; ok {
			field["value"] = value
		}
	}
}
