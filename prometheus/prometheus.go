/*
Package prometheus provides middleware to add Prometheus metrics.

Example:
```
package main
import (
    "github.com/labstack/echo/v4"
    "github.com/labstack/echo-contrib/prometheus"
)
func main() {
    e := echo.New()
    // Enable metrics middleware
    p := prometheus.NewPrometheus("echo")
    p.Embed(e)

    e.Logger.Fatal(e.Start(":1323"))
}
```
*/
package prometheus

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var defaultMetricPath = "/metrics"
var defaultSubsystem = "echo"

// Standard default metrics
//	counter, counter_vec, gauge, gauge_vec,
//	histogram, histogram_vec, summary, summary_vec
var reqCnt = &Metric{
	ID:          "reqCnt",
	Name:        "requests_total",
	Description: "How many HTTP requests processed, partitioned by status code and HTTP method.",
	Type:        "counter_vec",
	Args:        []string{"code", "method", "host", "url"}}

var reqDur = &Metric{
	ID:          "reqDur",
	Name:        "request_duration_seconds",
	Description: "The HTTP request latencies in seconds.",
	Args:        []string{"code", "method", "url"},
	Type:        "histogram_vec"}

var resSz = &Metric{
	ID:          "resSz",
	Name:        "response_size_bytes",
	Description: "The HTTP response sizes in bytes.",
	Args:        []string{"code", "method", "url"},
	Type:        "histogram_vec"}

var reqSz = &Metric{
	ID:          "reqSz",
	Name:        "request_size_bytes",
	Description: "The HTTP request sizes in bytes.",
	Args:        []string{"code", "method", "url"},
	Type:        "histogram_vec"}

var standardMetrics = []*Metric{
	reqCnt,
	reqDur,
	resSz,
	reqSz,
}

/*
RequestCounterURLLabelMappingFunc is a function which can be supplied to the middleware to control
the cardinality of the request counter's "url" label, which might be required in some contexts.
For instance, if for a "/customer/:name" route you don't want to generate a time series for every
possible customer name, you could use this function:

func(c echo.Context) string {
	url := c.Request.URL.Path
	for _, p := range c.Params {
		if p.Key == "name" {
			url = strings.Replace(url, p.Value, ":name", 1)
			break
		}
	}
	return url
}

which would map "/customer/alice" and "/customer/bob" to their template "/customer/:name".
*/
type RequestCounterURLLabelMappingFunc func(c echo.Context) string

// Metric is a definition for the name, description, type, ID, and
// prometheus.Collector type (i.e. CounterVec, Summary, etc) of each metric
type Metric struct {
	MetricCollector prometheus.Collector
	ID              string
	Name            string
	Description     string
	Type            string
	Args            []string
}

// Prometheus contains the metrics gathered by the instance and its path
type Prometheus struct {
	reqCnt               *prometheus.CounterVec
	reqDur, reqSz, resSz *prometheus.HistogramVec
	router               *echo.Echo
	Ppg                  PushGateway

	metricsList []*Metric
	metricsPath string
	subsystem   string
	skipper     middleware.Skipper

	requestCounterURLLabelMappingFunc RequestCounterURLLabelMappingFunc

	// Context string to use as a prometheus URL label
	urlLabelFromContext string
}

// PushGateway contains the configuration for pushing to a Prometheus pushgateway (optional)
type PushGateway struct {
	// Push interval in seconds
	PushIntervalSeconds time.Duration

	// Push Gateway URL in format http://domain:port
	// where JOBNAME can be any string of your choice
	PushGatewayURL string

	// Local metrics URL where metrics are fetched from, this could be ommited in the future
	// if implemented using prometheus common/expfmt instead
	MetricsURL string

	// pushgateway job name, defaults to "echo"
	Job string
}

// Config let you configure how Prometheus works
type Config struct {
	MetricsPath     string
	Subsystem       string
	Skipper         middleware.Skipper
	AdditionMetrics []*Metric
}

// NewPrometheusWithConfig creates a new Prometheus instance with your configuration
func NewPrometheusWithConfig(config Config) *Prometheus {
	metricsPath := defaultString(config.MetricsPath, defaultMetricPath)
	subsystem := defaultString(config.Subsystem, defaultSubsystem)

	var skipper middleware.Skipper
	if config.Skipper != nil {
		skipper = config.Skipper
	} else {
		skipper = middleware.DefaultSkipper
	}

	var metricsList []*Metric = config.AdditionMetrics
	for _, metric := range standardMetrics {
		metricsList = append(metricsList, metric)
	}

	p := &Prometheus{
		metricsList: metricsList,
		metricsPath: metricsPath,
		subsystem:   subsystem,
		skipper:     skipper,
		requestCounterURLLabelMappingFunc: func(c echo.Context) string {
			return c.Path() // i.e. by default do nothing, i.e. return URL as is
		},
	}
	p.registerMetrics(config.Subsystem)

	return p
}

// NewPrometheus creates a new Prometheus instance with default configuration
func NewPrometheus(subsystem string) *Prometheus {
	return NewPrometheusWithConfig(Config{
		Subsystem: subsystem,
	})
}

// SetPushGateway sends metrics to a remote pushgateway exposed on pushGatewayURL
// every pushIntervalSeconds. Metrics are fetched from metricsURL
func (p *Prometheus) SetPushGateway(pushGatewayURL, metricsURL string, pushIntervalSeconds time.Duration) {
	p.Ppg.PushGatewayURL = pushGatewayURL
	p.Ppg.MetricsURL = metricsURL
	p.Ppg.PushIntervalSeconds = pushIntervalSeconds
	p.startPushTicker()
}

// SetPushGatewayJob job name, defaults to "echo"
func (p *Prometheus) SetPushGatewayJob(j string) {
	p.Ppg.Job = j
}

func (p *Prometheus) getMetrics() []byte {
	response, err := http.Get(p.Ppg.MetricsURL)
	if err != nil {
		log.Errorf("Error getting metrics: %v", err)
	}
	defer response.Body.Close()
	body, _ := ioutil.ReadAll(response.Body)

	return body
}

func (p *Prometheus) getPushGatewayURL() string {
	h, _ := os.Hostname()
	if p.Ppg.Job == "" {
		p.Ppg.Job = "echo"
	}
	return p.Ppg.PushGatewayURL + "/metrics/job/" + p.Ppg.Job + "/instance/" + h
}

func (p *Prometheus) sendMetricsToPushGateway(metrics []byte) {
	req, err := http.NewRequest("POST", p.getPushGatewayURL(), bytes.NewBuffer(metrics))
	client := &http.Client{}
	if _, err = client.Do(req); err != nil {
		log.Errorf("Error sending to push gateway: %v", err)
	}
}

func (p *Prometheus) startPushTicker() {
	ticker := time.NewTicker(time.Second * p.Ppg.PushIntervalSeconds)
	go func() {
		for range ticker.C {
			p.sendMetricsToPushGateway(p.getMetrics())
		}
	}()
}

// NewMetric associates prometheus.Collector based on Metric.Type
func NewMetric(m *Metric, subsystem string) prometheus.Collector {
	var metric prometheus.Collector
	switch m.Type {
	case "counter_vec":
		metric = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
			m.Args,
		)
	case "counter":
		metric = prometheus.NewCounter(
			prometheus.CounterOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
		)
	case "gauge_vec":
		metric = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
			m.Args,
		)
	case "gauge":
		metric = prometheus.NewGauge(
			prometheus.GaugeOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
		)
	case "histogram_vec":
		metric = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
			m.Args,
		)
	case "histogram":
		metric = prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
		)
	case "summary_vec":
		metric = prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
			m.Args,
		)
	case "summary":
		metric = prometheus.NewSummary(
			prometheus.SummaryOpts{
				Subsystem: subsystem,
				Name:      m.Name,
				Help:      m.Description,
			},
		)
	}
	return metric
}

func (p *Prometheus) registerMetrics(subsystem string) {
	for _, metricDef := range p.metricsList {
		metric := NewMetric(metricDef, subsystem)
		if err := prometheus.Register(metric); err != nil {
			log.Errorf("%s could not be registered in Prometheus: %v", metricDef.Name, err)
		}
		switch metricDef {
		case reqCnt:
			p.reqCnt = metric.(*prometheus.CounterVec)
		case reqDur:
			p.reqDur = metric.(*prometheus.HistogramVec)
		case resSz:
			p.resSz = metric.(*prometheus.HistogramVec)
		case reqSz:
			p.reqSz = metric.(*prometheus.HistogramVec)
		}
		metricDef.MetricCollector = metric
	}
}

func (p *Prometheus) Mount(e *echo.Echo) {
	e.Use(p.HandlerFunc)
}

func (p *Prometheus) SetRoute(e *echo.Echo) {
	e.GET(p.metricsPath, prometheusHandler())
}

// Embed adds the middleware to the Echo instance and setup the route for exporting
func (p *Prometheus) Embed(e *echo.Echo) {
	p.Mount(e)
	p.SetRoute(e)
}

// Start start Prometheus in a separate Echo instance, which can be shutdowned with the Context parameter
func (p *Prometheus) Start(e *echo.Echo, listenAddress string) {
	p.SetRoute(e)
	e.Start(listenAddress)
}

// HandlerFunc defines handler function for middleware
func (p *Prometheus) HandlerFunc(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) (err error) {
		if c.Path() == p.metricsPath {
			return next(c)
		}
		if p.skipper(c) {
			return next(c)
		}

		start := time.Now()
		reqSz := computeApproximateRequestSize(c.Request())

		if err = next(c); err != nil {
			c.Error(err)
		}

		status := strconv.Itoa(c.Response().Status)
		url := p.requestCounterURLLabelMappingFunc(c)

		elapsed := float64(time.Since(start)) / float64(time.Second)
		resSz := float64(c.Response().Size)

		p.reqDur.WithLabelValues(status, c.Request().Method, url).Observe(elapsed)

		if len(p.urlLabelFromContext) > 0 {
			u := c.Get(p.urlLabelFromContext)
			if u == nil {
				u = "unknown"
			}
			url = u.(string)
		}

		p.reqCnt.WithLabelValues(status, c.Request().Method, c.Request().Host, url).Inc()
		p.reqSz.WithLabelValues(status, c.Request().Method, url).Observe(float64(reqSz))
		p.resSz.WithLabelValues(status, c.Request().Method, url).Observe(resSz)

		return
	}
}

func prometheusHandler() echo.HandlerFunc {
	h := promhttp.Handler()
	return func(c echo.Context) error {
		h.ServeHTTP(c.Response(), c.Request())
		return nil
	}
}

func computeApproximateRequestSize(r *http.Request) int {
	s := 0
	if r.URL != nil {
		s = len(r.URL.Path)
	}

	s += len(r.Method)
	s += len(r.Proto)
	for name, values := range r.Header {
		s += len(name)
		for _, value := range values {
			s += len(value)
		}
	}
	s += len(r.Host)

	// N.B. r.Form and r.MultipartForm are assumed to be included in r.URL.

	if r.ContentLength != -1 {
		s += int(r.ContentLength)
	}
	return s
}

func defaultString(val string, defaultVal string) string {
	if val != "" {
		return val
	}
	return defaultVal
}
