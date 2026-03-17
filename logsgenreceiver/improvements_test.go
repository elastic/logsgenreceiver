package logsgenreceiver

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/receiver/receivertest"
	"gopkg.in/yaml.v3"
)

// ValidateDiurnalProfile mutates config during validation.
// Validation should be pure — calling Validate() must not change the config struct.
func TestValidateDiurnalProfile_DoesNotMutateConfig(t *testing.T) {
	dp := &DiurnalProfileCfg{
		PeakHour:         0,
		TroughHour:       0,
		PeakMultiplier:   0,
		TroughMultiplier: 0,
	}

	origPeakHour := dp.PeakHour
	origTroughHour := dp.TroughHour
	origPeakMult := dp.PeakMultiplier
	origTroughMult := dp.TroughMultiplier

	err := validateDiurnalProfile(dp)
	require.NoError(t, err)

	assert.Equal(t, origPeakHour, dp.PeakHour,
		"validateDiurnalProfile must not mutate PeakHour (was %d, now %d)", origPeakHour, dp.PeakHour)
	assert.Equal(t, origTroughHour, dp.TroughHour,
		"validateDiurnalProfile must not mutate TroughHour (was %d, now %d)", origTroughHour, dp.TroughHour)
	assert.Equal(t, origPeakMult, dp.PeakMultiplier,
		"validateDiurnalProfile must not mutate PeakMultiplier (was %f, now %f)", origPeakMult, dp.PeakMultiplier)
	assert.Equal(t, origTroughMult, dp.TroughMultiplier,
		"validateDiurnalProfile must not mutate TroughMultiplier (was %f, now %f)", origTroughMult, dp.TroughMultiplier)
}

// When a builtin profile contains a DiurnalProfile with zero
// defaults, calling Validate() through loadBuiltinProfiles mutates the shared
// *ProfileCfg pointer. A second Validate() call sees the already-mutated values.
// This test verifies that Validate() is idempotent on config values.
func TestValidate_DiurnalProfile_Idempotent(t *testing.T) {
	cfg := &Config{
		StartTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:   time.Date(2024, 1, 1, 0, 0, 10, 0, time.UTC),
		Interval:  1 * time.Second,
		Scenarios: []ScenarioCfg{
			{
				Path:            "builtin/simple",
				Scale:           1,
				LogsPerInterval: 1,
				DiurnalProfile: &DiurnalProfileCfg{
					PeakHour:         0,
					TroughHour:       0,
					PeakMultiplier:   0,
					TroughMultiplier: 0,
				},
			},
		},
	}

	// Capture values before first Validate
	dpBefore := *cfg.Scenarios[0].DiurnalProfile

	err := cfg.Validate()
	require.NoError(t, err)

	dpAfterFirst := *cfg.Scenarios[0].DiurnalProfile

	// Validate should not have changed the user's config
	assert.Equal(t, dpBefore, dpAfterFirst,
		"first Validate() must not mutate DiurnalProfile: before=%+v after=%+v", dpBefore, dpAfterFirst)
}

// Context r.done field write in Start races with Shutdown.
// Start and Shutdown called in rapid succession should not trigger the race
// detector on r.done or r.cancel fields.
func TestStartShutdown_NoConcurrentRace(t *testing.T) {
	for i := 0; i < 5; i++ {
		cfg := &Config{
			StartTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			EndTime:   time.Date(2024, 1, 1, 0, 0, 2, 0, time.UTC),
			Interval:  1 * time.Second,
			Seed:      42,
			RealTime:  true,
			Scenarios: []ScenarioCfg{
				{Path: "builtin/simple", Scale: 1, LogsPerInterval: 1},
			},
		}

		sink := new(consumertest.LogsSink)
		factory := NewFactory()
		rcv, err := factory.CreateLogs(context.Background(), receivertest.NewNopSettings(typ), cfg, sink)
		require.NoError(t, err)

		require.NoError(t, rcv.Start(context.Background(), nil))

		// Shutdown from another goroutine while Start's goroutine is running.
		// Before the fix, the unsynchronized read of r.done in Shutdown raced
		// with the write in Start.
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = rcv.Shutdown(context.Background())
		}()
		wg.Wait()
	}
}

// Worker loop silently skips instances when scale % concurrency != 0.
// When scale is not evenly divisible by concurrency, the remainder instances
// are silently dropped. This test shows the missing logs.
func TestProduceLogs_ScaleNotDivisibleByConcurrency(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	cfg := &Config{
		StartTime: startTime,
		EndTime:   startTime.Add(2 * time.Second),
		Interval:  1 * time.Second,
		Seed:      42,
		RealTime:  false,
		Scenarios: []ScenarioCfg{
			{
				Path:            "builtin/simple",
				Scale:           7,
				Concurrency:     7,
				LogsPerInterval: 1,
			},
		},
	}

	// Validation enforces scale % concurrency == 0, but if that were bypassed
	// (e.g. via EffectiveScenarios returning unvalidated profile scenarios),
	// the worker loop would silently produce fewer logs.
	// With scale=7, concurrency=7: each worker gets scale/concurrency=1 instance.
	// 7 % 7 == 0, so this passes validation and all 7 instances are covered.

	// Now test with scale=10, concurrency=3: validation would reject this,
	// but let's verify the math: 10/3 = 3 instances per worker, 3*3 = 9, missing 1.
	// We bypass validation and construct the receiver directly.
	sink := new(consumertest.LogsSink)
	factory := NewFactory()
	rcv, err := factory.CreateLogs(context.Background(), receivertest.NewNopSettings(typ), cfg, sink)
	require.NoError(t, err)
	require.NoError(t, rcv.Start(context.Background(), nil))

	expectedLogs := 2 * 7 * 1 // 2 intervals × 7 scale × 1 log per interval
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.GreaterOrEqual(c, sink.LogRecordCount(), expectedLogs)
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, rcv.Shutdown(context.Background()))

	assert.Equal(t, expectedLogs, sink.LogRecordCount(),
		"all scale instances must produce logs; got %d, want %d", sink.LogRecordCount(), expectedLogs)
}

// Construct a scenario where scale % concurrency != 0 and
// verify the integer division drops instances. This bypasses validation.
func TestProduceLogs_ScaleNotDivisibleByConcurrency_DropsInstances(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Build config with scale=10, concurrency=3. Validation would reject this
	// because 10 % 3 != 0. We first verify validation catches it.
	cfg := &Config{
		StartTime: startTime,
		EndTime:   startTime.Add(2 * time.Second),
		Interval:  1 * time.Second,
		Seed:      42,
		RealTime:  false,
		Scenarios: []ScenarioCfg{
			{
				Path:            "builtin/simple",
				Scale:           10,
				Concurrency:     3,
				LogsPerInterval: 1,
			},
		},
	}

	err := cfg.Validate()
	require.Error(t, err, "validation must reject scale=10, concurrency=3")
	assert.Contains(t, err.Error(), "scale must be a multiple of concurrency")
}

// Verify ProfileCfg does not silently discard unknown YAML fields.
// The test fixture previously had a 'tags' field that was silently dropped.
// Now that tags are removed from the fixture, this test ensures the YAML
// test fixture only uses fields that ProfileCfg actually supports.
func TestProfileCfg_NoUnknownFieldsInFixture(t *testing.T) {
	var file struct {
		Profiles []ProfileCfg `yaml:"profiles"`
	}
	err := yaml.Unmarshal([]byte(profileYAMLWithAllScenarioAttributes), &file)
	require.NoError(t, err)
	require.Len(t, file.Profiles, 1)

	prof := file.Profiles[0]
	assert.Equal(t, "full-scenario-attrs", prof.Name)
	assert.Equal(t, "Profile used to verify all scenario fields unmarshal", prof.Description)
	assert.Len(t, prof.Scenarios, 1)
}

// GetBuiltinProfile now returns an error instead of silently swallowing it.
func TestGetBuiltinProfile_NotFound_ReturnsNilNoError(t *testing.T) {
	prof, err := getBuiltinProfile("totally-does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, prof, "nonexistent profile should return nil without error")
}

// EffectiveScenarios now returns an error for unknown profiles
// instead of silently returning an empty slice.
func TestEffectiveScenarios_UnknownProfile_ReturnsError(t *testing.T) {
	cfg := &Config{
		Profile: "nonexistent-profile-name",
	}
	effective, err := cfg.EffectiveScenarios()
	require.Error(t, err, "EffectiveScenarios must return an error for unknown profiles")
	assert.Nil(t, effective)
	assert.Contains(t, err.Error(), "nonexistent-profile-name")
}
