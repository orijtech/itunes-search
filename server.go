package main

import (
	"log"
	"net"

	"google.golang.org/grpc"

	xray "github.com/census-instrumentation/opencensus-go-exporter-aws"
	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"

	"github.com/orijtech/itunes-search/rpc"
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

	prefix := "media-search"
	se, err := stackdriver.NewExporter(stackdriver.Options{
		MetricPrefix: prefix,
		ProjectID:    "census-demos",
	})
	if err != nil {
		log.Fatalf("Failed to create the Stackdriver exporter: %v", err)
	}
	view.RegisterExporter(se)
	trace.RegisterExporter(se)

	// AWS X-Ray
	xe, err := xray.NewExporter(xray.WithVersion("latest"))
	if err != nil {
		log.Fatalf("Failed to create the X-Ray exporter: %v", err)
	}
	trace.RegisterExporter(xe)
}
