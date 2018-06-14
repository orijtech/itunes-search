// Copyright 2018, OpenCensus Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	proto "github.com/golang/protobuf/proto"
	"google.golang.org/grpc"

	xray "github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"

	"github.com/gomodule/redigo/redis"
	"github.com/olekukonko/tablewriter"

	"github.com/orijtech/itunes-search/rpc"
	"github.com/orijtech/otils"
)

var redisPool = &redis.Pool{
	Dial: func() (redis.Conn, error) {
		return redis.Dial("tcp",
			fmt.Sprintf("%s:6379", otils.EnvOrAlternates("ITUNESSEARCH_REDIS_SERVER_HOST", "localhost")))
	},
	TestOnBorrow: func(c redis.Conn, t time.Time) error {
		if time.Since(t) < (5 * time.Minute) {
			return nil
		}
		_, err := c.Do("PING")
		return err
	},
}

const _3HoursInSeconds = 3 * 60 * 60

func main() {
	serverAddr := flag.String("server_addr", ":9449", "the gRPC server's address")
	country := flag.String("country", "us", "the country that this content should be localized to")
	entity := flag.String("entity", "music", `the entity categorization of media e.g "all" or "tvShow" or "movie" or "music" or "musicVideo"`)
	flag.Parse()

	createAndRegisterExporters()

	cc, err := grpc.Dial(*serverAddr, grpc.WithInsecure(), grpc.WithStatsHandler(new(ocgrpc.ClientHandler)))
	if err != nil {
		log.Fatalf("Failed to dial to the gRPC server: %v", err)
	}

	searchClient := rpc.NewSearchClient(cc)
	in := bufio.NewReader(os.Stdin)
	for {
		ctx := context.Background()
		fmt.Printf("> ")
		bLine, _, err := in.ReadLine()
		if err != nil {
			log.Fatalf("Failed to read a line: %v", err)
		}
		line := strings.TrimSpace(string(bLine))
		res, err := performSearch(ctx, searchClient, &rpc.Request{
			Query:   line,
			Country: *country,
			Entity:  *entity,
		})
		if err != nil {
			log.Fatalf("Failed to search: %v", err)
		}
		fmt.Print("< ")
		printResults(res)
		fmt.Println("\n")
	}
}

func printResults(res *rpc.Response) {
	if len(res.Results) == 0 {
		fmt.Printf("No results returned!")
		return
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetRowLine(true)
	table.SetHeader([]string{"TrackName", "Kind", "TrackPrice", "Currency", "Streamable", "PreviewURL"})
	for _, res := range res.Results {
		table.Append([]string{
			res.TrackName,
			res.Kind,
			fmt.Sprintf("%.3f", res.TrackPrice),
			res.Currency,
			fmt.Sprintf("%v", res.Streamable),
			res.PreviewUrl,
		})
	}
	table.Render()
}

func createAndRegisterExporters() {
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.AlwaysSample()})

	// Register the gRPC related views
	if err := view.Register(ocgrpc.DefaultClientViews...); err != nil {
		log.Fatalf("Failed to register ocgrpc defaultClient views: %v", err)
	}
	if err := view.Register(ocgrpc.DefaultServerViews...); err != nil {
		log.Fatalf("Failed to register ocgrpc defaultServer views: %v", err)
	}

	// Register the HTTP related views
	if err := view.Register(ochttp.DefaultClientViews...); err != nil {
		log.Fatalf("Failed to register ochttp defaultClient views: %v", err)
	}
	if err := view.Register(ochttp.DefaultServerViews...); err != nil {
		log.Fatalf("Failed to register ochttp defaultServer views: %v", err)
	}

	// Register the Redis views
	if err := view.Register(redis.ObservabilityMetricViews...); err != nil {
		log.Fatalf("Failed to register redis views: %v", err)
	}

	xe, err := xray.NewExporter(xray.WithVersion("latest"))
	if err != nil {
		log.Fatalf("Failed to create the X-Ray exporter: %v", err)
	}
	trace.RegisterExporter(xe)

	prefix := "itunessearch_go_client"
	se, err := stackdriver.NewExporter(stackdriver.Options{
		MetricPrefix: prefix,
		ProjectID:    otils.EnvOrAlternates("ITUNESSEARCH_CLIENT_PROJECTID", "census-demos"),
	})
	if err != nil {
		log.Fatalf("Failed to create the Stackdriver exporter: %v", err)
	}
	view.RegisterExporter(se)
	trace.RegisterExporter(se)

	// Prometheus
	pe, err := prometheus.NewExporter(prometheus.Options{Namespace: prefix})
	if err != nil {
		log.Fatalf("Failed to create Prometheus exporter: %v", err)
	}
	view.RegisterExporter(pe)
	prometheusBindAddr := otils.EnvOrAlternates("ITUNESSEARCH_GO_CLIENT_PROMETHEUS_BIND_ADDR", ":9887")
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", pe)
		log.Fatal(http.ListenAndServe(prometheusBindAddr, mux))
	}()
}

var blankResponse = new(rpc.Response)

func performSearch(ctx context.Context, searchClient rpc.SearchClient, req *rpc.Request) (*rpc.Response, error) {
	ctx, span := trace.StartSpan(ctx, "searching")
	defer span.End()

	redisConn := redisPool.GetWithContext(ctx)
	defer redisConn.Close()

	// Check the cache
	cached, err := redis.Bytes(redisConn.Do("GET", req.Query))
	if err == nil && len(cached) > 0 {
		_, umSpan := trace.StartSpan(ctx, "proto.Unmarshal")
		defer umSpan.End()

		res := new(rpc.Response)
		if err := proto.Unmarshal(cached, res); err == nil && !reflect.DeepEqual(res, blankResponse) {
			return res, nil
		} else if err != nil {
			// Still mark the span and errored
			umSpan.SetStatus(trace.Status{Code: trace.StatusCodeInternal, Message: err.Error()})
		}
	}

	// Cache miss so now performing actual search
	res, err := searchClient.ITunesSearchNonStreaming(ctx, req)
	if err != nil {
		span.SetStatus(trace.Status{Code: trace.StatusCodeInternal, Message: err.Error()})
		return nil, err
	}

	// Now save this item for later cache hits
	_, mSpan := trace.StartSpan(ctx, "proto.Marshal")
	defer mSpan.End()

	blob, err := proto.Marshal(res)
	if err != nil {
		mSpan.SetStatus(trace.Status{Code: trace.StatusCodeInternal, Message: err.Error()})
		mSpan.End()
		// Not an error that should not show the user their results
		return res, nil
	}
	mSpan.End()

	if _, err := redisConn.Do("SETEX", req.Query, _3HoursInSeconds, blob); err != nil {
		span.SetStatus(trace.Status{Code: trace.StatusCodeInternal, Message: err.Error()})
	}
	return res, nil
}
