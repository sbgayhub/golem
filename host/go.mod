module github.com/sbgayhub/golem/host

go 1.26.0

require (
	github.com/duke-git/lancet/v2 v2.3.9
	github.com/fsnotify/fsnotify v1.10.1
	github.com/pelletier/go-toml/v2 v2.4.3
	github.com/phsym/console-slog v0.3.1
	github.com/sbgayhub/golem/sdk v0.1.0
	golem v0.0.0-00010101000000-000000000000
	google.golang.org/protobuf v1.36.11
)

replace github.com/sbgayhub/golem/sdk => ../sdk

//replace golem => ../../wechat-refactor/golem
replace golem => ../../../Project/golem_core

require (
	github.com/fatih/color v1.19.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-plugin v1.8.0 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/labstack/echo/v5 v5.3.1 // indirect
	github.com/magefile/mage v1.17.2 // indirect
	github.com/mattn/go-colorable v0.1.15 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/oklog/run v1.2.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	golang.org/x/exp v0.0.0-20260727155853-b88d891fe743 // indirect
	golang.org/x/image v0.44.0 // indirect
	golang.org/x/net v0.57.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260729162451-8efbd57d26e0 // indirect
	google.golang.org/grpc v1.83.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
)
