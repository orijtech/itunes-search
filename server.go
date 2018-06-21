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

	"google.golang.org/grpc"

	"github.com/orijtech/itunes-search/rpc"
)

func main() {
	ln, err := net.Listen("tcp", ":9449")
	if err != nil {
		log.Fatalf("Failed to find an available address to bind a listener: %v", err)
	}
	defer ln.Close()

	srv := grpc.NewServer()
	rpc.RegisterSearchServer(srv, new(rpc.SearchBackend))
	log.Printf("Running gRPC server at: %q", ln.Addr())

	if err := srv.Serve(ln); err != nil {
		log.Fatalf("gRPC server error while serving: %v", err)
	}
}
