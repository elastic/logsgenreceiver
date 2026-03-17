package logsgenreceiver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/confmap/confmaptest"
	"go.opentelemetry.io/collector/confmap/xconfmap"
)

func TestLoadConfig(t *testing.T) {
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config.yaml"))
	require.NoError(t, err)
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig().(*Config)
	sub, err := cm.Sub("logsgen")
	require.NoError(t, err)
	require.NoError(t, sub.Unmarshal(cfg))

	assert.NoError(t, xconfmap.Validate(cfg))
	assert.Equal(t, testdataConfigYamlAsMap(), cfg)
}

func TestLoadConfigWithProfile(t *testing.T) {
	cm, err := confmaptest.LoadConf(filepath.Join("testdata", "config_with_profile.yaml"))
	require.NoError(t, err)
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig().(*Config)
	sub, err := cm.Sub("logsgen")
	require.NoError(t, err)
	require.NoError(t, sub.Unmarshal(cfg))

	assert.NoError(t, xconfmap.Validate(cfg))
	assert.Equal(t, "minimal", cfg.Profile)
	assert.Empty(t, cfg.Scenarios)
	effective, err := cfg.EffectiveScenarios()
	require.NoError(t, err)
	require.Len(t, effective, 1)
	assert.Equal(t, "builtin/simple", effective[0].Path)
	assert.Equal(t, 10, effective[0].Scale)
	assert.Equal(t, 5, effective[0].LogsPerInterval)
}

func testdataConfigYamlAsMap() *Config {
	startTime, _ := time.Parse(time.RFC3339, "2024-12-17T00:00:00Z")
	endTime, _ := time.Parse(time.RFC3339, "2024-12-17T00:00:31Z")
	interval, _ := time.ParseDuration("30s")
	return &Config{
		StartTime: startTime,
		EndTime:   endTime,
		Interval:  interval,
		Seed:      123,
		Profile:   "",
		Scenarios: []ScenarioCfg{
			{
				Path:            "builtin/simple",
				Scale:           10,
				LogsPerInterval: 5,
			},
		},
	}
}

func validBaseConfig() *Config {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return &Config{
		StartTime: start,
		EndTime:   start.Add(10 * time.Second),
		Interval:  1 * time.Second,
		Scenarios: []ScenarioCfg{
			{Path: "builtin/simple", Scale: 1, LogsPerInterval: 1},
		},
	}
}

func TestValidateVolumeProfile(t *testing.T) {
	t.Run("nil profile is valid", func(t *testing.T) {
		assert.NoError(t, validateVolumeProfile(nil))
	})

	t.Run("valid profile", func(t *testing.T) {
		assert.NoError(t, validateVolumeProfile(&VolumeProfileCfg{
			BurstProbability:   0.1,
			BurstMultiplierMin: 2.0,
			BurstMultiplierMax: 5.0,
			BurstDurationMin:   1,
			BurstDurationMax:   3,
			QuietProbability:   0.1,
			QuietMultiplier:    0.2,
			QuietDurationMin:   1,
			QuietDurationMax:   3,
		}))
	})

	t.Run("burst_probability out of range", func(t *testing.T) {
		assert.ErrorContains(t, validateVolumeProfile(&VolumeProfileCfg{
			BurstProbability: 1.5,
			BurstDurationMin: 1, BurstDurationMax: 1,
		}), "burst_probability")
	})

	t.Run("quiet_probability out of range", func(t *testing.T) {
		assert.ErrorContains(t, validateVolumeProfile(&VolumeProfileCfg{
			QuietProbability: -0.1,
			QuietDurationMin: 1, QuietDurationMax: 1,
		}), "quiet_probability")
	})

	t.Run("combined probability exceeds 1", func(t *testing.T) {
		assert.ErrorContains(t, validateVolumeProfile(&VolumeProfileCfg{
			BurstProbability: 0.6, BurstDurationMin: 1, BurstDurationMax: 1,
			QuietProbability: 0.6, QuietDurationMin: 1, QuietDurationMax: 1,
		}), "burst_probability + quiet_probability")
	})

	t.Run("burst_multiplier_max less than min", func(t *testing.T) {
		assert.ErrorContains(t, validateVolumeProfile(&VolumeProfileCfg{
			BurstProbability:   0.1,
			BurstMultiplierMin: 5.0,
			BurstMultiplierMax: 2.0,
			BurstDurationMin:   1,
			BurstDurationMax:   1,
		}), "burst_multiplier_max")
	})

	t.Run("burst_duration_min zero with positive probability", func(t *testing.T) {
		assert.ErrorContains(t, validateVolumeProfile(&VolumeProfileCfg{
			BurstProbability:   0.1,
			BurstMultiplierMin: 2.0,
			BurstMultiplierMax: 3.0,
			BurstDurationMin:   0,
			BurstDurationMax:   3,
		}), "burst_duration_min")
	})

	t.Run("quiet_duration_max less than min", func(t *testing.T) {
		assert.ErrorContains(t, validateVolumeProfile(&VolumeProfileCfg{
			QuietProbability: 0.1,
			QuietMultiplier:  0.2,
			QuietDurationMin: 5,
			QuietDurationMax: 2,
		}), "quiet_duration_max")
	})
}

func TestValidateDiurnalProfile(t *testing.T) {
	t.Run("nil profile is valid", func(t *testing.T) {
		assert.NoError(t, validateDiurnalProfile(nil))
	})

	t.Run("valid profile", func(t *testing.T) {
		assert.NoError(t, validateDiurnalProfile(&DiurnalProfileCfg{
			PeakHour:         14,
			TroughHour:       4,
			PeakMultiplier:   3.0,
			TroughMultiplier: 0.2,
		}))
	})

	t.Run("peak_hour out of range", func(t *testing.T) {
		assert.ErrorContains(t, validateDiurnalProfile(&DiurnalProfileCfg{
			PeakHour: 24, TroughHour: 4, PeakMultiplier: 3.0, TroughMultiplier: 0.2,
		}), "peak_hour")
	})

	t.Run("peak_hour equals trough_hour", func(t *testing.T) {
		assert.ErrorContains(t, validateDiurnalProfile(&DiurnalProfileCfg{
			PeakHour: 14, TroughHour: 14, PeakMultiplier: 3.0, TroughMultiplier: 0.2,
		}), "must differ")
	})

	t.Run("empty profile passes validation without mutation", func(t *testing.T) {
		cfg := &DiurnalProfileCfg{PeakHour: 0, TroughHour: 0}
		require.NoError(t, validateDiurnalProfile(cfg))
		assert.Equal(t, 0, cfg.PeakHour, "validate must not mutate PeakHour")
		assert.Equal(t, 0, cfg.TroughHour, "validate must not mutate TroughHour")
	})

	t.Run("applyDiurnalDefaults sets defaults", func(t *testing.T) {
		cfg := &DiurnalProfileCfg{PeakHour: 0, TroughHour: 0}
		applyDiurnalDefaults(cfg)
		assert.Equal(t, 14, cfg.PeakHour)
		assert.Equal(t, 4, cfg.TroughHour)
		assert.Equal(t, 3.0, cfg.PeakMultiplier)
		assert.Equal(t, 0.2, cfg.TroughMultiplier)
	})

	t.Run("cron burst duration >= interval", func(t *testing.T) {
		assert.ErrorContains(t, validateDiurnalProfile(&DiurnalProfileCfg{
			PeakHour: 14, TroughHour: 4, PeakMultiplier: 3.0, TroughMultiplier: 0.2,
			CronBursts: []CronBurstCfg{
				{Interval: 15 * time.Minute, Multiplier: 5.0, Duration: 15 * time.Minute},
			},
		}), "duration must be < interval")
	})
}

func TestValidateProfileAndScenarios(t *testing.T) {
	t.Run("unknown profile returns error", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.Scenarios = nil
		cfg.Profile = "nonexistent-profile"
		assert.ErrorContains(t, cfg.Validate(), "unknown profile")
	})

	t.Run("both profile and scenarios returns error", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.Profile = "minimal"
		assert.ErrorContains(t, cfg.Validate(), "cannot set both profile and scenarios")
	})

	t.Run("neither profile nor scenarios returns error", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.Scenarios = nil
		cfg.Profile = ""
		assert.ErrorContains(t, cfg.Validate(), "must set either profile or scenarios")
	})

	t.Run("valid profile passes validation", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.Scenarios = nil
		cfg.Profile = "minimal"
		assert.NoError(t, cfg.Validate())
	})

	t.Run("valid inline scenarios passes validation", func(t *testing.T) {
		cfg := validBaseConfig()
		assert.NoError(t, cfg.Validate())
	})
}

func TestEffectiveScenarios(t *testing.T) {
	t.Run("returns inline scenarios when profile empty", func(t *testing.T) {
		cfg := validBaseConfig()
		effective, err := cfg.EffectiveScenarios()
		require.NoError(t, err)
		require.Len(t, effective, 1)
		assert.Equal(t, "builtin/simple", effective[0].Path)
	})

	t.Run("returns profile scenarios when profile set", func(t *testing.T) {
		cfg := validBaseConfig()
		cfg.Scenarios = nil
		cfg.Profile = "minimal"
		require.NoError(t, cfg.Validate())
		effective, err := cfg.EffectiveScenarios()
		require.NoError(t, err)
		require.Len(t, effective, 1)
		assert.Equal(t, "builtin/simple", effective[0].Path)
		assert.Equal(t, 10, effective[0].Scale)
		assert.Equal(t, 5, effective[0].LogsPerInterval)
	})
}
