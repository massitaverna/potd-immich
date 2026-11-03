package conf

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	EnvPrefix = "POTD"

	KeyAPIURL           = "api-url"
	KeyAPIKeyFile       = "api-key-file"
	KeyAlbumName        = "album-name"
	KeyAvoidRepeatDays  = "avoid-repeat-days"
	KeyStateFile        = "state-file"
	KeyBlocklistFile    = "blocklist-file"
	KeyScheduleTime     = "schedule-time"
	KeyScheduleTimezone = "schedule-timezone"
	KeyDryRun           = "dry-run"

	DefaultAPIURL           = "http://host.docker.internal:2283"
	DefaultAPIKeyFile       = "/run/secrets/immich_api_key"
	DefaultAvoidRepeatDays  = 60
	DefaultStateFile        = "/var/lib/potd-immich/state.json"
	DefaultBlocklistFile    = "/var/lib/potd-immich/blocklist.yaml"
	DefaultScheduleTime     = "06:00"
	DefaultScheduleTimezone = "UTC"

	scheduleTimeLayout = "15:04"
)

type Config struct {
	APIURL          string
	APIKey          string
	AlbumName       string
	AvoidRepeatDays int
	StateFile       string
	BlocklistFile   string
	ScheduleHour    int
	ScheduleMinute  int
	Location        *time.Location
	DryRun          bool
}

// AddConnectionFlags registers the flags every subcommand needs.
func AddConnectionFlags(fs *pflag.FlagSet) {
	fs.String(KeyAPIURL, DefaultAPIURL, "Immich server URL")
	fs.String(KeyAPIKeyFile, DefaultAPIKeyFile, "file containing the Immich API key")
	fs.String(KeyBlocklistFile, DefaultBlocklistFile, "blocklist YAML file")
}

// AddRunFlags registers the flags needed to pick a photo and update the album.
func AddRunFlags(fs *pflag.FlagSet) {
	fs.String(KeyAlbumName, "", "name of the album holding the photo of the day (required)")
	fs.Int(KeyAvoidRepeatDays, DefaultAvoidRepeatDays, "do not repeat photos shown within this many days")
	fs.String(KeyStateFile, DefaultStateFile, "JSON file recording recent picks")
}

func AddDryRunFlag(fs *pflag.FlagSet) {
	fs.Bool(KeyDryRun, false, "print what would change without changing anything")
}

func AddScheduleFlags(fs *pflag.FlagSet) {
	fs.String(KeyScheduleTime, DefaultScheduleTime, "daily run time, HH:MM")
	fs.String(KeyScheduleTimezone, DefaultScheduleTimezone, "IANA time zone of the schedule, e.g. Europe/Zurich")
}

// Load resolves the flags in fs from, in order of precedence: CLI flags, POTD_* env vars, defaults.
func Load(fs *pflag.FlagSet) (Config, error) {
	v := viper.New()
	v.SetEnvPrefix(EnvPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	v.AutomaticEnv()
	if err := v.BindPFlags(fs); err != nil {
		return Config{}, err
	}

	c := Config{
		APIURL:          v.GetString(KeyAPIURL),
		AlbumName:       v.GetString(KeyAlbumName),
		AvoidRepeatDays: v.GetInt(KeyAvoidRepeatDays),
		StateFile:       v.GetString(KeyStateFile),
		BlocklistFile:   v.GetString(KeyBlocklistFile),
		DryRun:          v.GetBool(KeyDryRun),
	}
	if fs.Lookup(KeyAlbumName) != nil && c.AlbumName == "" {
		return c, fmt.Errorf("--%s (or %s) is required", KeyAlbumName, envName(KeyAlbumName))
	}
	if c.AvoidRepeatDays < 0 {
		return c, fmt.Errorf("--%s must not be negative", KeyAvoidRepeatDays)
	}
	if fs.Lookup(KeyScheduleTime) != nil {
		t, err := time.Parse(scheduleTimeLayout, v.GetString(KeyScheduleTime))
		if err != nil {
			return c, fmt.Errorf("--%s: %w", KeyScheduleTime, err)
		}
		c.ScheduleHour, c.ScheduleMinute = t.Hour(), t.Minute()
		if c.Location, err = time.LoadLocation(v.GetString(KeyScheduleTimezone)); err != nil {
			return c, fmt.Errorf("--%s: %w", KeyScheduleTimezone, err)
		}
	}

	key, err := os.ReadFile(v.GetString(KeyAPIKeyFile))
	if err != nil {
		return c, fmt.Errorf("read API key: %w", err)
	}
	if c.APIKey = strings.TrimSpace(string(key)); c.APIKey == "" {
		return c, errors.New("API key file is empty")
	}
	return c, nil
}

func envName(key string) string {
	return EnvPrefix + "_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
}
