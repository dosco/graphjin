package serv

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"contrib.go.opencensus.io/exporter/aws"
	"contrib.go.opencensus.io/exporter/prometheus"
	"contrib.go.opencensus.io/exporter/stackdriver"

	"contrib.go.opencensus.io/exporter/zipkin"
	"contrib.go.opencensus.io/integrations/ocsql"
	stdzipkin "github.com/openzipkin/zipkin-go"
	httpreporter "github.com/openzipkin/zipkin-go/reporter/http"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"go.opencensus.io/zpages"
)

func enableObservability(s *Service, mux *http.ServeMux) (func(), error) {
	// Enable OpenCensus zPages
	if s.conf.Telemetry.Debug {
		zpages.Handle(mux, "/telemetry")
	}

	// Enable ocsql metrics with OpenCensus
	ocsql.RegisterAllViews()

	var mex view.Exporter
	var tex trace.Exporter

	var mCloseFn, tCloseFn func()
	var err error

	// Set up the metrics exporter
	switch s.conf.Telemetry.Metrics.Exporter {
	case "prometheus":
		ep := "/metrics"

		if s.conf.Telemetry.Metrics.Endpoint != "" {
			ep = s.conf.Telemetry.Metrics.Endpoint
		}

		ex, err1 := prometheus.NewExporter(prometheus.Options{Namespace: s.conf.Telemetry.Metrics.Namespace})
		if err == nil {
			mux.Handle(ep, ex)
			s.log.Infof("prometheus exporter listening on: %s", ep)
		}
		mex, err = view.Exporter(ex), err1

	case "stackdriver":
		mex, err = stackdriver.NewExporter(stackdriver.Options{ProjectID: s.conf.Telemetry.Metrics.Key})
		if err == nil {
			s.log.Info("google stackdriver exporter initialized")
		}

	case "":
		s.log.Warn("open-census: no metrics exporter defined")

	default:
		err = fmt.Errorf("invalid metrics exporter")
	}

	if err != nil {
		return nil, fmt.Errorf("open-census: %s: %s", s.conf.Telemetry.Metrics, err)
	}

	if mex != nil {
		// Register the exporter
		view.RegisterExporter(mex)
	}

	// Set up the tracing exporter
	switch s.conf.Telemetry.Tracing.Exporter {
	case "xray", "aws":
		ex, err1 := aws.NewExporter(aws.WithVersion("latest"))
		if err == nil {
			tCloseFn = func() { ex.Flush() }
			s.log.Info("Amazon X-Ray exporter initialized")
		}
		tex, err = trace.Exporter(ex), err1

	case "zipkin":
		// The local endpoint stores the name and address of the local service
		lep, err := stdzipkin.NewEndpoint(s.conf.AppName, s.conf.hostPort)
		if err != nil {
			return nil, err
		}

		// The Zipkin reporter takes collected spans from the app and reports them to the backend
		// http://localhost:9411/api/v2/spans is the default for the Zipkin Span v2
		re := httpreporter.NewReporter(s.conf.Telemetry.Tracing.Endpoint)
		tCloseFn = func() { re.Close() }
		tex = zipkin.NewExporter(re, lep)

	case "":
		s.log.Warn("open-census: no traceing exporter defined")

	default:
		err = fmt.Errorf("invalid tracing exporter")
	}

	if err != nil {
		return nil, fmt.Errorf("open-census: %s: %v", s.conf.Telemetry.Tracing.Exporter,
			err)
	}

	if tex != nil {
		trace.RegisterExporter(tex)
		sample := s.conf.Telemetry.Tracing.Sample

		if sample == "always" {
			trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

		} else {
			prob := 0.5
			if v, err := strconv.ParseFloat(sample, 10); err == nil {
				prob = v
			}
			trace.ApplyConfig(trace.Config{DefaultSampler: trace.ProbabilitySampler(prob)})

		}
	}

	var closeOnce sync.Once

	return func() {
		// Flush and shutdown the Zipkin HTTP reporter
		closeOnce.Do(func() {
			if mCloseFn != nil {
				mCloseFn()
			}
			if tCloseFn != nil {
				tCloseFn()
			}
		})
	}, err
}
