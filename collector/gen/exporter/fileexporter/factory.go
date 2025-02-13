// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package fileexporter // import "github.com/open-telemetry/opentelemetry-collector-contrib/exporter/fileexporter"

import (
	"context"
	"io"
	"os"

	"github.com/f5/otel-arrow-adapter/collector/gen/exporter/fileexporter/internal/metadata"
	"github.com/f5/otel-arrow-adapter/collector/gen/internal/sharedcomponent"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.uber.org/zap"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// the number of old log files to retain
	defaultMaxBackups = 100

	// the format of encoded telemetry data
	formatTypeJSON  = "json"
	formatTypeProto = "proto"

	// the type of compression codec
	compressionZSTD = "zstd"
)

// NewFactory creates a factory for OTLP exporter.
func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		metadata.Type,
		createDefaultConfig,
		exporter.WithTraces(createTracesExporter, metadata.TracesStability),
		exporter.WithMetrics(createMetricsExporter, metadata.MetricsStability),
		exporter.WithLogs(createLogsExporter, metadata.LogsStability))
}

func createDefaultConfig() component.Config {
	return &Config{
		FormatType: formatTypeJSON,
		Rotation:   &Rotation{MaxBackups: defaultMaxBackups},
	}
}

func createTracesExporter(
	ctx context.Context,
	set exporter.CreateSettings,
	cfg component.Config,
) (exporter.Traces, error) {
	conf := cfg.(*Config)
	writer, err := buildFileWriter(conf, set.Logger)
	if err != nil {
		return nil, err
	}
	fe, err := exporters.GetOrAdd(conf, func() (component.Component, error) {
		return newFileExporter(conf, writer), nil
	})
	if err != nil {
		return nil, err
	}
	return exporterhelper.NewTracesExporter(
		ctx,
		set,
		cfg,
		fe.Unwrap().(*fileExporter).consumeTraces,
		exporterhelper.WithStart(fe.Start),
		exporterhelper.WithShutdown(fe.Shutdown),
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
	)
}

func createMetricsExporter(
	ctx context.Context,
	set exporter.CreateSettings,
	cfg component.Config,
) (exporter.Metrics, error) {
	conf := cfg.(*Config)
	writer, err := buildFileWriter(conf, set.Logger)
	if err != nil {
		return nil, err
	}
	fe, err := exporters.GetOrAdd(conf, func() (component.Component, error) {
		return newFileExporter(conf, writer), nil
	})
	if err != nil {
		return nil, err
	}
	return exporterhelper.NewMetricsExporter(
		ctx,
		set,
		cfg,
		fe.Unwrap().(*fileExporter).consumeMetrics,
		exporterhelper.WithStart(fe.Start),
		exporterhelper.WithShutdown(fe.Shutdown),
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
	)
}

func createLogsExporter(
	ctx context.Context,
	set exporter.CreateSettings,
	cfg component.Config,
) (exporter.Logs, error) {
	conf := cfg.(*Config)
	writer, err := buildFileWriter(conf, set.Logger)
	if err != nil {
		return nil, err
	}
	fe, err := exporters.GetOrAdd(conf, func() (component.Component, error) {
		return newFileExporter(conf, writer), nil
	})
	if err != nil {
		return nil, err
	}
	return exporterhelper.NewLogsExporter(
		ctx,
		set,
		cfg,
		fe.Unwrap().(*fileExporter).consumeLogs,
		exporterhelper.WithStart(fe.Start),
		exporterhelper.WithShutdown(fe.Shutdown),
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
	)
}

func newFileExporter(conf *Config, writer WriteCloseFlusher) *fileExporter {
	fe := &fileExporter{
		path:             conf.Path,
		formatType:       conf.FormatType,
		file:             writer,
		tracesMarshaler:  tracesMarshalers[conf.FormatType],
		metricsMarshaler: metricsMarshalers[conf.FormatType],
		logsMarshaler:    logsMarshalers[conf.FormatType],
		compression:      conf.Compression,
		compressor:       buildCompressor(conf.Compression),
		flushInterval:    conf.FlushInterval,
	}
	return fe
}

func buildFileWriter(cfg *Config, logger *zap.Logger) (WriteCloseFlusher, error) {
	var writer io.WriteCloser
	var err error
	if cfg.Rotation == nil {
		writer, err = os.OpenFile(cfg.Path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
		if err != nil {
			return nil, err
		}
	} else {
		writer = &lumberjack.Logger{
			Filename:   cfg.Path,
			MaxSize:    cfg.Rotation.MaxMegabytes,
			MaxAge:     cfg.Rotation.MaxDays,
			MaxBackups: cfg.Rotation.MaxBackups,
			LocalTime:  cfg.Rotation.LocalTime,
		}
	}

	if cfg.FormatType == formatTypeProto {
		return NewFileWriter(cfg, logger, writer), nil
	}

	return NewLineWriter(cfg, logger, writer), nil
}

// This is the map of already created File exporters for particular configurations.
// We maintain this map because the Factory is asked trace and metric receivers separately
// when it gets CreateTracesReceiver() and CreateMetricsReceiver() but they must not
// create separate objects, they must use one Receiver object per configuration.
var exporters = sharedcomponent.NewSharedComponents[*Config, component.Component]()