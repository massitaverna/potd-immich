package immich

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	apiBasePath     = "/api"
	pathVersion     = "/server/version"
	pathMe          = "/users/me"
	pathPeople      = "/people"
	pathSearch      = "/search/metadata"
	pathAlbums      = "/albums"
	pathAlbumAssets = "/albums/%s/assets"
	queryTrue       = "true"
	apiKeyHeader    = "x-api-key"
	contentTypeJSON = "application/json"
	requestTimeout  = 30 * time.Second
	peoplePageSize  = 1000
	searchPageSize  = 1000
	errorBodyLimit  = 512

	AssetTypeImage     = "IMAGE"
	VisibilityTimeline = "timeline"
	bulkErrorDuplicate = "duplicate"
	bulkErrorNotFound  = "not_found"
	supportedMajor     = 3
	minSupportedMinor  = 2
)

type Person struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IsHidden bool   `json:"isHidden"`
}

type Asset struct {
	ID               string `json:"id"`
	OwnerID          string `json:"ownerId"`
	Type             string `json:"type"`
	Visibility       string `json:"visibility"`
	IsTrashed        bool   `json:"isTrashed"`
	OriginalFileName string `json:"originalFileName"`
	LocalDateTime    string `json:"localDateTime"`
}

type Album struct {
	ID         string `json:"id"`
	AlbumName  string `json:"albumName"`
	AssetCount int    `json:"assetCount"`
}

type Version struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
	Patch int `json:"patch"`
}

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Supported reports whether the server has the v3.2 search filter API and is not a future major.
func (v Version) Supported() bool {
	return v.Major == supportedMajor && v.Minor >= minSupportedMinor
}

type IDsFilter struct {
	Any  []string `json:"any,omitempty"`
	None []string `json:"none,omitempty"`
}

type EqFilter struct {
	Eq string `json:"eq"`
}

// NullEqFilter marshals to {"eq": null}, i.e. "column IS NULL".
type NullEqFilter struct {
	Eq *string `json:"eq"`
}

type SearchFilter struct {
	PersonIDs  *IDsFilter    `json:"personIds,omitempty"`
	AlbumIDs   *IDsFilter    `json:"albumIds,omitempty"`
	Type       *EqFilter     `json:"type,omitempty"`
	Visibility *EqFilter     `json:"visibility,omitempty"`
	TrashedAt  *NullEqFilter `json:"trashedAt,omitempty"`
}

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/") + apiBasePath,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: requestTimeout},
	}
}

func (c *Client) ServerVersion(ctx context.Context) (Version, error) {
	var v Version
	err := c.do(ctx, http.MethodGet, pathVersion, nil, nil, &v)
	return v, err
}

func (c *Client) MyUserID(ctx context.Context) (string, error) {
	var me struct {
		ID string `json:"id"`
	}
	err := c.do(ctx, http.MethodGet, pathMe, nil, nil, &me)
	return me.ID, err
}

// People returns every person, including hidden and unnamed ones.
func (c *Client) People(ctx context.Context) ([]Person, error) {
	var all []Person
	for page := 1; ; page++ {
		q := url.Values{
			"withHidden": {queryTrue},
			"size":       {strconv.Itoa(peoplePageSize)},
			"page":       {strconv.Itoa(page)},
		}
		var resp struct {
			People      []Person `json:"people"`
			HasNextPage bool     `json:"hasNextPage"`
		}
		if err := c.do(ctx, http.MethodGet, pathPeople, q, nil, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.People...)
		if !resp.HasNextPage {
			return all, nil
		}
	}
}

func (c *Client) SearchAssets(ctx context.Context, filter SearchFilter) ([]Asset, error) {
	var all []Asset
	var cursor string
	for {
		req := struct {
			Filter SearchFilter `json:"filter"`
			Size   int          `json:"size"`
			Cursor string       `json:"cursor,omitempty"`
		}{filter, searchPageSize, cursor}
		var resp struct {
			Assets struct {
				Items      []Asset `json:"items"`
				NextCursor *string `json:"nextCursor"`
			} `json:"assets"`
		}
		if err := c.do(ctx, http.MethodPost, pathSearch, nil, req, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Assets.Items...)
		if resp.Assets.NextCursor == nil || *resp.Assets.NextCursor == "" {
			return all, nil
		}
		cursor = *resp.Assets.NextCursor
	}
}

// OwnedAlbumsByName returns the caller's own albums whose name matches exactly.
func (c *Client) OwnedAlbumsByName(ctx context.Context, name string) ([]Album, error) {
	var albums []Album
	q := url.Values{"name": {name}, "isOwned": {queryTrue}}
	err := c.do(ctx, http.MethodGet, pathAlbums, q, nil, &albums)
	return albums, err
}

func (c *Client) CreateAlbum(ctx context.Context, name string, assetIDs []string) (Album, error) {
	req := struct {
		AlbumName string   `json:"albumName"`
		AssetIDs  []string `json:"assetIds"`
	}{name, assetIDs}
	var album Album
	err := c.do(ctx, http.MethodPost, pathAlbums, nil, req, &album)
	return album, err
}

func (c *Client) AddAlbumAssets(ctx context.Context, albumID string, assetIDs []string) error {
	return c.bulk(ctx, http.MethodPut, albumID, assetIDs, bulkErrorDuplicate)
}

func (c *Client) RemoveAlbumAssets(ctx context.Context, albumID string, assetIDs []string) error {
	return c.bulk(ctx, http.MethodDelete, albumID, assetIDs, bulkErrorNotFound)
}

func (c *Client) bulk(ctx context.Context, method, albumID string, assetIDs []string, tolerated string) error {
	req := struct {
		IDs []string `json:"ids"`
	}{assetIDs}
	var results []struct {
		ID      string `json:"id"`
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := c.do(ctx, method, fmt.Sprintf(pathAlbumAssets, url.PathEscape(albumID)), nil, req, &results); err != nil {
		return err
	}
	for _, r := range results {
		if !r.Success && r.Error != tolerated {
			return fmt.Errorf("%s album asset %s: %s", method, r.ID, r.Error)
		}
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	req.Header.Set(apiKeyHeader, c.apiKey)
	req.Header.Set("Accept", contentTypeJSON)
	if body != nil {
		req.Header.Set("Content-Type", contentTypeJSON)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodyLimit))
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}
