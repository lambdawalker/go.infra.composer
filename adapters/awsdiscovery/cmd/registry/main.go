package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/lambdawalker/go.infra.composer/adapters/awsdiscovery/registry"
)

func main() {
	table := os.Getenv("DISCOVERY_TABLE")
	if table == "" {
		log.Fatal("DISCOVERY_TABLE is required")
	}
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		log.Fatal("load discovery AWS configuration")
	}
	store := registry.Store{DB: dynamodb.NewFromConfig(cfg), Table: table}
	lambda.Start(func(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
		return registry.Handle(ctx, event, store), nil
	})
}
