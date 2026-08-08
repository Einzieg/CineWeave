package main

import (
	"context"
	"log"

	"github.com/Einzieg/cineweave/internal/agentworkerapp"
)

func main() {
	if err := agentworkerapp.Run(context.Background(), buildEditionRuntime); err != nil {
		log.Fatal(err)
	}
}
