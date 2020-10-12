# Echo Contribution

> Forked from [labstack/echo-contrib](https://github.com/labstack/echo-contrib).

[![GoDoc](http://img.shields.io/badge/go-documentation-blue.svg?style=flat-square)](http://godoc.org/github.com/aaronjan/echo-contrib)
[![License](http://img.shields.io/badge/license-mit-blue.svg?style=flat-square)](https://raw.githubusercontent.com/aaronjan/echo-contrib/master/LICENSE)

## Packages

### prometheus

> Original author: [carlosedp](https://github.com/carlosedp)

#### Embed Prometheus into an Existent Echo Instance

```go
import "github.com/labstack/echo/v4"
import "github.com/aaronjan/echo-contrib/prometheus"

func urlSkipper(c echo.Context) bool {
    if strings.HasPrefix(c.Path(), "/health-check") {
        return true
    }
    return false
}

func main() {
    e := echo.New()

    additionMetrics := []*prometheus.Metric{&prometheus.Metric{
        ID:   "helloCnt",
        Name: "hellos_total",
        Description: "How many times this app says hello to users.",
        Type: "summary",
    }}

    p := prometheus.NewPrometheusWithConfig(prometheus.Config{
        MetricsPath: "/metrics",
        Subsystem: "echo",
        Skipper: urlSkipper,
        AdditionMetrics: additionMetrics,
    })
    p.Embed(e)

    e.Start()
}
```

#### Start Prometheus in a Separate Echo Instance

```go
import "context"
import "github.com/labstack/echo/v4"
import "github.com/aaronjan/echo-contrib/prometheus"

func urlSkipper(c echo.Context) bool {
    if strings.HasPrefix(c.Path(), "/health-check") {
        return true
    }
    return false
}

func main() {
    e := echo.New()

    additionMetrics := []*prometheus.Metric{&prometheus.Metric{
        ID:   "helloCnt",
        Name: "hellos_total",
        Description: "How many times this app says hello to users.",
        Type: "summary",
    }}

    p := prometheus.NewPrometheusWithConfig(prometheus.Config{
        MetricsPath: "/metrics",
        Subsystem: "echo",
        Skipper: urlSkipper,
        AdditionMetrics: additionMetrics,
    })
    p.Mount(e)

    ctx := context.Background()
    p.Start(ctx, echo.New(), ":1323")
    // To stop: ctx.Done() <- nil

    e.Start()
}
```
