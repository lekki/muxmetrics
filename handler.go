package muxmetrics

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatch"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"os"
	"time"
)

type MuxMetricsHandler struct {
	handler    *mux.Router
	metrics    chan *latencyMetric
	metricName string
}

type latencyMetric struct {
	Path    string
	Latency time.Duration
	Method  string
	When    time.Time
}

func NewMuxMetricsHandler(router *mux.Router, metricName string) *MuxMetricsHandler {
	mmh := &MuxMetricsHandler{handler: router, metricName: metricName, metrics: make(chan *latencyMetric)}
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

	for {
		metric, active := <-mmh.metrics
		if !active {
			break
		}

		latency := metric.Latency.Nanoseconds() / int64(time.Millisecond)
		params := &cloudwatch.PutMetricDataInput{
			MetricData: []*cloudwatch.MetricDatum{
				{
					MetricName: aws.String("Latency"),
					Dimensions: []*cloudwatch.Dimension{
						{
							Name:  aws.String("Path"),
							Value: aws.String(metric.Path),
						},
						{
							Name:  aws.String("Method"),
							Value: aws.String(metric.Method),
						},
					},
					Timestamp: aws.Time(metric.When),
					Unit:      aws.String("Milliseconds"),
					Value:     aws.Float64(float64(latency)),
				},
			},
			Namespace: aws.String(mmh.metricName),
		}
		_, err := cw.PutMetricData(params)

		if err != nil {
			log.Println(err.Error())
		}

	}
}
