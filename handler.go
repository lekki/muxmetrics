package muxmetrics

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"
)

type DimensionConfig struct {
	Patterns      []string
	NotPatterns   []string
	Name          string
	patternsRE    []*regexp.Regexp
	notPatternsRE []*regexp.Regexp
}

type MuxMetricsHandler struct {
	handler          *mux.Router
	metrics          chan *latencyMetric
	metricName       string
	dimensionConfigs []*DimensionConfig
}

type latencyMetric struct {
	Path    string
	Latency time.Duration
	Method  string
	When    time.Time
}

func NewMuxMetricsHandler(router *mux.Router, metricName string, dimensionConfigs []*DimensionConfig) *MuxMetricsHandler {
	mmh := &MuxMetricsHandler{handler: router, metricName: metricName, metrics: make(chan *latencyMetric), dimensionConfigs: dimensionConfigs}
	go mmh.initCloudWatchSender()
	return mmh
}

func (mmh *MuxMetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	start := time.Now()
	mmh.handler.ServeHTTP(w, r)
	completed := time.Since(start)

	var match mux.RouteMatch
	if mmh.handler.Match(r, &match) {
		path, err := match.Route.GetPathTemplate()

		if err != nil {
			log.Println(err)
			return
		}
		mmh.metrics <- &latencyMetric{Latency: completed, Path: path, Method: r.Method, When: start}
	}

}

func (mmh *MuxMetricsHandler) initCloudWatchSender() {
	cw := cloudwatch.New(session.New(&aws.Config{Region: aws.String(os.Getenv("AWS_REGION"))}), aws.NewConfig().WithLogLevel(aws.LogOff))

	for _, cfg := range mmh.dimensionConfigs {

		cfg.patternsRE = make([]*regexp.Regexp, len(cfg.Patterns), len(cfg.Patterns))
		for i, pattern := range cfg.Patterns {
			re, err := regexp.Compile(pattern)
			if err != nil {
				panic(err)
			}
			cfg.patternsRE[i] = re
		}

		cfg.notPatternsRE = make([]*regexp.Regexp, len(cfg.NotPatterns), len(cfg.NotPatterns))
		for i, pattern := range cfg.NotPatterns {
			re, err := regexp.Compile(pattern)
			if err != nil {
				panic(err)
			}
			cfg.notPatternsRE[i] = re
		}

	}

	for {
		metric, active := <-mmh.metrics
		if !active {
			break
		}

		latency := metric.Latency.Nanoseconds() / int64(time.Millisecond)

		metricsData := []*cloudwatch.MetricDatum{
			{
				MetricName: aws.String("Latency per Request"),
				Dimensions: []*cloudwatch.Dimension{
					{
						Name:  aws.String("Path"),
						Value: aws.String(metric.Method + " " + metric.Path),
					},
				},
				Timestamp: aws.Time(metric.When),
				Unit:      aws.String("Milliseconds"),
				Value:     aws.Float64(float64(latency)),
			},
			{
				MetricName: aws.String("Latency"),
				Timestamp:  aws.Time(metric.When),
				Unit:       aws.String("Milliseconds"),
				Value:      aws.Float64(float64(latency)),
			}}

		var matchCfgName string
		for _, cfg := range mmh.dimensionConfigs {
			match := true
			for _, re := range cfg.patternsRE {
				if !re.MatchString(metric.Path) {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			match = true
			for _, re := range cfg.notPatternsRE {
				if re.MatchString(metric.Path) {
					match = false
					break
				}
			}
			matchCfgName = cfg.Name

		}

		if len(matchCfgName) > 0 {

			metricsDatum := &cloudwatch.MetricDatum{MetricName: aws.String("Latency per Group"),
				Timestamp: aws.Time(metric.When),
				Unit:      aws.String("Milliseconds"),
				Value:     aws.Float64(float64(latency)),
				Dimensions: []*cloudwatch.Dimension{
					{
						Name:  aws.String("Group"),
						Value: aws.String(matchCfgName),
					},
				},
			}

			metricsData = append(metricsData, metricsDatum)
		}

		params := &cloudwatch.PutMetricDataInput{
			MetricData: metricsData,
			Namespace:  aws.String(mmh.metricName),
		}
		_, err := cw.PutMetricData(params)

		if err != nil {
			log.Println(err.Error())
		}

	}
}
