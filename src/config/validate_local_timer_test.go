package config

import (
	"strings"
	"testing"
)

func localTimerBaseConfig() Config {
	return attachMinimalRouting(Config{
		Receivers: map[string]ReceiverConfig{
			"rx_local": {
				Type:     "local_timer",
				Selector: "sel1",
				MatchKey: ReceiverMatchKeyConfig{
					Mode:       "fixed",
					FixedValue: "chain-monitor",
				},
				Generator: LocalGeneratorConfig{
					Interval:      "500ms",
					PayloadFormat: "hex",
					PayloadData:   "0102",
				},
			},
		},
		Senders:   map[string]SenderConfig{"s1": {Type: "tcp_gnet", Remote: "127.0.0.1:2"}},
		Pipelines: map[string][]StageConfig{"p1": {}},
		Tasks:     map[string]TaskConfig{"t1": {Pipelines: []string{"p1"}, Senders: []string{"s1"}}},
	})
}

func TestValidateLocalTimerIntervalModeValid(t *testing.T) {
	cfg := localTimerBaseConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate local_timer interval mode: %v", err)
	}
}

func TestValidateLocalTimerRateModeValid(t *testing.T) {
	cfg := localTimerBaseConfig()
	rc := cfg.Receivers["rx_local"]
	rc.Generator.Interval = ""
	rc.Generator.RatePerSec = 1000
	rc.Generator.TickInterval = "100ms"
	cfg.Receivers["rx_local"] = rc
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate local_timer rate mode: %v", err)
	}
}

func TestValidateLocalTimerRequiresOneScheduleMode(t *testing.T) {
	cfg := localTimerBaseConfig()
	rc := cfg.Receivers["rx_local"]
	rc.Generator.Interval = ""
	rc.Generator.RatePerSec = 0
	cfg.Receivers["rx_local"] = rc
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "interval and rate_per_sec") {
		t.Fatalf("expected schedule mode error, got %v", err)
	}
}

func TestValidateLocalTimerRejectsTwoScheduleModes(t *testing.T) {
	cfg := localTimerBaseConfig()
	rc := cfg.Receivers["rx_local"]
	rc.Generator.RatePerSec = 2
	cfg.Receivers["rx_local"] = rc
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "interval and rate_per_sec") {
		t.Fatalf("expected schedule mode conflict, got %v", err)
	}
}

func TestValidateLocalTimerRejectsPayloadFormat(t *testing.T) {
	cfg := localTimerBaseConfig()
	rc := cfg.Receivers["rx_local"]
	rc.Generator.PayloadFormat = "json"
	cfg.Receivers["rx_local"] = rc
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "payload_format") {
		t.Fatalf("expected payload_format error, got %v", err)
	}
}

func TestValidateLocalTimerRejectsNegativeTotalPackets(t *testing.T) {
	cfg := localTimerBaseConfig()
	rc := cfg.Receivers["rx_local"]
	rc.Generator.TotalPackets = -1
	cfg.Receivers["rx_local"] = rc
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "total_packets") {
		t.Fatalf("expected total_packets error, got %v", err)
	}
}
