start: # start-server
	CompileDaemon -command="./swiftfiat_backend"

test: # run tests
	go test -v -cover ./...