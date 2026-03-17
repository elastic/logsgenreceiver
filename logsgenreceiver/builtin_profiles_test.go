package logsgenreceiver

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// profileYAMLWithAllScenarioAttributes is a single profile whose scenario sets every
// attribute that can appear in a scenario. Used to ensure profile YAML unmarshalling
// does not skip any field (e.g. due to missing yaml tags on nested structs).
const profileYAMLWithAllScenarioAttributes = `
profiles:
  - name: full-scenario-attrs
    description: "Profile used to verify all scenario fields unmarshal"
    scenarios:
      - path: builtin/k8s-nginx
        scale: 42
        concurrency: 6
        logs_per_interval: 100
        emit_trace_context: true
        instance_volume_skew: 1.25
        template_vars:
          nodes: 10
          pods_per_node: 3
        severity_weights: [0, 2, 80, 92, 100, 100]
        ip_pool:
          cidrs:
            - 192.168.0.0/24
            - 10.1.0.0/16
          zipf_skew: 2.0
        volume_profile:
          burst_probability: 0.2
          burst_multiplier_min: 1.5
          burst_multiplier_max: 5.0
          burst_duration_min: 2
          burst_duration_max: 10
          quiet_probability: 0.05
          quiet_multiplier: 0.3
          quiet_duration_min: 2
          quiet_duration_max: 8
        diurnal_profile:
          peak_hour: 14
          trough_hour: 4
          peak_multiplier: 3.5
          trough_multiplier: 0.25
          cron_bursts:
            - interval: 15m
              multiplier: 2.0
              duration: 2m
        needles:
          - name: test_needle
            message: "test needle message"
            rate: 0.01
            severity: WARN
            attributes:
              attr1: value1
              attr2: value2
          - name: error_needle
            message: "error needle"
            rate: 0.005
            severity: ERROR
            attributes: {}
`

func TestProfileYAMLUnmarshal_AllScenarioAttributes(t *testing.T) {
	var file struct {
		Profiles []ProfileCfg `yaml:"profiles"`
	}
	err := yaml.Unmarshal([]byte(profileYAMLWithAllScenarioAttributes), &file)
	require.NoError(t, err)
	require.Len(t, file.Profiles, 1)
	require.Len(t, file.Profiles[0].Scenarios, 1)

	prof := &file.Profiles[0]
	scn := &prof.Scenarios[0]

	// Profile metadata
	assert.Equal(t, "full-scenario-attrs", prof.Name)
	assert.Equal(t, "Profile used to verify all scenario fields unmarshal", prof.Description)

	// Scenario top-level
	assert.Equal(t, "builtin/k8s-nginx", scn.Path)
	assert.Equal(t, 42, scn.Scale)
	assert.Equal(t, 6, scn.Concurrency)
	assert.Equal(t, 100, scn.LogsPerInterval)
	assert.True(t, scn.EmitTraceContext)
	assert.InDelta(t, 1.25, scn.InstanceVolumeSkew, 1e-9)

	// template_vars
	require.NotNil(t, scn.TemplateVars)
	assert.Equal(t, 10, int(toInt64(scn.TemplateVars["nodes"])))
	assert.Equal(t, 3, int(toInt64(scn.TemplateVars["pods_per_node"])))

	// severity_weights
	require.NotNil(t, scn.SeverityWeights)
	assert.Equal(t, [6]int{0, 2, 80, 92, 100, 100}, *scn.SeverityWeights)

	// ip_pool
	require.NotNil(t, scn.IPPool)
	assert.Equal(t, []string{"192.168.0.0/24", "10.1.0.0/16"}, scn.IPPool.CIDRs)
	assert.InDelta(t, 2.0, scn.IPPool.ZipfSkew, 1e-9)

	// volume_profile
	require.NotNil(t, scn.VolumeProfile)
	assert.InDelta(t, 0.2, scn.VolumeProfile.BurstProbability, 1e-9)
	assert.InDelta(t, 1.5, scn.VolumeProfile.BurstMultiplierMin, 1e-9)
	assert.InDelta(t, 5.0, scn.VolumeProfile.BurstMultiplierMax, 1e-9)
	assert.Equal(t, 2, scn.VolumeProfile.BurstDurationMin)
	assert.Equal(t, 10, scn.VolumeProfile.BurstDurationMax)
	assert.InDelta(t, 0.05, scn.VolumeProfile.QuietProbability, 1e-9)
	assert.InDelta(t, 0.3, scn.VolumeProfile.QuietMultiplier, 1e-9)
	assert.Equal(t, 2, scn.VolumeProfile.QuietDurationMin)
	assert.Equal(t, 8, scn.VolumeProfile.QuietDurationMax)

	// diurnal_profile
	require.NotNil(t, scn.DiurnalProfile)
	assert.Equal(t, 14, scn.DiurnalProfile.PeakHour)
	assert.Equal(t, 4, scn.DiurnalProfile.TroughHour)
	assert.InDelta(t, 3.5, scn.DiurnalProfile.PeakMultiplier, 1e-9)
	assert.InDelta(t, 0.25, scn.DiurnalProfile.TroughMultiplier, 1e-9)
	require.Len(t, scn.DiurnalProfile.CronBursts, 1)
	assert.Equal(t, 15*time.Minute, scn.DiurnalProfile.CronBursts[0].Interval)
	assert.InDelta(t, 2.0, scn.DiurnalProfile.CronBursts[0].Multiplier, 1e-9)
	assert.Equal(t, 2*time.Minute, scn.DiurnalProfile.CronBursts[0].Duration)

	// needles
	require.Len(t, scn.Needles, 2)
	assert.Equal(t, "test_needle", scn.Needles[0].Name)
	assert.Equal(t, "test needle message", scn.Needles[0].Message)
	assert.InDelta(t, 0.01, scn.Needles[0].Rate, 1e-9)
	assert.Equal(t, "WARN", scn.Needles[0].Severity)
	assert.Equal(t, map[string]string{"attr1": "value1", "attr2": "value2"}, scn.Needles[0].Attributes)
	assert.Equal(t, "error_needle", scn.Needles[1].Name)
	assert.Equal(t, "error needle", scn.Needles[1].Message)
	assert.InDelta(t, 0.005, scn.Needles[1].Rate, 1e-9)
	assert.Equal(t, "ERROR", scn.Needles[1].Severity)
	assert.NotNil(t, scn.Needles[1].Attributes)
	assert.Empty(t, scn.Needles[1].Attributes)
}

func toInt64(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	default:
		return 0
	}
}

func TestProfileYAMLUnmarshal_BuiltinProfileK8sStack_NeedlesAndNestedAttrs(t *testing.T) {
	// Load real built-in profile and assert nested attributes (needles, volume_profile, etc.)
	// are present so we don't rely only on synthetic YAML.
	prof, err := getBuiltinProfile("k8s-medium-multiapp")
	require.NoError(t, err)
	require.NotNil(t, prof)
	require.NotEmpty(t, prof.Scenarios)

	// First scenario (nginx) has needles and volume_profile in builtin/profiles.yaml
	scn := &prof.Scenarios[0]
	assert.Equal(t, "builtin/k8s-nginx", scn.Path)
	require.NotNil(t, scn.VolumeProfile, "k8s-medium-multiapp nginx scenario should have volume_profile")
	assert.Greater(t, scn.VolumeProfile.BurstProbability, 0.0)
	assert.Greater(t, scn.VolumeProfile.BurstMultiplierMin, 0.0)
	require.NotEmpty(t, scn.Needles, "k8s-medium-multiapp nginx scenario should have needles")
	assert.Equal(t, "upstream_timeout", scn.Needles[0].Name)
	assert.Equal(t, "ERROR", scn.Needles[0].Severity)
	assert.Contains(t, scn.Needles[0].Message, "upstream timed out")
	require.NotNil(t, scn.Needles[0].Attributes)
	assert.Equal(t, "timeout", scn.Needles[0].Attributes["error.type"])
	assert.Equal(t, "backend-svc:8080", scn.Needles[0].Attributes["upstream.host"])

	// At least one scenario has diurnal_profile or ip_pool if we add it later; for now
	// ensure template_vars and instance_volume_skew are present where defined.
	assert.NotNil(t, scn.TemplateVars)
	assert.Greater(t, scn.InstanceVolumeSkew, 0.0)
}
