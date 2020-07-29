dev:
	go run ./main.go
build: build_linux build_win build_macos
build_linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./bin/linux/sync ./main.go && upx ./bin/linux/sync
build_macos:
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o ./bin/macos/sync ./main.go && upx ./bin/macos/sync
build_win:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o ./bin/windows/sync.exe ./main.go
