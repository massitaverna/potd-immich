package potd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"

	"github.com/massitaverna/potd-immich/internal/immich"
)

const (
	me      = "user-me"
	partner = "user-partner"
	album   = "Photo of the day"
)

var errEmptyAny = errors.New("400 Bad Request: personIds.any must contain at least 1 element")

var errAddFailed = errors.New("500 Internal Server Error")

type fakeAPI struct {
	people  []immich.Person
	assets  map[string][]immich.Asset // person ID -> assets with that person
	albums  []immich.Album
	inAlbum map[string][]string // album ID -> asset IDs
	failAdd bool                // AddAlbumAssets fails without changing anything
	// t, when set, is failed whenever a removal leaves an album empty:
	// the widget must never show an empty album, not even between two API calls.
	t testing.TB
}

func (f *fakeAPI) ServerVersion(context.Context) (immich.Version, error) {
	return immich.Version{Major: 3, Minor: 3, Patch: 3}, nil
}

func (f *fakeAPI) MyUserID(context.Context) (string, error) { return me, nil }

func (f *fakeAPI) People(context.Context) ([]immich.Person, error) { return f.people, nil }

// SearchAssets emulates the server: personIds any/none for candidates, albumIds any for album contents.
// Like Immich, it rejects an empty personIds.any (the OpenAPI spec requires minItems: 1).
func (f *fakeAPI) SearchAssets(_ context.Context, filter immich.SearchFilter) ([]immich.Asset, error) {
	if filter.PersonIDs != nil && len(filter.PersonIDs.Any) == 0 {
		return nil, errEmptyAny
	}
	if filter.AlbumIDs != nil {
		var out []immich.Asset
		for _, id := range f.inAlbum[filter.AlbumIDs.Any[0]] {
			out = append(out, immich.Asset{ID: id})
		}
		return out, nil
	}
	excluded := map[string]bool{}
	for _, p := range filter.PersonIDs.None {
		for _, a := range f.assets[p] {
			excluded[a.ID] = true
		}
	}
	seen := map[string]bool{}
	var out []immich.Asset
	for _, p := range filter.PersonIDs.Any {
		for _, a := range f.assets[p] {
			if !excluded[a.ID] && !seen[a.ID] {
				seen[a.ID] = true
				out = append(out, a)
			}
		}
	}
	return out, nil
}

func (f *fakeAPI) OwnedAlbumsByName(_ context.Context, name string) ([]immich.Album, error) {
	return f.albums, nil
}

func (f *fakeAPI) CreateAlbum(_ context.Context, name string, ids []string) (immich.Album, error) {
	created := immich.Album{ID: "new-album", AlbumName: name}
	f.albums = append(f.albums, created)
	if f.inAlbum == nil {
		f.inAlbum = map[string][]string{}
	}
	f.inAlbum[created.ID] = slices.Clone(ids)
	return created, nil
}

func (f *fakeAPI) AddAlbumAssets(_ context.Context, albumID string, ids []string) error {
	if f.failAdd {
		return errAddFailed
	}
	for _, id := range ids {
		if !slices.Contains(f.inAlbum[albumID], id) {
			f.inAlbum[albumID] = append(f.inAlbum[albumID], id)
		}
	}
	return nil
}

func (f *fakeAPI) RemoveAlbumAssets(_ context.Context, albumID string, ids []string) error {
	f.inAlbum[albumID] = slices.DeleteFunc(f.inAlbum[albumID], func(id string) bool { return slices.Contains(ids, id) })
	if f.t != nil && len(f.inAlbum[albumID]) == 0 {
		f.t.Errorf("album %s became empty after removing %v", albumID, ids)
	}
	return nil
}

func img(id string) immich.Asset {
	return immich.Asset{ID: id, OwnerID: me, Type: immich.AssetTypeImage, Visibility: immich.VisibilityTimeline}
}

func ids(as []immich.Asset) []string {
	out := []string{}
	for _, a := range as {
		out = append(out, a.ID)
	}
	slices.Sort(out)
	return out
}

func newRunner(api API) Runner {
	return Runner{API: api, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Rand: func(int) int { return 0 }}
}

func TestCandidatesUnionMinusBlocked(t *testing.T) {
	api := &fakeAPI{
		people: []immich.Person{
			{ID: "alice", Name: "Alice"},
			{ID: "bob", Name: "Bob"},
			{ID: "eve", Name: "Eve"},
			{ID: "nameless"},
			{ID: "hidden", Name: "Hidden", IsHidden: true},
		},
		assets: map[string][]immich.Asset{
			"alice":    {img("a1"), img("ab")},
			"bob":      {img("ab"), img("b1"), img("be")},
			"eve":      {img("be"), img("e1")},
			"nameless": {img("n1")},
			"hidden":   {img("h1")},
		},
	}

	got, err := newRunner(api).Candidates(context.Background(), []string{"eve"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"a1", "ab", "b1"}
	if !slices.Equal(ids(got), want) {
		t.Errorf("candidates = %v, want %v", ids(got), want)
	}
}

func TestCandidatesBlockedHiddenPersonStillExcluded(t *testing.T) {
	api := &fakeAPI{
		people: []immich.Person{{ID: "alice", Name: "Alice"}, {ID: "eve", Name: "Eve", IsHidden: true}},
		assets: map[string][]immich.Asset{"alice": {img("a1"), img("ae")}, "eve": {img("ae")}},
	}
	got, _ := newRunner(api).Candidates(context.Background(), []string{"eve"})
	if !slices.Equal(ids(got), []string{"a1"}) {
		t.Errorf("candidates = %v, want [a1]", ids(got))
	}
}

func TestCandidatesDropsForeignAndUnexpectedAssets(t *testing.T) {
	foreign := img("partner")
	foreign.OwnerID = partner
	trashed := img("trashed")
	trashed.IsTrashed = true
	archived := img("archived")
	archived.Visibility = "archive"
	video := img("video")
	video.Type = "VIDEO"
	api := &fakeAPI{
		people: []immich.Person{{ID: "alice", Name: "Alice"}},
		assets: map[string][]immich.Asset{"alice": {img("ok"), foreign, trashed, archived, video}},
	}
	got, _ := newRunner(api).Candidates(context.Background(), nil)
	if !slices.Equal(ids(got), []string{"ok"}) {
		t.Errorf("candidates = %v, want [ok]", ids(got))
	}
}

func TestCandidatesNoAllowedPeople(t *testing.T) {
	api := &fakeAPI{people: []immich.Person{{ID: "eve", Name: "Eve"}, {ID: "nameless"}}}
	got, err := newRunner(api).Candidates(context.Background(), []string{"eve"})
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, err %v; want no candidates and no error", got, err)
	}
}

func TestCandidateFilter(t *testing.T) {
	const asset = `"type":{"eq":"IMAGE"},"visibility":{"eq":"timeline"},"trashedAt":{"eq":null}`
	cases := []struct {
		name    string
		blocked []string
		want    string
	}{
		{"with blocklist", []string{"eve"}, `{"personIds":{"any":["alice","bob"],"none":["eve"]},` + asset + `}`},
		{"nil blocklist omits none", nil, `{"personIds":{"any":["alice","bob"]},` + asset + `}`},
		{"empty blocklist omits none", []string{}, `{"personIds":{"any":["alice","bob"]},` + asset + `}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := json.Marshal(CandidateFilter([]string{"alice", "bob"}, c.blocked))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("filter JSON:\n got  %s\n want %s", got, c.want)
			}
		})
	}
}

func TestPickAvoidsRecent(t *testing.T) {
	cands := []immich.Asset{img("a"), img("b"), img("c")}
	recent := map[string]bool{"a": true, "c": true}

	for range 10 {
		got, fallback, err := Pick(cands, recent, func(n int) int { return n - 1 })
		if err != nil || fallback || got.ID != "b" {
			t.Fatalf("got %s fallback=%v err=%v, want b", got.ID, fallback, err)
		}
	}
}

func TestPickFallsBackWhenAllRecent(t *testing.T) {
	cands := []immich.Asset{img("a"), img("b")}
	recent := map[string]bool{"a": true, "b": true}

	got, fallback, err := Pick(cands, recent, func(n int) int { return 1 })
	if err != nil || !fallback || got.ID != "b" {
		t.Errorf("got %s fallback=%v err=%v", got.ID, fallback, err)
	}
}

func TestPickEmpty(t *testing.T) {
	if _, _, err := Pick(nil, nil, func(int) int { return 0 }); !errors.Is(err, ErrNoCandidates) {
		t.Errorf("err = %v, want ErrNoCandidates", err)
	}
}

func TestSetAlbumNeverLeavesAlbumEmpty(t *testing.T) {
	api := &fakeAPI{
		albums:  []immich.Album{{ID: "alb", AlbumName: album}},
		inAlbum: map[string][]string{"alb": {"old1", "old2"}},
		t:       t,
	}
	if err := newRunner(api).SetAlbum(context.Background(), album, "new", false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(api.inAlbum["alb"], []string{"new"}) {
		t.Errorf("album = %v, want [new]", api.inAlbum["alb"])
	}
}

func TestSetAlbumKeepsOldPhotosWhenAddFails(t *testing.T) {
	api := &fakeAPI{
		albums:  []immich.Album{{ID: "alb", AlbumName: album}},
		inAlbum: map[string][]string{"alb": {"old1", "old2"}},
		failAdd: true,
	}
	if err := newRunner(api).SetAlbum(context.Background(), album, "new", false); !errors.Is(err, errAddFailed) {
		t.Errorf("err = %v, want the add failure", err)
	}
	if !slices.Equal(api.inAlbum["alb"], []string{"old1", "old2"}) {
		t.Errorf("album = %v, want the old photos untouched", api.inAlbum["alb"])
	}
}

func TestSetAlbumChosenAlreadyPresent(t *testing.T) {
	api := &fakeAPI{
		albums:  []immich.Album{{ID: "alb", AlbumName: album}},
		inAlbum: map[string][]string{"alb": {"keep", "old"}},
		t:       t,
	}
	if err := newRunner(api).SetAlbum(context.Background(), album, "keep", false); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(api.inAlbum["alb"], []string{"keep"}) {
		t.Errorf("album = %v, want [keep]", api.inAlbum["alb"])
	}
}

func TestSetAlbumCreatesMissing(t *testing.T) {
	api := &fakeAPI{albums: []immich.Album{{ID: "other", AlbumName: album + " 2"}}, t: t}
	if err := newRunner(api).SetAlbum(context.Background(), album, "new", false); err != nil {
		t.Fatal(err)
	}
	var found []string
	for _, a := range api.albums {
		if a.AlbumName == album {
			found = append(found, a.ID)
		}
	}
	if len(found) != 1 || !slices.Equal(api.inAlbum[found[0]], []string{"new"}) {
		t.Errorf("albums named %q = %v, contents %v; want one album holding [new]", album, found, api.inAlbum)
	}
}

func TestSetAlbumAmbiguous(t *testing.T) {
	api := &fakeAPI{albums: []immich.Album{{ID: "a", AlbumName: album}, {ID: "b", AlbumName: album}}}
	if err := newRunner(api).SetAlbum(context.Background(), album, "new", false); err == nil {
		t.Error("expected error for duplicate album names")
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	for _, albums := range [][]immich.Album{nil, {{ID: "alb", AlbumName: album}}} {
		api := &fakeAPI{albums: slices.Clone(albums), inAlbum: map[string][]string{"alb": {"old"}}}
		if err := newRunner(api).SetAlbum(context.Background(), album, "new", true); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(api.albums, albums) || len(api.inAlbum) != 1 || !slices.Equal(api.inAlbum["alb"], []string{"old"}) {
			t.Errorf("dry run changed state: albums %v, contents %v", api.albums, api.inAlbum)
		}
	}
}

func TestRunEndToEnd(t *testing.T) {
	api := &fakeAPI{
		people:  []immich.Person{{ID: "alice", Name: "Alice"}},
		assets:  map[string][]immich.Asset{"alice": {img("a1"), img("a2")}},
		albums:  []immich.Album{{ID: "alb", AlbumName: album}},
		inAlbum: map[string][]string{"alb": {"a1"}},
		t:       t,
	}
	got, err := newRunner(api).Run(context.Background(), Options{AlbumName: album, Recent: map[string]bool{"a1": true}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "a2" || !slices.Equal(api.inAlbum["alb"], []string{"a2"}) {
		t.Errorf("chose %s, album %v; want a2 as the only photo", got.ID, api.inAlbum["alb"])
	}
}
