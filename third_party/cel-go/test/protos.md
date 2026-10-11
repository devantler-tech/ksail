## How to regenerate protos

Install protoc and protoc-go plugin

https://protobuf.dev/installation/
https://protobuf.dev/getting-started/gotutorial/

Run:
```
protoc --proto_path=$(pwd) --go_out=$(pwd) --go_opt=paths=source_relative test/proto3pb/test_import.proto test/proto3pb/test_all_types.proto

protoc --proto_path=$(pwd) --go_out=$(pwd) --go_opt=paths=source_relative test/proto2pb/test_extensions.proto test/proto2pb/test_all_types.proto
```