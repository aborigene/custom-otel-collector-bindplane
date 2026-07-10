// factory.go — processor factory and KeyProvider construction.
package fieldcryptoprocessor

import (
	"context"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
)

// typeStr is the component type string used in collector config ("fieldcrypto").
const typeStr = "fieldcrypto"

// NewFactory returns the processor factory. Logs/traces/metrics are all supported at
// alpha stability; the demo and tests focus on logs.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		processor.WithLogs(createLogsProcessor, component.StabilityLevelAlpha),
		processor.WithTraces(createTracesProcessor, component.StabilityLevelAlpha),
		processor.WithMetrics(createMetricsProcessor, component.StabilityLevelAlpha),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		KeyDir:      "/var/keys",
		KeyProvider: providerDisk,
		MaskValue:   "[MASKED]",
	}
}

func createLogsProcessor(_ context.Context, set processor.Settings, cfg component.Config, next consumer.Logs) (processor.Logs, error) {
	return &logsProcessor{fieldCrypto: newFieldCrypto(cfg.(*Config), set.Logger), next: next}, nil
}

func createTracesProcessor(_ context.Context, set processor.Settings, cfg component.Config, next consumer.Traces) (processor.Traces, error) {
	return &tracesProcessor{fieldCrypto: newFieldCrypto(cfg.(*Config), set.Logger), next: next}, nil
}

func createMetricsProcessor(_ context.Context, set processor.Settings, cfg component.Config, next consumer.Metrics) (processor.Metrics, error) {
	return &metricsProcessor{fieldCrypto: newFieldCrypto(cfg.(*Config), set.Logger), next: next}, nil
}

// buildKeyProvider constructs the configured KeyProvider. The disk provider is the lab
// default; the KMS provider is a compiling stub that fails loudly (see kms_provider.go).
func buildKeyProvider(cfg *Config) (KeyProvider, error) {
	switch cfg.KeyProvider {
	case "", providerDisk:
		return NewDiskKeyProvider(cfg.KeyDir)
	case providerKMS:
		return NewKMSKeyProvider(cfg.KeyDir)
	default:
		return nil, fmt.Errorf("unknown key_provider %q", cfg.KeyProvider)
	}
}
