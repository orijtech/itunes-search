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
	"log"
	"net"
	"net/http"

	"google.golang.org/grpc"

	"go.opencensus.io/exporter/prometheus"
	"go.opencensus.io/exporter/zipkin"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"
	"go.opencensus.io/zpages"

	openzipkin "github.com/openzipkin/zipkin-go"
	zipkinHTTP "github.com/openzipkin/zipkin-go/reporter/http"

	"github.com/orijtech/itunes-search/rpc"
	"github.com/orijtech/otils"
)

func main() {
	ln, err := net.Listen("tcp", ":9449")
	if err != nil {
		log.Fatalf("Failed to find an available address to bind a listener: %v", err)
	}
	defer ln.Close()

	createAndRegisterExporters()
	srv := grpc.NewServer(grpc.StatsHandler(new(ocgrpc.ServerHandler)))
	rpc.RegisterSearchServer(srv, new(rpc.SearchBackend))
	log.Printf("Running gRPC server at: %q", ln.Addr())

	// Run zPages too
	go func() {
		mux := http.NewServeMux()
		zpages.Handle(mux, "/debug")
		log.Fatal(http.ListenAndServe(":8888", mux))
	}()

	if err := srv.Serve(ln); err != nil {
		log.Fatalf("gRPC server error while serving: %v", err)
	}
}

func createAndRegisterExporters() {
	if err := view.Register(ocgrpc.DefaultClientViews...); err != nil {
		log.Fatalf("Failed to register ocgrpc defaultClient views: %v", err)
	}
	if err := view.Register(ocgrpc.DefaultServerViews...); err != nil {
		log.Fatalf("Failed to register ocgrpc defaultServer views: %v", err)
	}
	if err := view.Register(ochttp.DefaultClientViews...); err != nil {
		log.Fatalf("Failed to register ochttp defaultClient views: %v", err)
	}
	if err := view.Register(ochttp.DefaultServerViews...); err != nil {
		log.Fatalf("Failed to register ochttp defaultServer views: %v", err)
	}

	prefix := "itunessearch_server"
	localEndpoint, err := openzipkin.NewEndpoint(prefix, "localhost")
	if err != nil {
		log.Fatalf("Failed to create the Zipkin exporter endpoint: %v", err)
	}
	reporter := zipkinHTTP.NewReporter("http://localhost:9411/api/v2/spans")
	ze := zipkin.NewExporter(reporter, localEndpoint)
	trace.RegisterExporter(ze)

	// Prometheus
	pe, err := prometheus.NewExporter(prometheus.Options{Namespace: prefix})
	if err != nil {
		log.Fatalf("Failed to create Prometheus exporter: %v", err)
	}
	view.RegisterExporter(pe)
	prometheusBindAddr := otils.EnvOrAlternates("ITUNESSEARCH_GO_SERVER_PROMETHEUS_BIND_ADDR", ":9889")
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", pe)
		log.Fatal(http.ListenAndServe(prometheusBindAddr, mux))
	}()
}
