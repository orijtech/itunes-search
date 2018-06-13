package io.mediasearch.search;

import com.google.protobuf.CodedOutputStream;

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;

import io.mediasearch.search.Defs.Request;
import io.mediasearch.search.Defs.Response;
import io.mediasearch.search.SearchGrpc;

import io.opencensus.common.Duration;
import io.opencensus.common.Scope;
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

import java.io.BufferedReader;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStreamReader;
import java.util.concurrent.TimeUnit;
import java.nio.charset.StandardCharsets;

import redis.clients.jedis.Jedis;
import redis.clients.jedis.Observability;
import redis.clients.jedis.params.SetParams;

public class MediasearchClient {
    private final ManagedChannel channel;
    private final SearchGrpc.SearchBlockingStub stub;

    private static final Tracer tracer = Tracing.getTracer();
    private static final Jedis jedis = new Jedis("localhost");
    private static final SetParams _3hoursExpiryInSeconds = SetParams.setParams().ex(3 * 60 * 60);
    private static final String utf8 = StandardCharsets.UTF_8.toString();

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

    public Response search(Request req) {
        Span span = this.tracer.spanBuilder("searching")
            .setRecordEvents(true)
            .startSpan();

        try (Scope scopeSpan = tracer.withSpan(span)) {
            String query = req.getQuery();
            String found = jedis.get(query);
            if (found != null && found != "") {
                // Then parse the message from the memoized result
                try {
                    Response resp = deserialize(found);
                    if (resp != null) {
                        span.addAnnotation("Cache hit");
                        return resp;
                    }
                } catch(Exception e) {
                    // If we failed to deserialize, just fallthrough and
                    // continue to instead fetch -- treat it like a cache miss.
                    System.err.println("While deserializing got error: " + e.toString());
                }
            }

            // Otherwise this is a cache miss, now query then insert the result
            span.addAnnotation("Cache miss");
            Response resp = this.stub.iTunesSearchNonStreaming(req);

            // And now to retrieve the serialized blob
            String serialized = null;

            try {
                serialized = this.serialize(resp);
            } catch (IOException e) {
                // It's not a problem if we've failed to cache the response or if
                // the Redis connection fails -- memoizations is just a nice to have.
                // Just ensure that we give back to the user the response.
                System.err.println("Encountered an exception while serializing " + e.toString());
            }

            if (serialized != null && serialized.length() != 0) {
                // To ensure that items don't go stale forever and
                // to prune out wasteful storage, let's store them for 3 hours
                jedis.set(query, serialized, _3hoursExpiryInSeconds);
            }
            return resp;
        } finally {
            span.end();
        }
    }

    public String serialize(Response resp) throws IOException {
        ByteArrayOutputStream bs = new ByteArrayOutputStream();
        CodedOutputStream cos = CodedOutputStream.newInstance(bs, resp.getSerializedSize());
        resp.writeTo(cos);
        cos.flush();
        // System.out.println("\033[33mBytesWritten " + cos.getTotalBytesWritten() + "\033[00m\nsupposedBytesWritten: \033[00m");
        return bs.toString(utf8);
    }

    public Response deserialize(String data) throws IOException {
        return Response.parseFrom(data.getBytes(utf8));
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

                try (Scope scopeSpan = tracer.withSpan(span)) {
                    Request req = Request.newBuilder().setQuery(query).build();
                    Response response = client.search(req);
                }
                span.end();
                if (response != null)
                    System.out.println("< " + response.toString());
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
