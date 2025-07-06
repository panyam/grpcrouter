module github.com/panyam/grpcrouter/cmd/protoc-gen-grpcrouter-impl

go 1.24.0

replace github.com/panyam/grpcrouter => ../../

require (
	github.com/panyam/grpcrouter v0.0.0-00010101000000-000000000000
	google.golang.org/protobuf v1.36.6
)

require (
	golang.org/x/net v0.30.0 // indirect
	golang.org/x/sys v0.26.0 // indirect
	golang.org/x/text v0.19.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20241219192143-6b3ec007d9bb // indirect
	google.golang.org/grpc v1.69.4 // indirect
)
