module github.com/satori-protocol-go/satori-go/test/servercompat

go 1.25.4

require (
	github.com/go-chi/chi/v5 v5.2.3
	github.com/satori-protocol-go/satori-go v0.0.0
)

require (
	github.com/gorilla/websocket v1.5.3 // indirect
	golang.org/x/sync v0.20.0 // indirect
)

replace github.com/satori-protocol-go/satori-go => ../..
