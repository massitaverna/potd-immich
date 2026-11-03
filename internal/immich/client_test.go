package immich

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testKey = "secret"

func TestVersionSupported(t *testing.T) {
	cases := map[Version]bool{
		{3, 1, 9}: false,
		{3, 2, 0}: true,
		{3, 3, 3}: true,
		{4, 0, 0}: false,
		{2, 9, 0}: false,
	}
	for v, want := range cases {
		if v.Supported() != want {
			t.Errorf("%s supported = %v, want %v", v, !want, want)
		}
	}
}

func TestSearchAssetsBodyAndCursorPagination(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search/metadata" || r.Method != http.MethodPost || r.Header.Get(apiKeyHeader) != testKey {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			io.WriteString(w, `{"assets":{"items":[{"id":"a"}],"nextCursor":"c2"}}`)
			return
		}
		io.WriteString(w, `{"assets":{"items":[{"id":"b"}],"nextCursor":null}}`)
	}))
	defer srv.Close()

	filter := SearchFilter{
		PersonIDs:  &IDsFilter{Any: []string{"p1"}, None: []string{"p2"}},
		Type:       &EqFilter{Eq: AssetTypeImage},
		Visibility: &EqFilter{Eq: VisibilityTimeline},
		TrashedAt:  &NullEqFilter{},
	}
	got, err := New(srv.URL+"/", testKey).SearchAssets(context.Background(), filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("assets = %+v", got)
	}
	want := `{"filter":{"personIds":{"any":["p1"],"none":["p2"]},"type":{"eq":"IMAGE"},` +
		`"visibility":{"eq":"timeline"},"trashedAt":{"eq":null}},"size":1000}`
	if bodies[0] != want {
		t.Errorf("first body = %s\nwant          %s", bodies[0], want)
	}
	var second map[string]any
	json.Unmarshal([]byte(bodies[1]), &second)
	if second["cursor"] != "c2" {
		t.Errorf("second request cursor = %v", second["cursor"])
	}
}

func TestPeoplePagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("withHidden") != "true" {
			t.Error("withHidden not set")
		}
		if q.Get("page") == "1" {
			io.WriteString(w, `{"people":[{"id":"1","name":"A"}],"hasNextPage":true,"total":2,"hidden":0}`)
			return
		}
		io.WriteString(w, `{"people":[{"id":"2","name":"","isHidden":true}],"hasNextPage":false,"total":2,"hidden":1}`)
	}))
	defer srv.Close()

	got, err := New(srv.URL, testKey).People(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].ID != "2" || !got[1].IsHidden {
		t.Errorf("people = %+v", got)
	}
}

func TestBulkErrors(t *testing.T) {
	resp := `[{"id":"a","success":true},{"id":"b","success":false,"error":"duplicate"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, resp)
	}))
	defer srv.Close()
	c := New(srv.URL, testKey)

	if err := c.AddAlbumAssets(context.Background(), "alb", []string{"a", "b"}); err != nil {
		t.Errorf("duplicate on add should be tolerated: %v", err)
	}
	resp = `[{"id":"b","success":false,"error":"no_permission"}]`
	if err := c.AddAlbumAssets(context.Background(), "alb", []string{"b"}); err == nil {
		t.Error("expected error for no_permission")
	}
}

func TestHTTPErrorIncludesStatusAndBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Missing required permission: person.read"}`, http.StatusForbidden)
	}))
	defer srv.Close()
	_, err := New(srv.URL, testKey).People(context.Background())
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "person.read") {
		t.Errorf("err = %v", err)
	}
}
