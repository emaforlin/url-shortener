package main

import (
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/awslabs/aws-lambda-go-api-proxy/httpadapter"
	"github.com/emaforlin/url-shortener/internal/bootstrap"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	app, err := bootstrap.New(version)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}

	lambda.Start(httpadapter.NewV2(app.Handler).ProxyWithContext)
	return nil
}
