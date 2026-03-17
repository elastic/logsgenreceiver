package logsgenreceiver

import (
	"fmt"
	"net"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
)

type Config struct {
	StartTime            time.Time     `mapstructure:"start_time"`
	StartNowMinus        time.Duration `mapstructure:"start_now_minus"`
	EndTime              time.Time     `mapstructure:"end_time"`
	EndNowMinus          time.Duration `mapstructure:"end_now_minus"`
	Interval             time.Duration `mapstructure:"interval"`
	IntervalJitterStdDev time.Duration `mapstructure:"interval_jitter_std_dev"`
	RealTime             bool          `mapstructure:"real_time"`
	ExitAfterEnd         bool          `mapstructure:"exit_after_end"`
	ExitAfterEndTimeout  time.Duration `mapstructure:"exit_after_end_timeout"`
	Seed                 int64         `mapstructure:"seed"`
	Profile              string        `mapstructure:"profile"`
	Scenarios            []ScenarioCfg `mapstructure:"scenarios"`
}

type ScenarioCfg struct {
	Path             string             `mapstructure:"path" yaml:"path"`
	Scale            int                `mapstructure:"scale" yaml:"scale"`
	Concurrency      int                `mapstructure:"concurrency" yaml:"concurrency"`
	TemplateVars     map[string]any     `mapstructure:"template_vars" yaml:"template_vars"`
	LogsPerInterval  int                `mapstructure:"logs_per_interval" yaml:"logs_per_interval"`
	EmitTraceContext bool               `mapstructure:"emit_trace_context" yaml:"emit_trace_context"`
	Needles          []NeedleCfg        `mapstructure:"needles" yaml:"needles"`
	VolumeProfile    *VolumeProfileCfg  `mapstructure:"volume_profile" yaml:"volume_profile"`
	DiurnalProfile   *DiurnalProfileCfg `mapstructure:"diurnal_profile" yaml:"diurnal_profile"`
	// SeverityWeights overrides the profile's default severity distribution.
	// Cumulative percentages for [TRACE, DEBUG, INFO, WARN, ERROR, FATAL].
	// e.g. [0, 3, 85, 93, 100, 100] = 0% TRACE, 3% DEBUG, 82% INFO, 8% WARN, 7% ERROR, 0% FATAL.
	// If nil or all zeros, the profile default is used.
	SeverityWeights *[6]int `mapstructure:"severity_weights" yaml:"severity_weights"`
	// IPPool configures the IP address pool used for generating net.peer.ip and similar fields.
	// When nil, defaults are used: CIDRs=["10.0.0.0/8"], pool size=scale*10, zipf_skew=1.5.
	IPPool *IPPoolCfg `mapstructure:"ip_pool" yaml:"ip_pool"`
	// InstanceVolumeSkew applies a log-normal distribution to per-instance log counts.
	// The value is the sigma (standard deviation) of the underlying normal distribution.
	// Higher values produce wider spread: 0 = flat (all instances equal),
	// 1.0 = moderate variation (~0.3x to ~3x), 1.5 = wide (~0.1x to ~5x).
	// Multipliers are computed once at init from the global seed, so output is deterministic.
	// The mean multiplier is normalized to 1.0, preserving total volume.
	InstanceVolumeSkew float64 `mapstructure:"instance_volume_skew" yaml:"instance_volume_skew"`
}

// ProfileCfg defines a named set of scenarios with optional metadata.
// Built-in profiles are loaded from embedded YAML; use Config.Profile to select one by name.
type ProfileCfg struct {
	Name        string        `mapstructure:"name" yaml:"name"`
	Description string        `mapstructure:"description" yaml:"description"`
	Scenarios   []ScenarioCfg `mapstructure:"scenarios" yaml:"scenarios"`
}

type IPPoolCfg struct {
	// CIDRs to draw IPs from. Each must be a valid IPv4 CIDR.
	// Default: ["10.0.0.0/8"]
	CIDRs []string `mapstructure:"cidrs" yaml:"cidrs"`
	// ZipfSkew controls the Zipf distribution skew (s parameter).
	// Higher values make fewer IPs dominate traffic. Must be > 1.0.
	// Default: 1.5
	ZipfSkew float64 `mapstructure:"zipf_skew" yaml:"zipf_skew"`
}

type DiurnalProfileCfg struct {
	PeakHour         int            `mapstructure:"peak_hour" yaml:"peak_hour"`
	TroughHour       int            `mapstructure:"trough_hour" yaml:"trough_hour"`
	PeakMultiplier   float64        `mapstructure:"peak_multiplier" yaml:"peak_multiplier"`
	TroughMultiplier float64        `mapstructure:"trough_multiplier" yaml:"trough_multiplier"`
	CronBursts       []CronBurstCfg `mapstructure:"cron_bursts" yaml:"cron_bursts"`
}

type CronBurstCfg struct {
	Interval   time.Duration `mapstructure:"interval" yaml:"interval"`
	Multiplier float64       `mapstructure:"multiplier" yaml:"multiplier"`
	Duration   time.Duration `mapstructure:"duration" yaml:"duration"`
}

type VolumeProfileCfg struct {
	BurstProbability   float64 `mapstructure:"burst_probability" yaml:"burst_probability"`
	BurstMultiplierMin float64 `mapstructure:"burst_multiplier_min" yaml:"burst_multiplier_min"`
	BurstMultiplierMax float64 `mapstructure:"burst_multiplier_max" yaml:"burst_multiplier_max"`
	BurstDurationMin   int     `mapstructure:"burst_duration_min" yaml:"burst_duration_min"`
	BurstDurationMax   int     `mapstructure:"burst_duration_max" yaml:"burst_duration_max"`
	QuietProbability   float64 `mapstructure:"quiet_probability" yaml:"quiet_probability"`
	QuietMultiplier    float64 `mapstructure:"quiet_multiplier" yaml:"quiet_multiplier"`
	QuietDurationMin   int     `mapstructure:"quiet_duration_min" yaml:"quiet_duration_min"`
	QuietDurationMax   int     `mapstructure:"quiet_duration_max" yaml:"quiet_duration_max"`
}

type NeedleCfg struct {
	Name       string            `mapstructure:"name" yaml:"name"`
	Message    string            `mapstructure:"message" yaml:"message"`
	Rate       float64           `mapstructure:"rate" yaml:"rate"`
	Severity   string            `mapstructure:"severity" yaml:"severity"`
	Attributes map[string]string `mapstructure:"attributes" yaml:"attributes"`
}

func createDefaultConfig() component.Config {
	return &Config{
		Seed:      0,
		Scenarios: make([]ScenarioCfg, 0),
	}
}

func (cfg *Config) Validate() error {
	if cfg.Interval.Seconds() < 1 {
		return fmt.Errorf("the interval has to be set to at least 1 second (1s)")
	}

	if cfg.StartTime.After(cfg.EndTime) {
		return fmt.Errorf("start_time must be before end_time")
	}

	hasProfile := cfg.Profile != ""
	hasScenarios := len(cfg.Scenarios) > 0
	if hasProfile && hasScenarios {
		return fmt.Errorf("cannot set both profile and scenarios; use one or the other")
	}
	if !hasProfile && !hasScenarios {
		return fmt.Errorf("must set either profile or scenarios")
	}

	if hasProfile {
		prof, err := getBuiltinProfile(cfg.Profile)
		if err != nil {
			return fmt.Errorf("profile %q: %w", cfg.Profile, err)
		}
		if prof == nil {
			return fmt.Errorf("unknown profile %q", cfg.Profile)
		}
		if len(prof.Scenarios) == 0 {
			return fmt.Errorf("profile %q has no scenarios", cfg.Profile)
		}
		return validateScenarios(prof.Scenarios)
	}

	return validateScenarios(cfg.Scenarios)
}

// EffectiveScenarios returns the scenarios to use: either from the selected built-in profile or from Config.Scenarios.
// Must be called after a successful Validate() so the selected profile is guaranteed to exist.
func (cfg *Config) EffectiveScenarios() ([]ScenarioCfg, error) {
	if cfg.Profile != "" {
		prof, err := getBuiltinProfile(cfg.Profile)
		if err != nil {
			return nil, fmt.Errorf("loading profile %q: %w", cfg.Profile, err)
		}
		if prof == nil {
			return nil, fmt.Errorf("unknown profile %q", cfg.Profile)
		}
		return prof.Scenarios, nil
	}

	return cfg.Scenarios, nil
}

func validateScenarios(scenarios []ScenarioCfg) error {
	for _, scn := range scenarios {
		if scn.Scale < 0 {
			return fmt.Errorf("scenarios: scale must be non-negative")
		}
		if scn.Concurrency != 0 && scn.Scale > 0 && scn.Scale%scn.Concurrency != 0 {
			return fmt.Errorf("scenarios: scale must be a multiple of concurrency")
		}
		if scn.Concurrency < 0 {
			return fmt.Errorf("scenarios: concurrency must be non-negative")
		}
		if scn.LogsPerInterval < 0 {
			return fmt.Errorf("scenarios: logs_per_interval must be non-negative")
		}
		for _, needle := range scn.Needles {
			if needle.Name == "" {
				return fmt.Errorf("scenarios: needle name must not be empty")
			}
			if needle.Message == "" {
				return fmt.Errorf("scenarios: needle %q message must not be empty", needle.Name)
			}
			if needle.Rate < 0.0 || needle.Rate > 1.0 {
				return fmt.Errorf("scenarios: needle %q rate must be between 0.0 and 1.0", needle.Name)
			}
			sev := strings.ToUpper(strings.TrimSpace(needle.Severity))
			if sev != "" {
				valid := sev == "TRACE" || sev == "DEBUG" || sev == "INFO" || sev == "WARN" || sev == "ERROR" || sev == "FATAL"
				if !valid {
					return fmt.Errorf("scenarios: needle %q severity must be TRACE, DEBUG, INFO, WARN, ERROR, or FATAL", needle.Name)
				}
			}
		}
		if err := validateVolumeProfile(scn.VolumeProfile); err != nil {
			return fmt.Errorf("scenarios: %w", err)
		}
		if err := validateDiurnalProfile(scn.DiurnalProfile); err != nil {
			return fmt.Errorf("scenarios: %w", err)
		}
		if err := validateSeverityWeights(scn.SeverityWeights); err != nil {
			return fmt.Errorf("scenarios: %w", err)
		}
		if err := validateIPPool(scn.IPPool); err != nil {
			return fmt.Errorf("scenarios: %w", err)
		}
		if scn.InstanceVolumeSkew < 0 {
			return fmt.Errorf("scenarios: instance_volume_skew must be non-negative (got %f)", scn.InstanceVolumeSkew)
		}
	}
	return nil
}

func validateSeverityWeights(sw *[6]int) error {
	if sw == nil {
		return nil
	}
	prev := 0
	for i, w := range sw {
		if w < prev {
			return fmt.Errorf("severity_weights: values must be non-decreasing (index %d: %d < %d)", i, w, prev)
		}
		if w < 0 || w > 100 {
			return fmt.Errorf("severity_weights: values must be between 0 and 100 (index %d: %d)", i, w)
		}
		prev = w
	}
	if sw[5] != 100 {
		return fmt.Errorf("severity_weights: last value must be 100 (got %d)", sw[5])
	}
	return nil
}

func validateIPPool(ip *IPPoolCfg) error {
	if ip == nil {
		return nil
	}
	for _, cidr := range ip.CIDRs {
		_, _, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("ip_pool: invalid CIDR %q: %w", cidr, err)
		}
	}
	if ip.ZipfSkew != 0 && ip.ZipfSkew <= 1.0 {
		return fmt.Errorf("ip_pool: zipf_skew must be > 1.0 (got %f)", ip.ZipfSkew)
	}
	return nil
}

func applyDiurnalDefaults(dp *DiurnalProfileCfg) {
	if dp == nil {
		return
	}
	if dp.PeakHour == 0 && dp.TroughHour == 0 {
		dp.PeakHour = 14
		dp.TroughHour = 4
	}
	if dp.PeakMultiplier <= 0 {
		dp.PeakMultiplier = 3.0
	}
	if dp.TroughMultiplier <= 0 {
		dp.TroughMultiplier = 0.2
	}
}

func validateDiurnalProfile(dp *DiurnalProfileCfg) error {
	if dp == nil {
		return nil
	}
	peakHour := dp.PeakHour
	troughHour := dp.TroughHour
	if peakHour == 0 && troughHour == 0 {
		peakHour = 14
		troughHour = 4
	}
	if peakHour < 0 || peakHour > 23 {
		return fmt.Errorf("diurnal_profile: peak_hour must be 0-23 (got %d)", peakHour)
	}
	if troughHour < 0 || troughHour > 23 {
		return fmt.Errorf("diurnal_profile: trough_hour must be 0-23 (got %d)", troughHour)
	}
	if peakHour == troughHour {
		return fmt.Errorf("diurnal_profile: peak_hour and trough_hour must differ")
	}
	for i, cb := range dp.CronBursts {
		if cb.Interval <= 0 {
			return fmt.Errorf("diurnal_profile: cron_bursts[%d] interval must be > 0", i)
		}
		if cb.Multiplier <= 0 {
			return fmt.Errorf("diurnal_profile: cron_bursts[%d] multiplier must be > 0", i)
		}
		if cb.Duration <= 0 {
			return fmt.Errorf("diurnal_profile: cron_bursts[%d] duration must be > 0", i)
		}
		if cb.Duration >= cb.Interval {
			return fmt.Errorf("diurnal_profile: cron_bursts[%d] duration must be < interval", i)
		}
	}
	return nil
}

func validateVolumeProfile(vp *VolumeProfileCfg) error {
	if vp == nil {
		return nil
	}
	if vp.BurstProbability < 0 || vp.BurstProbability > 1 {
		return fmt.Errorf("volume_profile: burst_probability must be between 0.0 and 1.0")
	}
	if vp.QuietProbability < 0 || vp.QuietProbability > 1 {
		return fmt.Errorf("volume_profile: quiet_probability must be between 0.0 and 1.0")
	}
	if vp.BurstProbability+vp.QuietProbability > 1 {
		return fmt.Errorf("volume_profile: burst_probability + quiet_probability must not exceed 1.0")
	}
	if vp.BurstMultiplierMin < 0 {
		return fmt.Errorf("volume_profile: burst_multiplier_min must be non-negative")
	}
	if vp.BurstMultiplierMax < vp.BurstMultiplierMin {
		return fmt.Errorf("volume_profile: burst_multiplier_max must be >= burst_multiplier_min")
	}
	if vp.BurstDurationMin < 1 && vp.BurstProbability > 0 {
		return fmt.Errorf("volume_profile: burst_duration_min must be >= 1 when burst_probability > 0")
	}
	if vp.BurstDurationMax < vp.BurstDurationMin {
		return fmt.Errorf("volume_profile: burst_duration_max must be >= burst_duration_min")
	}
	if vp.QuietMultiplier < 0 {
		return fmt.Errorf("volume_profile: quiet_multiplier must be non-negative")
	}
	if vp.QuietDurationMin < 1 && vp.QuietProbability > 0 {
		return fmt.Errorf("volume_profile: quiet_duration_min must be >= 1 when quiet_probability > 0")
	}
	if vp.QuietDurationMax < vp.QuietDurationMin {
		return fmt.Errorf("volume_profile: quiet_duration_max must be >= quiet_duration_min")
	}
	return nil
}
