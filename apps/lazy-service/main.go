// Command lazy-service starts a child HTTP process on the first request and
// stops it when idle. See package lazy.
package main

import (
	"os"

	"github.com/open-neko/lazy-service/lazy"
)

func main() {
	lazy.Main(os.Args[1:])
}
