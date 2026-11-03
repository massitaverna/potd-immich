package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/massitaverna/potd-immich/internal/state"
)

// fakeImmich serves the endpoints the CLI uses and records album changes.
type fakeImmich struct {
	mu            sync.Mutex
	people        []map[string]any
	inAlbum       []string
	mutatingCalls []string
}

func (f *fakeImmich) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const albumID = "00000000-0000-4000-8000-000000000099"

	f.mu.Lock()
	defer f.mu.Unlock()

	reply := func(v any) { json.NewEncoder(w).Encode(v) }
	switch r.Method + " " + r.URL.Path {
	case "GET /api/server/version":
		reply(map[string]any{"major": 3, "minor": 3, "patch": 3})
	case "GET /api/users/me":
		reply(map[string]any{"id": "me"})
	case "GET /api/people":
		reply(map[string]any{"hasNextPage": false, "people": f.people})
	case "GET /api/albums":
		reply([]map[string]any{{"id": albumID, "albumName": r.URL.Query().Get("name")}})
	case "POST /api/search/metadata":
		var body struct {
			Filter map[string]json.RawMessage `json:"filter"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		var items []map[string]any
		if _, ok := body.Filter["albumIds"]; ok {
			for _, id := range f.inAlbum {
				items = append(items, map[string]any{"id": id})
			}
		} else {
			for i := 1; i <= 3; i++ {
				items = append(items, map[string]any{"id": fmt.Sprintf("img-%d", i), "ownerId": "me",
					"type": "IMAGE", "visibility": "timeline"})
			}
		}
		reply(map[string]any{"assets": map[string]any{"items": items, "nextCursor": nil}})
	case "PUT /api/albums/" + albumID + "/assets", "DELETE /api/albums/" + albumID + "/assets":
		var body struct{ IDs []string }
		json.NewDecoder(r.Body).Decode(&body)
		f.mutatingCalls = append(f.mutatingCalls, r.Method+" "+strings.Join(body.IDs, ","))
		if r.Method == http.MethodPut {
			f.inAlbum = append(f.inAlbum, body.IDs...)
		} else {
			f.inAlbum = []string{f.inAlbum[len(f.inAlbum)-1]}
		}
		var res []map[string]any
		for _, id := range body.IDs {
			res = append(res, map[string]any{"id": id, "success": true})
		}
		reply(res)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (f *fakeImmich) addPerson(id, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.people = append(f.people, map[string]any{"id": id, "name": name})
}

// execute runs one CLI invocation and returns what it printed to stdout and logged.
func execute(t *testing.T, stdin string, args ...string) (stdout, logs string) {
	t.Helper()
	var out, logBuf bytes.Buffer
	cmd := NewRootCmd(slog.New(slog.NewTextHandler(&logBuf, nil)), "test")
	cmd.SetArgs(args)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("%v: %v\nlogs:\n%s", args, err, logBuf.String())
	}
	return out.String(), logBuf.String()
}

// readBLocklist reads the blockist at the provided path and returns it as a string.
// It replaces the time-dependent last_synced value with "<time>", for testing purposes.
func readBlocklist(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	lastSynced := regexp.MustCompile(`(?m)^  last_synced: .*$`)
	return lastSynced.ReplaceAllString(string(raw), "  last_synced: <time>")
}

// TestRoot is an integration test of the entire tool, backed by a fake Immich API.
// It walks through the documented workflow: build the blocklist, dry run,
// daily runs, then a new person is named and the blocklist is updated again.
func TestRoot(t *testing.T) {
	fake := &fakeImmich{inAlbum: []string{"old"}}
	fake.addPerson("p-anna", "Anna")
	fake.addPerson("p-zed", "Zed")
	fake.addPerson("p-unnamed", "")
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	keyFile := filepath.Join(dir, "api-key")
	stateFile := filepath.Join(dir, "state.json")
	blocklistFile := filepath.Join(dir, "blocklist.yaml")
	os.WriteFile(keyFile, []byte("k\n"), 0o600)
	t.Setenv("POTD_API_URL", srv.URL)
	t.Setenv("POTD_API_KEY_FILE", keyFile)
	t.Setenv("POTD_STATE_FILE", stateFile)
	t.Setenv("POTD_BLOCKLIST_FILE", blocklistFile)
	t.Setenv("POTD_ALBUM_NAME", "Photo of the day")

	// 1. First update-blocklist: block Anna, allow Zed.
	stdout, _ := execute(t, "y\n\n", "update-blocklist")
	if want := "Add Anna to blocklist? (y/N): Add Zed to blocklist? (y/N): "; stdout != want {
		t.Errorf("prompts = %q, want %q", stdout, want)
	}
	want := `blocklist:
  last_synced: <time>
  # Delete everyone you want to ALLOW; the remaining entries form the blocklist.
  blocked:
    - {id: p-anna, name: Anna}
  # Used to keep track of people for a future blocklist update. It does not define who is allowed:
  # people absent from ` + "`catalog`" + ` (and ` + "`blocked`" + `) are allowed.
  catalog:
    - {id: p-anna, name: Anna}
    - {id: p-zed, name: Zed}
`
	if got := readBlocklist(t, blocklistFile); got != want {
		t.Errorf("blocklist.yaml after first update:\n%s\nwant:\n%s", got, want)
	}

	// 2. Dry run: logs the plan, changes nothing.
	_, logs := execute(t, "", "run", "--dry-run")
	for _, line := range []string{"picked photo", "dry run: would update album", "remove=1"} {
		if !strings.Contains(logs, line) {
			t.Errorf("dry-run logs missing %q:\n%s", line, logs)
		}
	}
	if len(fake.mutatingCalls) != 0 {
		t.Fatalf("dry run changed the album: %v", fake.mutatingCalls)
	}
	if _, err := os.Stat(stateFile); !os.IsNotExist(err) {
		t.Fatal("dry run wrote state")
	}

	// 3. Three daily runs: always exactly one photo, never a repeat, add before remove.
	seen := map[string]bool{}
	for range 3 {
		execute(t, "", "run")
		if len(fake.inAlbum) != 1 {
			t.Fatalf("album holds %v, want exactly one asset", fake.inAlbum)
		}
		seen[fake.inAlbum[0]] = true
	}
	if len(seen) != 3 {
		t.Errorf("three runs showed %v, want three distinct photos", seen)
	}
	if !strings.HasPrefix(fake.mutatingCalls[0], "PUT ") || !strings.HasPrefix(fake.mutatingCalls[1], "DELETE old") {
		t.Errorf("first swap must add before removing: %v", fake.mutatingCalls)
	}
	st, _ := state.Load(stateFile)
	if len(st.History) != 3 {
		t.Errorf("history = %+v", st.History)
	}

	// 4. Bea gets named in Immich: only she is prompted, and the hand-kept blocklist stays.
	fake.addPerson("p-bea", "Bea")
	stdout, _ = execute(t, "\n", "update-blocklist")
	if want := "Add Bea to blocklist? (y/N): "; stdout != want {
		t.Errorf("second blocklist update prompts = %q, want %q", stdout, want)
	}
	got := readBlocklist(t, blocklistFile)
	if !strings.Contains(got, "  blocked:\n    - {id: p-anna, name: Anna}\n  #") ||
		!strings.Contains(got, "    - {id: p-anna, name: Anna}\n    - {id: p-bea, name: Bea}\n    - {id: p-zed, name: Zed}\n") {
		t.Errorf("blocklist.yaml after second update is incorrect:\n%s", got)
	}
}
