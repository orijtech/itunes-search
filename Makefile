protoc: 
	# Generate protobuf definitions for Go. Those for Java are handled by the Maven plugin
	protoc -I src/main/proto/ src/main/proto/defs.proto --go_out=plugins=grpc:rpc
all: protoc java_client go_client go_server
	./bin/server &

java_client:
	mvn install

go_client:
	go build -o ./bin/client client.go

go_server:
	go build -o ./bin/server server.go
