package conf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// daemonFlags returns the daemon command's flag set (the widest one), parsed from args.
func daemonFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	fs := pflag.NewFlagSet("daemon", pflag.ContinueOnError)
	AddConnectionFlags(fs)
	AddRunFlags(fs)
	AddScheduleFlags(fs)
	return parse(t, fs, args)
}

// updateBlocklistFlags returns the update-blocklist command's flag set (connection flags only), parsed from args.
func updateBlocklistFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	fs := pflag.NewFlagSet("update-blocklist", pflag.ContinueOnError)
	AddConnectionFlags(fs)
	return parse(t, fs, args)
}

func parse(t *testing.T, fs *pflag.FlagSet, args []string) *pflag.FlagSet {
	t.Helper()
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	return fs
}

func keyFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key")
	os.WriteFile(path, []byte("  the-key\n"), 0o600)
	return path
}

func TestPrecedenceFlagOverEnvOverDefault(t *testing.T) {
	t.Setenv("POTD_API_KEY_FILE", keyFile(t))
	t.Setenv("POTD_ALBUM_NAME", "From env")
	t.Setenv("POTD_AVOID_REPEAT_DAYS", "10")
	t.Setenv("POTD_SCHEDULE_TIMEZONE", "Europe/Zurich")

	c, err := Load(daemonFlags(t, "--album-name", "From flag", "--schedule-time", "07:30"))
	if err != nil {
		t.Fatal(err)
	}
	if c.AlbumName != "From flag" || c.AvoidRepeatDays != 10 || c.APIKey != "the-key" {
		t.Errorf("config = %+v", c)
	}
	if c.APIURL != DefaultAPIURL || c.StateFile != DefaultStateFile {
		t.Errorf("defaults not applied: %+v", c)
	}
	if c.ScheduleHour != 7 || c.ScheduleMinute != 30 || c.Location.String() != "Europe/Zurich" {
		t.Errorf("schedule = %d:%d %v", c.ScheduleHour, c.ScheduleMinute, c.Location)
	}
}

func TestAlbumNameRequired(t *testing.T) {
	t.Setenv("POTD_API_KEY_FILE", keyFile(t))
	if _, err := Load(daemonFlags(t)); err == nil || !strings.Contains(err.Error(), envName(KeyAlbumName)) {
		t.Errorf("err = %v", err)
	}
}

func TestAlbumNameNotNeededForBlocklist(t *testing.T) {
	t.Setenv("POTD_API_KEY_FILE", keyFile(t))
	if _, err := Load(updateBlocklistFlags(t)); err != nil {
		t.Error(err)
	}
}

func TestDefaultTimezoneIsUTC(t *testing.T) {
	t.Setenv("POTD_API_KEY_FILE", keyFile(t))
	c, err := Load(daemonFlags(t, "--album-name", "x"))
	if err != nil || c.Location.String() != "UTC" {
		t.Errorf("location = %v, err = %v", c.Location, err)
	}
}

func TestMissingKeyFile(t *testing.T) {
	if _, err := Load(daemonFlags(t, "--album-name", "x", "--api-key-file", "/nonexistent")); err == nil {
		t.Error("expected error")
	}
}
