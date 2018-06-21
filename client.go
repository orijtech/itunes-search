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
	"os"
	"reflect"
	"strings"
	"time"

	proto "github.com/golang/protobuf/proto"
	"google.golang.org/grpc"

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

	cc, err := grpc.Dial(*serverAddr, grpc.WithInsecure())
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

var blankResponse = new(rpc.Response)

func performSearch(ctx context.Context, searchClient rpc.SearchClient, req *rpc.Request) (*rpc.Response, error) {
	redisConn := redisPool.GetWithContext(ctx)
	defer redisConn.Close()

	// Check the cache
	cached, err := redis.Bytes(redisConn.Do("GET", req.Query))
	if err == nil && len(cached) > 0 {
		res := new(rpc.Response)
		if err := proto.Unmarshal(cached, res); err == nil && !reflect.DeepEqual(res, blankResponse) {
			return res, nil
		} else if err != nil {
			// Log the error but then treat it as a cache miss
			log.Printf("Unmarshaling error: %v", err)
		}
	}

	// Cache miss so now performing actual search
	res, err := searchClient.ITunesSearchNonStreaming(ctx, req)
	if err != nil {
		return nil, err
	}

	blob, err := proto.Marshal(res)
	if err != nil {
		// Not an error that should not show the user their results
		return res, nil
	}

	if _, err := redisConn.Do("SETEX", req.Query, _3HoursInSeconds, blob); err != nil {
		log.Printf("Caching error: %v", err)
	}
	return res, nil
}
