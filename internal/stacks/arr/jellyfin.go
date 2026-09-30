package arr

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

func newJellyfin(baseURL, apiKey string, client *http.Client) *apiClient {
	return &apiClient{
		name:    "jellyfin",
		baseURL: strings.TrimRight(baseURL, "/"),
		headers: map[string]string{"Authorization": `MediaBrowser Token="` + apiKey + `"`},
		http:    client,
	}
}

type jellyfinLibrary struct {
	name           string
	collectionType string
	path           string
}

// ensureJellyfinLibrary adds a library unless one already uses the path.
func ensureJellyfinLibrary(ctx context.Context, jellyfin *apiClient, library jellyfinLibrary) error {
	var folders []struct {
		Locations []string `json:"Locations"`
	}
	if err := jellyfin.do(ctx, http.MethodGet, "/Library/VirtualFolders", nil, &folders); err != nil {
		return err
	}

	for _, folder := range folders {
		if slices.Contains(folder.Locations, library.path) {
			return nil
		}
	}

	query := url.Values{
		"name":           {library.name},
		"collectionType": {library.collectionType},
		"refreshLibrary": {"true"},
	}
	body := map[string]any{
		"LibraryOptions": map[string]any{
			"PathInfos": []map[string]string{{"Path": library.path}},
		},
	}

	return jellyfin.do(ctx, http.MethodPost, "/Library/VirtualFolders?"+query.Encode(), body, nil)
}
