package state

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"slices"
	"time"

	"github.com/massitaverna/potd-immich/internal/fileutil"
)

const (
	hoursPerDay = 24
	filePerm    = 0o644
)

type Pick struct {
	AssetID  string    `json:"assetId"`
	PickedAt time.Time `json:"pickedAt"`
}

type State struct {
	History []Pick `json:"history"`
}

// Load returns an empty state if the file does not exist yet.
func Load(path string) (State, error) {
	var s State
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(b, &s)
}

func (s State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.WriteAtomic(path, append(b, '\n'), filePerm)
}

func subtractDays(now time.Time, days int) time.Time {
	return now.Add(-time.Duration(days*hoursPerDay) * time.Hour)
}

// Recent returns the asset IDs picked within the last `days` days.
func (s State) Recent(now time.Time, days int) map[string]bool {
	recent := map[string]bool{}
	from := subtractDays(now, days)
	for _, p := range s.History {
		if p.PickedAt.After(from) {
			recent[p.AssetID] = true
		}
	}
	return recent
}

// Record appends a pick, stored in UTC, and drops picks older than `days`.
func (s *State) Record(assetID string, now time.Time, days int) {
	from := subtractDays(now, days)
	s.History = slices.DeleteFunc(s.History, func(p Pick) bool { return !p.PickedAt.After(from) })
	s.History = append(s.History, Pick{AssetID: assetID, PickedAt: now.UTC()})
}

// LastPick returns the time a photo was last picked.
// If a photo was never picked so far, the returned boolean is false.
func (s State) LastPick() (time.Time, bool) {
	if len(s.History) == 0 {
		return time.Time{}, false
	}
	return s.History[len(s.History)-1].PickedAt, true
}
