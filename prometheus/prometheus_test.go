package prometheus

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/appleboy/gofight/v2"
	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
)

func unregister(p *Prometheus) {
	prometheus.Unregister(p.reqCnt)
	prometheus.Unregister(p.reqDur)
	prometheus.Unregister(p.reqSz)
	prometheus.Unregister(p.resSz)
}

func TestPrometheus_Use(t *testing.T) {
	e := echo.New()
	p := NewPrometheus("echo")
	p.Embed(e)

	assert.Equal(t, 1, len(e.Routes()), "only one route should be added")
	assert.NotNil(t, e, "the engine should not be empty")
	assert.Equal(t, e.Routes()[0].Path, p.metricsPath, "the path should match the metrics path")
	unregister(p)
}

func TestPath(t *testing.T) {
	p := NewPrometheus("echo")
	assert.Equal(t, p.metricsPath, defaultMetricPath, "no usage of path should yield default path")
	unregister(p)
}

func TestSubsystem(t *testing.T) {
	p := NewPrometheus("echo")
	assert.Equal(t, p.subsystem, "echo", "subsystem should be default")
	unregister(p)
}

func TestEmbed(t *testing.T) {
	e := echo.New()
	p := NewPrometheus("echo")

	g := gofight.New()
	g.GET(p.metricsPath).Run(e, func(r gofight.HTTPResponse, rq gofight.HTTPRequest) {
		assert.Equal(t, http.StatusNotFound, r.Code)
	})

	p.Embed(e)

	g.GET(p.metricsPath).Run(e, func(r gofight.HTTPResponse, rq gofight.HTTPRequest) {
		assert.Equal(t, http.StatusOK, r.Code)
	})
	unregister(p)
}

func TestSetRoute(t *testing.T) {
	p := NewPrometheus("echo")
	e := echo.New()

	g := gofight.New()
	g.GET(p.metricsPath).Run(e, func(r gofight.HTTPResponse, rq gofight.HTTPRequest) {
		assert.Equal(t, http.StatusNotFound, r.Code)
	})

	p.SetRoute(e)

	g.GET(p.metricsPath).Run(e, func(r gofight.HTTPResponse, rq gofight.HTTPRequest) {
		assert.Equal(t, http.StatusOK, r.Code)
	})
	unregister(p)
}

func TestIgnore(t *testing.T) {
	e := echo.New()

	ipath := "/ping"
	lipath := fmt.Sprintf(`path="%s"`, ipath)
	ignore := func(c echo.Context) bool {
		if strings.HasPrefix(c.Path(), ipath) {
			return true
		}
		return false
	}
	p := NewPrometheusWithConfig(Config{
		Subsystem: "echo",
		Skipper:   ignore,
	})
	p.Embed(e)

	g := gofight.New()

	g.GET(p.metricsPath).Run(e, func(r gofight.HTTPResponse, rq gofight.HTTPRequest) {
		assert.Equal(t, http.StatusOK, r.Code)
		assert.NotContains(t, r.Body.String(), fmt.Sprintf("%s_requests_total", p.subsystem))
	})

	g.GET("/ping").Run(e, func(r gofight.HTTPResponse, rq gofight.HTTPRequest) { assert.Equal(t, http.StatusNotFound, r.Code) })

	g.GET(p.metricsPath).Run(e, func(r gofight.HTTPResponse, rq gofight.HTTPRequest) {
		assert.Equal(t, http.StatusOK, r.Code)
		assert.NotContains(t, r.Body.String(), fmt.Sprintf("%s_requests_total", p.subsystem))
		assert.NotContains(t, r.Body.String(), lipath, "ignored path must not be present")
	})
	unregister(p)
}

func TestMetricsGenerated(t *testing.T) {
	e := echo.New()
	p := NewPrometheus("echo")
	p.Embed(e)

	path := "/ping"
	lpath := fmt.Sprintf(`url="%s"`, path)

	g := gofight.New()
	g.GET(path).Run(e, func(r gofight.HTTPResponse, rq gofight.HTTPRequest) { assert.Equal(t, http.StatusNotFound, r.Code) })

	g.GET(p.metricsPath).Run(e, func(r gofight.HTTPResponse, rq gofight.HTTPRequest) {
		assert.Equal(t, http.StatusOK, r.Code)
		assert.Contains(t, r.Body.String(), fmt.Sprintf("%s_requests_total", p.subsystem))
		assert.Contains(t, r.Body.String(), lpath, "path must be present")
	})
	unregister(p)
}

func TestMetricsPathIgnored(t *testing.T) {
	e := echo.New()
	p := NewPrometheus("echo")
	p.Embed(e)

	g := gofight.New()
	g.GET(p.metricsPath).Run(e, func(r gofight.HTTPResponse, rq gofight.HTTPRequest) {
		assert.Equal(t, http.StatusOK, r.Code)
		assert.NotContains(t, r.Body.String(), fmt.Sprintf("%s_requests_total", p.subsystem))
	})
	unregister(p)
}
