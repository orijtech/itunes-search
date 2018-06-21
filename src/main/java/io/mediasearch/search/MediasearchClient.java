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

package io.mediasearch.search;

import com.google.protobuf.CodedOutputStream;

import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;

import io.mediasearch.search.Defs.Request;
import io.mediasearch.search.Defs.Response;
import io.mediasearch.search.SearchGrpc;

import java.io.BufferedReader;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStreamReader;
import java.net.URI;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.TimeUnit;

import redis.clients.jedis.Jedis;
import redis.clients.jedis.params.SetParams;

public class MediasearchClient {
    private final ManagedChannel channel;
    private final SearchGrpc.SearchBlockingStub stub;

    private static final String redisHost = envOrAlternative("ITUNESSEARCH_REDIS_SERVER_HOST", "localhost");
    private static final Jedis jedis = new Jedis(redisHost);

    private static final SetParams _3hoursExpiryInSeconds = SetParams.setParams().ex(3 * 60 * 60);
    private static final String utf8 = StandardCharsets.UTF_8.toString();

    public MediasearchClient(String grpcServerHost, int grpcServerPort) {
        String redisPassword = envOrAlternative("ITUNESSEARCH_REDIS_PASSWORD", "");
        try {
            if (redisPassword != null && redisPassword != "") {
                jedis.auth(redisPassword);
            }
        } catch (Exception e) {
            // Perhaps this is a NoAuth Set exception, so that's alright
            // TODO: Actually check for NoAuth exception or throw
        }

        // Create the gRPC channel to the server.
        this.channel = ManagedChannelBuilder.forAddress(grpcServerHost, grpcServerPort)
            .usePlaintext(true)
            .build();
        this.stub = SearchGrpc.newBlockingStub(this.channel);
    }

    public void shutdown() throws InterruptedException {
        this.channel.shutdown().awaitTermination(4, TimeUnit.SECONDS);
    }

    public Response search(Request req) {
            String query = req.getQuery();
            String found = jedis.get(query);
            if (found != null && found != "") {
                // Then parse the message from the memoized result
                try {
                    Response resp = deserialize(found);
                    if (resp != null)
                        return resp;
                } catch(Exception e) {
                    // If we failed to deserialize, just fallthrough and
                    // continue to instead fetch -- treat it like a cache miss.
                    System.err.println("While deserializing got error: " + e);
                }
            }

            // Otherwise this is a cache miss, now query then insert the result
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

            if (serialized != null && serialized != "") {
                // To ensure that items don't go stale forever and
                // to prune out wasteful storage, let's store them for 3 hours
                jedis.set(query, serialized, _3hoursExpiryInSeconds);
            }
            return resp;
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
            BufferedReader stdin = new BufferedReader(new InputStreamReader(System.in));
            while (true) {
                System.out.print("> ");
                System.out.flush();
                String query = stdin.readLine();

                Request req = Request.newBuilder().setQuery(query).build();
                Response response = client.search(req);
                System.out.println("< " + response);
            }
        } catch (Exception e) {
            System.err.println("Exception encountered: " + e);
        }
    }

    private static String envOrAlternative(String key, String ...alternatives) {
        String value = System.getenv().get(key);

        if (value == null || value == "") { // In this case, the environment variable is not set
            for (String alternative: alternatives) {
                if (alternative != null && alternative != "") {
                    value = alternative;
                    break;
                }
            }
        }

        return value;
    }
}
