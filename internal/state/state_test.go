package state

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const day = 24 * time.Hour

// fakeNow is 06:00 UTC, deliberately in another zone so tests can check that picks are stored in UTC.
var fakeNow = time.Date(2026, 10, 17, 8, 0, 0, 0, time.FixedZone("CEST", 2*60*60))

func TestRecentAndPrune(t *testing.T) {
	s := State{History: []Pick{
		{AssetID: "old", PickedAt: fakeNow.Add(-61 * day)},
		{AssetID: "mid", PickedAt: fakeNow.Add(-30 * day)},
	}}
	recent := s.Recent(fakeNow, 60)
	if recent["old"] || !recent["mid"] {
		t.Errorf("recent = %v", recent)
	}
	s.Record("new", fakeNow, 60)
	if len(s.History) != 2 || s.History[0].AssetID != "mid" || s.History[1].AssetID != "new" {
		t.Errorf("history = %+v", s.History)
	}
	if last, ok := s.LastPick(); !ok || !last.Equal(fakeNow) {
		t.Errorf("last = %v %v", last, ok)
	}
}

func TestZeroDaysKeepsLatestPick(t *testing.T) {
	var s State
	s.Record("a", fakeNow.Add(-day), 0)
	s.Record("b", fakeNow, 0)
	if len(s.History) != 1 || s.History[0].AssetID != "b" || len(s.Recent(fakeNow, 0)) != 0 {
		t.Errorf("history = %+v", s.History)
	}
}

func TestRecordStoresUTC(t *testing.T) {
	var s State
	s.Record("a", fakeNow, 60)
	if got := s.History[0].PickedAt; !got.Equal(fakeNow) || got.Location() != time.UTC {
		t.Errorf("pickedAt = %v, want %v", got, fakeNow.UTC())
	}
}

// savedState holds what Record produces: UTC times.
var savedState = State{History: []Pick{
	{AssetID: "a", PickedAt: fakeNow.Add(-day).UTC()},
	{AssetID: "b", PickedAt: fakeNow.UTC()},
}}

func TestSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := savedState.Save(path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "history": [
    {
      "assetId": "a",
      "pickedAt": "2026-10-16T06:00:00Z"
    },
    {
      "assetId": "b",
      "pickedAt": "2026-10-17T06:00:00Z"
    }
  ]
}
`
	if string(raw) != want {
		t.Errorf("saved file:\n%s\nwant:\n%s", raw, want)
	}
}

func TestLoadAfterSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := savedState.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	// Compare times with Equal: a loaded time has an equivalent but distinct *time.Location.
	same := func(a, b Pick) bool { return a.AssetID == b.AssetID && a.PickedAt.Equal(b.PickedAt) }
	if !slices.EqualFunc(got.History, savedState.History, same) {
		t.Errorf("loaded %+v, want %+v", got.History, savedState.History)
	}
}

func TestLoadMissingFile(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Load(missing file): unexpected error: %v", err)
	}
	if len(s.History) != 0 {
		t.Errorf("Load(missing file) = %+v, want an empty history", s)
	}
}
