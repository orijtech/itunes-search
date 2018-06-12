package io.mediasearch.search;

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;

import redis.clients.jedis.Jedis;
import redis.clients.jedis.Observability;

import io.mediasearch.search.Defs.Request;
import io.mediasearch.search.Defs.Response;
import io.mediasearch.search.SearchGrpc;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStreamReader;
import java.util.concurrent.TimeUnit;

import io.opencensus.common.Duration;
import io.opencensus.contrib.grpc.metrics.RpcViews;
import io.opencensus.exporter.stats.stackdriver.StackdriverStatsConfiguration;
import io.opencensus.exporter.stats.stackdriver.StackdriverStatsExporter;
import io.opencensus.exporter.trace.stackdriver.StackdriverTraceConfiguration;
import io.opencensus.exporter.trace.stackdriver.StackdriverTraceExporter;
import io.opencensus.trace.Span;
import io.opencensus.trace.Tracer;
import io.opencensus.trace.Tracing;
import io.opencensus.trace.config.TraceConfig;
import io.opencensus.trace.config.TraceParams;
import io.opencensus.trace.samplers.Samplers;

public class MediasearchClient {
    private final ManagedChannel channel;
    private final SearchGrpc.SearchBlockingStub stub;

    private static final Tracer tracer = Tracing.getTracer();
    private static final Jedis jedis = new Jedis("localhost");

    public MediasearchClient(String host, int port) {
        try {
            jedis.auth("");
        } catch (Exception e) {
            // Just in case the server doesn't support auth
            // TODO: actually check if NoAuth is returned
        }

        // Create the gRPC channel to the server.
        this.channel = ManagedChannelBuilder.forAddress(host, port)
            .usePlaintext(true)
            .build();
        this.stub = SearchGrpc.newBlockingStub(this.channel);
    }

    public void shutdown() throws InterruptedException {
        this.channel.shutdown().awaitTermination(4, TimeUnit.SECONDS);
    }

    public String search(Request req) {
        Span span = MediasearchClient.tracer.spanBuilder("(*MediasearchClient).search")
            .setRecordEvents(true)
            .startSpan();

        try {
            String query = req.getQuery();
            String found = jedis.get(query);
            if (found != null && found != "") {
                span.addAnnotation("Cache hit");
                return found;
            }

            // Otherwise this is a cache miss, now query then insert the result
            span.addAnnotation("Cache miss");
            Response resp = this.stub.iTunesSearchNonStreaming(req);
            String dataOut = resp.toString();
            jedis.set(query, dataOut);
            return dataOut;
        } finally {
            System.out.println("span.end() " + span.toString());
            span.end();
        }
    }

    public static void main(String []args) {
        MediasearchClient client = new MediasearchClient("0.0.0.0", 9449);

        try {
            setupOpenCensusAndExporters();
        } catch (IOException e) {
            System.err.println("Failed to setup OpenCensus exporters: " + e + "\nso proceeding without them");
        }

        try {
            // Setup OpenCensus exporters
            BufferedReader stdin = new BufferedReader(new InputStreamReader(System.in));
            while (true) {
                System.out.print("> ");
                System.out.flush();
                String query = stdin.readLine();

                Span span = MediasearchClient.tracer.spanBuilder("search")
                    .setRecordEvents(true)
                    .startSpan();
                Request req = Request.newBuilder().setQuery(query).build();
                String response = client.search(req);
                span.end();
                System.out.println("< " + response);
            }
        } catch (Exception e) {
            System.err.println("Exception encountered: " + e);
        }
    }

    private static void setupOpenCensusAndExporters() throws IOException {
        // Enable exporting of all the Jedis specific metrics and views
        Observability.registerAllViews();

        // Change the sampling rate to always sample
        TraceConfig traceConfig = Tracing.getTraceConfig();
        traceConfig.updateActiveTraceParams(
                traceConfig.getActiveTraceParams().toBuilder().setSampler(Samplers.alwaysSample()).build());

        // Register all the gRPC views and enable stats
        RpcViews.registerAllViews();

        String gcpProjectId = System.getenv().get("MEDIASEARCH_CLIENT_PROJECTID");
        if (gcpProjectId == null || gcpProjectId == "")
            gcpProjectId = "census-demos";

        // Create the Stackdriver stats exporter
        StackdriverStatsExporter.createAndRegister(
                StackdriverStatsConfiguration.builder()
                .setProjectId(gcpProjectId)
                .setExportInterval(Duration.create(12, 0))
                .build());

        // Next create the Stackdriver trace exporter
        StackdriverTraceExporter.createAndRegister(
                StackdriverTraceConfiguration.builder()
                .setProjectId(gcpProjectId)
                .build());
    }
}
