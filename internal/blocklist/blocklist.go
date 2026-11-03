package blocklist

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/massitaverna/potd-immich/internal/fileutil"
	"github.com/massitaverna/potd-immich/internal/immich"
)

const (
	filePerm       = 0o644
	yamlIndent     = 2
	keyBlocked     = "blocked"
	keyCatalog     = "catalog"
	commentBlocked = "Delete everyone you want to ALLOW; the remaining entries form the blocklist."
	commentCatalog = "Used to keep track of people for a future blocklist update. It does not define who is allowed:\n" +
		"people absent from `catalog` (and `blocked`) are allowed."
)

type Person struct {
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

type Blocklist struct {
	LastSynced time.Time `yaml:"last_synced,omitempty"`
	Blocked    []Person  `yaml:"blocked"`
	Catalog    []Person  `yaml:"catalog"`
}

type File struct {
	Blocklist Blocklist `yaml:"blocklist"`
}

// Confirm asks whether a person should be blocked.
type Confirm func(label string) (bool, error)

// Load returns an empty file if it does not exist yet.
func Load(path string) (File, error) {
	var f File
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return f, nil
	}
	if err != nil {
		return f, err
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		return f, fmt.Errorf("parse %s: %w", path, err)
	}
	return f, nil
}

func (f File) BlockedIDs() []string {
	ids := make([]string, 0, len(f.Blocklist.Blocked))
	for _, p := range f.Blocklist.Blocked {
		ids = append(ids, p.ID)
	}
	return ids
}

func Save(path string, f File) error {
	var root yaml.Node
	if err := root.Encode(f); err != nil {
		return err
	}
	annotate(&root)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(yamlIndent)
	if err := enc.Encode(&root); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return fileutil.WriteAtomic(path, buf.Bytes(), filePerm)
}

// annotate adds the header comments and renders each person on a single line.
func annotate(root *yaml.Node) {
	bl := root.Content[1]
	for i := 0; i < len(bl.Content); i += 2 {
		key, val := bl.Content[i], bl.Content[i+1]
		switch key.Value {
		case keyBlocked:
			key.HeadComment = commentBlocked
		case keyCatalog:
			key.HeadComment = commentCatalog
		default:
			continue
		}
		for _, entry := range val.Content {
			entry.Style = yaml.FlowStyle
		}
	}
}

// Sync updates the blocklist from the current list of people in Immich.
func Sync(old File, people []immich.Person, confirm Confirm, log *slog.Logger, now time.Time) (File, error) {
	exists := map[string]immich.Person{}
	var catalog []Person
	nameCount := map[string]int{}
	for _, p := range people {
		if _, dup := exists[p.ID]; dup {
			continue
		}
		exists[p.ID] = p
		if p.Name != "" && !p.IsHidden {
			catalog = append(catalog, Person{ID: p.ID, Name: p.Name})
			nameCount[p.Name]++
		}
	}
	sortPeople(catalog)

	var blocked []Person
	isBlocked := map[string]bool{}
	for _, b := range old.Blocklist.Blocked {
		cur, ok := exists[b.ID]
		if !ok {
			log.Warn("blocked person no longer exists in Immich, dropping", "id", b.ID, "name", b.Name)
			continue
		}
		if cur.Name != "" {
			b.Name = cur.Name
		}
		if !isBlocked[b.ID] {
			blocked = append(blocked, b)
			isBlocked[b.ID] = true
		}
	}

	known := map[string]bool{}
	for _, p := range old.Blocklist.Catalog {
		known[p.ID] = true
	}
	for _, p := range catalog {
		if known[p.ID] || isBlocked[p.ID] {
			continue
		}
		label := p.Name
		if nameCount[p.Name] > 1 {
			label = fmt.Sprintf("%s (%s)", p.Name, p.ID)
		}
		yes, err := confirm(label)
		if err != nil {
			return File{}, err
		}
		if yes {
			blocked = append(blocked, p)
		}
	}
	sortPeople(blocked)

	return File{Blocklist: Blocklist{LastSynced: now.UTC(), Blocked: blocked, Catalog: catalog}}, nil
}

func sortPeople(ps []Person) {
	slices.SortFunc(ps, func(a, b Person) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}
