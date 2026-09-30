package jellyfin

import (
	"context"
	_ "embed"
	"net/http"
	"strings"
)

//go:embed netflix.css
var netflixCSS string

// The theme sits between these markers in Jellyfin's custom CSS, so turning it
// off leaves any CSS the user wrote themselves in place.
const (
	themeStart = "/* homelab-theme:start */"
	themeEnd   = "/* homelab-theme:end */"
)

type branding struct {
	LoginDisclaimer     string `json:"LoginDisclaimer"`
	CustomCss           string `json:"CustomCss"`
	SplashscreenEnabled bool   `json:"SplashscreenEnabled"`
}

func (c *Client) branding(ctx context.Context) (branding, error) {
	var config branding
	err := c.do(ctx, http.MethodGet, "/System/Configuration/branding", nil, &config)

	return config, err
}

func (c *Client) ThemeEnabled(ctx context.Context) (bool, error) {
	config, err := c.branding(ctx)

	return strings.Contains(config.CustomCss, themeStart), err
}

func (c *Client) SetTheme(ctx context.Context, enabled bool) error {
	config, err := c.branding(ctx)
	if err != nil {
		return err
	}

	config.CustomCss = withTheme(config.CustomCss, enabled)

	return c.do(ctx, http.MethodPost, "/System/Configuration/Branding", config, nil)
}

// withTheme removes our block from css and, when enabled, adds the current version.
func withTheme(css string, enabled bool) string {
	if start := strings.Index(css, themeStart); start >= 0 {
		end := strings.Index(css[start:], themeEnd)
		if end >= 0 {
			css = css[:start] + css[start+end+len(themeEnd):]
		} else {
			css = css[:start]
		}
	}
	css = strings.TrimSpace(css)

	if !enabled {
		return css
	}

	block := themeStart + "\n" + netflixCSS + "\n" + themeEnd
	if css == "" {
		return block
	}

	return css + "\n\n" + block
}
