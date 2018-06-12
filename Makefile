protoc: 
	# Generate protobuf definitions for Go. Those for Java are handled by the Maven plugin
	protoc -I src/main/proto/ src/main/proto/defs.proto --go_out=plugins=grpc:rpc
