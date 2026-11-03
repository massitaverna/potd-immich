package blocklist

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/massitaverna/potd-immich/internal/immich"
)

var (
	quiet = slog.New(slog.NewTextHandler(io.Discard, nil))
	// fakeNow is 06:00 UTC, deliberately in another zone so tests can check that last_synced is stored in UTC.
	fakeNow = time.Date(2026, 10, 17, 8, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
)

func answer(yes ...string) (Confirm, *[]string) {
	var asked []string
	return func(label string) (bool, error) {
		asked = append(asked, label)
		return slices.Contains(yes, label), nil
	}, &asked
}

func names(ps []Person) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func TestSync(t *testing.T) {
	old := File{Blocklist: Blocklist{
		Blocked: []Person{{ID: "z", Name: "Zed"}},
		Catalog: []Person{{ID: "a", Name: "Anna"}, {ID: "z", Name: "Zed"}},
	}}
	people := []immich.Person{
		{ID: "a", Name: "Anna"},
		{ID: "z", Name: "Zed"},
		{ID: "c", Name: "carl"},
		{ID: "b", Name: "Bea"},
		{ID: "u"},
		{ID: "h", Name: "Hidden", IsHidden: true},
	}
	confirm, asked := answer("Bea")
	got, err := Sync(old, people, confirm, quiet, fakeNow)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*asked, []string{"Bea", "carl"}) {
		t.Errorf("asked = %v", *asked)
	}
	if !slices.Equal(names(got.Blocklist.Blocked), []string{"Bea", "Zed"}) {
		t.Errorf("blocked = %v", names(got.Blocklist.Blocked))
	}
	if !slices.Equal(names(got.Blocklist.Catalog), []string{"Anna", "Bea", "carl", "Zed"}) {
		t.Errorf("catalog = %v", names(got.Blocklist.Catalog))
	}
	if ls := got.Blocklist.LastSynced; !ls.Equal(fakeNow) || ls.Location() != time.UTC {
		t.Errorf("last_synced = %v, want %v", ls, fakeNow.UTC())
	}
}

func TestSyncKeepsHiddenBlockedAndDropsDeleted(t *testing.T) {
	old := File{Blocklist: Blocklist{Blocked: []Person{
		{ID: "h", Name: "Hidden now"},
		{ID: "r", Name: "Old name"},
		{ID: "gone", Name: "Deleted"},
	}}}
	people := []immich.Person{
		{ID: "h", Name: "Hidden now", IsHidden: true},
		{ID: "r", Name: "New name"},
	}
	confirm, _ := answer()
	got, err := Sync(old, people, confirm, quiet, fakeNow)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names(got.Blocklist.Blocked), []string{"Hidden now", "New name"}) {
		t.Errorf("blocked = %v", names(got.Blocklist.Blocked))
	}
}

func TestSyncUnhiddenBlockedPersonIsNotAskedAgain(t *testing.T) {
	// Bea was hidden at the last sync, so she is blocked but missing from the old catalog.
	old := File{Blocklist: Blocklist{
		Blocked: []Person{{ID: "b", Name: "Bea"}},
		Catalog: []Person{{ID: "a", Name: "Anna"}},
	}}
	people := []immich.Person{{ID: "a", Name: "Anna"}, {ID: "b", Name: "Bea"}}
	confirm, asked := answer()
	got, err := Sync(old, people, confirm, quiet, fakeNow)

	if err != nil {
		t.Fatal(err)
	}
	if len(*asked) != 0 {
		t.Errorf("asked = %v, want nobody", *asked)
	}
	if !slices.Equal(names(got.Blocklist.Blocked), []string{"Bea"}) {
		t.Errorf("blocked = %v, want [Bea] exactly once", names(got.Blocklist.Blocked))
	}
	if !slices.Equal(names(got.Blocklist.Catalog), []string{"Anna", "Bea"}) {
		t.Errorf("catalog = %v", names(got.Blocklist.Catalog))
	}
}

func TestSyncDeduplicatesPeopleFromAPI(t *testing.T) {
	people := []immich.Person{{ID: "m", Name: "Max"}, {ID: "a", Name: "Anna"}, {ID: "m", Name: "Max"}}
	confirm, asked := answer("Max")
	got, err := Sync(File{}, people, confirm, quiet, fakeNow)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*asked, []string{"Anna", "Max"}) {
		t.Errorf("asked = %v, want [Anna Max] (once each, no ID suffix)", *asked)
	}
	if !slices.Equal(names(got.Blocklist.Catalog), []string{"Anna", "Max"}) {
		t.Errorf("catalog = %v", names(got.Blocklist.Catalog))
	}
	if !slices.Equal(names(got.Blocklist.Blocked), []string{"Max"}) {
		t.Errorf("blocked = %v", names(got.Blocklist.Blocked))
	}
}

func TestSyncDisambiguatesDuplicateNames(t *testing.T) {
	people := []immich.Person{{ID: "1", Name: "Max"}, {ID: "2", Name: "Max"}}
	confirm, asked := answer()
	if _, err := Sync(File{}, people, confirm, quiet, fakeNow); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(*asked, []string{"Max (1)", "Max (2)"}) {
		t.Errorf("asked = %v", *asked)
	}
}

var savedFile = File{Blocklist: Blocklist{
	LastSynced: fakeNow.UTC(),
	Blocked:    []Person{{ID: "z", Name: "Zed"}},
	Catalog:    []Person{{ID: "a", Name: "Anna"}, {ID: "z", Name: "Zed"}},
}}

func TestSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocklist.yaml")
	if err := Save(path, savedFile); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `blocklist:
  last_synced: 2026-10-17T06:00:00Z
  # Delete everyone you want to ALLOW; the remaining entries form the blocklist.
  blocked:
    - {id: z, name: Zed}
  # Used to keep track of people for a future blocklist update. It does not define who is allowed:
  # people absent from ` + "`catalog`" + ` (and ` + "`blocked`" + `) are allowed.
  catalog:
    - {id: a, name: Anna}
    - {id: z, name: Zed}
`
	if string(raw) != want {
		t.Errorf("saved file:\n%s\nwant:\n%s", raw, want)
	}
}

func TestLoadAfterSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blocklist.yaml")
	if err := Save(path, savedFile); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	b, want := got.Blocklist, savedFile.Blocklist
	if !slices.Equal(b.Blocked, want.Blocked) || !slices.Equal(b.Catalog, want.Catalog) || !b.LastSynced.Equal(want.LastSynced) {
		t.Errorf("loaded %+v, want %+v", b, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "blocklist.yaml"))
	if err != nil {
		t.Fatalf("Load(missing file): unexpected error: %v", err)
	}
	if len(f.Blocklist.Blocked) != 0 || len(f.Blocklist.Catalog) != 0 {
		t.Errorf("Load(missing file) = %+v, want an empty blocklist", f)
	}
}
