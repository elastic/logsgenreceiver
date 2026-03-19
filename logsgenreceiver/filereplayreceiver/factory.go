package filereplayreceiver

import (
	"context"

	"github.com/elastic/logsgenreceiver/logsgenreceiver/filereplayreceiver/internal/metadata"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
)

func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		metadata.Type,
		createDefaultConfig,
		receiver.WithLogs(createLogsReceiver, metadata.LogsStability))
}

func createLogsReceiver(
	_ context.Context,
	set receiver.Settings,
	cfg component.Config,
	consumer consumer.Logs,
) (receiver.Logs, error) {
	oCfg := cfg.(*Config)
	return &fileReplayReceiver{
		cfg:      oCfg,
		settings: set,
		nextLogs: consumer,
		done:     make(chan struct{}),
	}, nil
}
