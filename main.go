package swiftfiatbackend

import (
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/api"
)

func main() {
	fmt.Println("Hello SwiftFiat")

	server := api.NewServer(".")
	server.Start(8000)
}
