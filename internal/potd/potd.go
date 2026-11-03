package potd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/massitaverna/potd-immich/internal/immich"
)

var ErrNoCandidates = errors.New("no candidate photos")

type API interface {
	ServerVersion(ctx context.Context) (immich.Version, error)
	MyUserID(ctx context.Context) (string, error)
	People(ctx context.Context) ([]immich.Person, error)
	SearchAssets(ctx context.Context, filter immich.SearchFilter) ([]immich.Asset, error)
	OwnedAlbumsByName(ctx context.Context, name string) ([]immich.Album, error)
	CreateAlbum(ctx context.Context, name string, assetIDs []string) (immich.Album, error)
	AddAlbumAssets(ctx context.Context, albumID string, assetIDs []string) error
	RemoveAlbumAssets(ctx context.Context, albumID string, assetIDs []string) error
}

type Options struct {
	AlbumName  string
	BlockedIDs []string
	Recent     map[string]bool
	DryRun     bool
}

type Runner struct {
	API  API
	Log  *slog.Logger
	Rand func(n int) int
}

// Run picks a photo and makes it the only asset in the album. It returns the chosen asset.
func (r Runner) Run(ctx context.Context, opts Options) (immich.Asset, error) {
	v, err := r.API.ServerVersion(ctx)
	if err != nil {
		return immich.Asset{}, fmt.Errorf("server version: %w", err)
	}
	if !v.Supported() {
		return immich.Asset{}, fmt.Errorf("unsupported Immich version %s (need >=3.2.0 and <4.0.0)", v)
	}
	r.Log.Info("connected to Immich", "version", v.String())

	cands, err := r.Candidates(ctx, opts.BlockedIDs)
	if err != nil {
		return immich.Asset{}, err
	}
	chosen, fallback, err := Pick(cands, opts.Recent, r.Rand)
	if err != nil {
		return immich.Asset{}, err
	}
	if fallback {
		r.Log.Warn("every candidate was shown recently, ignoring repeat avoidance", "candidates", len(cands))
	}
	r.Log.Info("picked photo", "asset", chosen.ID, "file", chosen.OriginalFileName, "taken", chosen.LocalDateTime,
		"candidates", len(cands), "recent", len(opts.Recent))

	return chosen, r.SetAlbum(ctx, opts.AlbumName, chosen.ID, opts.DryRun)
}

// Candidates returns own, untrashed timeline images with at least one allowed named person and no blocked person.
func (r Runner) Candidates(ctx context.Context, blockedIDs []string) ([]immich.Asset, error) {
	me, err := r.API.MyUserID(ctx)
	if err != nil {
		return nil, fmt.Errorf("current user: %w", err)
	}
	people, err := r.API.People(ctx)
	if err != nil {
		return nil, fmt.Errorf("people: %w", err)
	}

	blocked := map[string]bool{}
	for _, id := range blockedIDs {
		blocked[id] = true
	}
	exists := map[string]bool{}
	var allowed []string
	for _, p := range people {
		exists[p.ID] = true
		if p.Name != "" && !p.IsHidden && !blocked[p.ID] {
			allowed = append(allowed, p.ID)
		}
	}
	for _, id := range blockedIDs {
		if !exists[id] {
			r.Log.Warn("blocked person no longer exists in Immich", "id", id)
		}
	}
	r.Log.Info("people", "total", len(people), "allowed", len(allowed), "blocked", len(blockedIDs))
	if len(allowed) == 0 {
		return nil, nil
	}

	assets, err := r.API.SearchAssets(ctx, CandidateFilter(allowed, blockedIDs))
	if err != nil {
		return nil, fmt.Errorf("search candidates: %w", err)
	}
	var cands []immich.Asset
	unexpected := 0
	for _, a := range assets {
		switch {
		case a.OwnerID != me:
		case a.IsTrashed || a.Type != immich.AssetTypeImage || a.Visibility != immich.VisibilityTimeline:
			unexpected++
		default:
			cands = append(cands, a)
		}
	}
	if unexpected > 0 {
		r.Log.Warn("server search returned assets that do not match the filter, ignoring them", "count", unexpected)
	}
	return cands, nil
}

func CandidateFilter(allowedIDs, blockedIDs []string) immich.SearchFilter {
	return immich.SearchFilter{
		PersonIDs:  &immich.IDsFilter{Any: allowedIDs, None: blockedIDs},
		Type:       &immich.EqFilter{Eq: immich.AssetTypeImage},
		Visibility: &immich.EqFilter{Eq: immich.VisibilityTimeline},
		TrashedAt:  &immich.NullEqFilter{},
	}
}

// Pick chooses uniformly among candidates not shown recently, or among all if every one was.
func Pick(cands []immich.Asset, recent map[string]bool, randN func(int) int) (chosen immich.Asset, fallback bool, err error) {
	if len(cands) == 0 {
		return immich.Asset{}, false, ErrNoCandidates
	}
	var fresh []immich.Asset
	for _, a := range cands {
		if !recent[a.ID] {
			fresh = append(fresh, a)
		}
	}
	if len(fresh) == 0 {
		return cands[randN(len(cands))], true, nil
	}
	return fresh[randN(len(fresh))], false, nil
}

// SetAlbum makes assetID the only asset of the named album, adding before removing.
func (r Runner) SetAlbum(ctx context.Context, name, assetID string, dryRun bool) error {
	albums, err := r.API.OwnedAlbumsByName(ctx, name)
	if err != nil {
		return fmt.Errorf("find album: %w", err)
	}
	albums = slices.DeleteFunc(albums, func(a immich.Album) bool { return a.AlbumName != name })
	switch len(albums) {
	case 0:
		if dryRun {
			r.Log.Info("dry run: would create album", "album", name, "asset", assetID)
			return nil
		}
		album, err := r.API.CreateAlbum(ctx, name, []string{assetID})
		if err != nil {
			return fmt.Errorf("create album: %w", err)
		}
		r.Log.Info("created album", "album", name, "id", album.ID, "asset", assetID)
		return nil
	case 1:
	default:
		return fmt.Errorf("found %d albums named %q, expected one", len(albums), name)
	}

	album := albums[0]
	current, err := r.API.SearchAssets(ctx, immich.SearchFilter{AlbumIDs: &immich.IDsFilter{Any: []string{album.ID}}})
	if err != nil {
		return fmt.Errorf("list album assets: %w", err)
	}
	var stale []string
	present := false
	for _, a := range current {
		if a.ID == assetID {
			present = true
		} else {
			stale = append(stale, a.ID)
		}
	}

	if dryRun {
		r.Log.Info("dry run: would update album",
			"album", name, "id", album.ID, "add", !present, "asset", assetID, "remove", len(stale),
		)
		return nil
	}
	if !present {
		if err := r.API.AddAlbumAssets(ctx, album.ID, []string{assetID}); err != nil {
			return fmt.Errorf("add to album: %w", err)
		}
	}
	if len(stale) > 0 {
		if err := r.API.RemoveAlbumAssets(ctx, album.ID, stale); err != nil {
			return fmt.Errorf("remove from album: %w", err)
		}
	}
	r.Log.Info("updated album", "album", name, "id", album.ID, "asset", assetID, "removed", len(stale))
	return nil
}
