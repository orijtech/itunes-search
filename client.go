package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"google.golang.org/grpc"

	"go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/plugin/ocgrpc"
	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/trace"

	"github.com/olekukonko/tablewriter"
	"github.com/orijtech/itunes-search/rpc"
)

func main() {
	serverAddr := flag.String("server_addr", ":9449", "the gRPC server's address")
	country := flag.String("country", "us", "the country that this content should be localized to")
	entity := flag.String("entity", "music", `the entity categorization of media e.g "all" or "tvShow" or "movie" or "music" or "musicVideo"`)
	flag.Parse()

	createAndRegisterExporters()

	cc, err := grpc.Dial(*serverAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Failed to dial to the gRPC server: %v", err)
	}

	client := rpc.NewSearchClient(cc)
	in := bufio.NewReader(os.Stdin)
	for {
		ctx := context.Background()
		fmt.Printf("> ")
		bLine, _, err := in.ReadLine()
		if err != nil {
			log.Fatalf("Failed to read a line: %v", err)
		}
		line := strings.TrimSpace(string(bLine))
		ctx, span := trace.StartSpan(ctx, "searching")
		res, err := client.ITunesSearchNonStreaming(ctx, &rpc.Request{
			Query:   line,
			Country: *country,
			Entity:  *entity,
		})
		span.End()
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
	trace.ApplyConfig(trace.Config{DefaultSampler: trace.ProbabilitySampler(0.9)})

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
}
