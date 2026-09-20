package otel

import (
	"fmt"
	"strings"

	collectormetricsv1 "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricsv1 "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// DecodeMetrics validates an OTLP/HTTP metrics request and returns its data-point count.
func DecodeMetrics(contentType string, body []byte) (int, error) {
	var request collectormetricsv1.ExportMetricsServiceRequest
	var err error
	if strings.Contains(strings.ToLower(contentType), "json") {
		err = protojson.Unmarshal(body, &request)
	} else {
		err = proto.Unmarshal(body, &request)
	}
	if err != nil {
		return 0, fmt.Errorf("decode OTLP metrics: %w", err)
	}
	count := 0
	for _, resource := range request.ResourceMetrics {
		for _, scope := range resource.ScopeMetrics {
			for _, metric := range scope.Metrics {
				count += metricDataPoints(metric)
			}
		}
	}
	return count, nil
}

func metricDataPoints(metric *metricsv1.Metric) int {
	switch data := metric.GetData().(type) {
	case *metricsv1.Metric_Gauge:
		return len(data.Gauge.DataPoints)
	case *metricsv1.Metric_Sum:
		return len(data.Sum.DataPoints)
	case *metricsv1.Metric_Histogram:
		return len(data.Histogram.DataPoints)
	case *metricsv1.Metric_ExponentialHistogram:
		return len(data.ExponentialHistogram.DataPoints)
	case *metricsv1.Metric_Summary:
		return len(data.Summary.DataPoints)
	default:
		return 0
	}
}
