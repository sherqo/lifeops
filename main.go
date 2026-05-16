package main

import (
	"log"

	"github.com/sherqo/lifeops/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
