package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/lambdawalker/go.infra.composer/discovery"
)

type DynamoDB interface {
	Scan(context.Context, *dynamodb.ScanInput, ...func(*dynamodb.Options)) (*dynamodb.ScanOutput, error)
	UpdateItem(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
}

type Store struct {
	DB    DynamoDB
	Table string
}

func (s Store) List(ctx context.Context) ([]discovery.Entry, error) {
	var entries []discovery.Entry
	var start map[string]types.AttributeValue
	for {
		result, err := s.DB.Scan(ctx, &dynamodb.ScanInput{
			TableName: aws.String(s.Table), ExclusiveStartKey: start, ConsistentRead: aws.Bool(true),
			ProjectionExpression: aws.String("published"),
		})
		if err != nil {
			return nil, err
		}
		for _, item := range result.Items {
			published, ok := item["published"].(*types.AttributeValueMemberS)
			if !ok {
				continue
			}
			var entry discovery.Entry
			if json.Unmarshal([]byte(published.Value), &entry) != nil || !discovery.ValidEnvironment(entry.ID) ||
				entry.Revision < 1 || entry.Configuration.Validate() != nil {
				return nil, errors.New("invalid stored discovery entry")
			}
			entries = append(entries, entry)
		}
		start = result.LastEvaluatedKey
		if len(start) == 0 {
			return entries, nil
		}
	}
}

func (s Store) Begin(ctx context.Context, id string) (int64, error) {
	result, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.Table), Key: key(id),
		UpdateExpression:          aws.String("ADD generation :one"),
		ExpressionAttributeValues: map[string]types.AttributeValue{":one": number(1)},
		ReturnValues:              types.ReturnValueUpdatedNew,
	})
	if err != nil {
		return 0, err
	}
	generation, ok := result.Attributes["generation"].(*types.AttributeValueMemberN)
	if !ok {
		return 0, errors.New("missing discovery generation")
	}
	n, err := strconv.ParseInt(generation.Value, 10, 64)
	if err != nil || n < 1 {
		return 0, errors.New("invalid discovery generation")
	}
	return n, nil
}

func (s Store) Publish(ctx context.Context, entry discovery.Entry) error {
	value, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return s.update(ctx, entry.ID, entry.Revision, "SET published = :entry", map[string]types.AttributeValue{
		":entry": &types.AttributeValueMemberS{Value: string(value)},
	})
}

func (s Store) Retire(ctx context.Context, id string, revision int64) error {
	return s.update(ctx, id, revision, "REMOVE published", make(map[string]types.AttributeValue))
}

func (s Store) update(ctx context.Context, id string, revision int64, update string, values map[string]types.AttributeValue) error {
	values[":generation"] = number(revision)
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.Table), Key: key(id), UpdateExpression: aws.String(update),
		ConditionExpression: aws.String("generation = :generation"), ExpressionAttributeValues: values,
	})
	var superseded *types.ConditionalCheckFailedException
	if errors.As(err, &superseded) {
		return discovery.ErrSuperseded
	}
	return err
}

func key(id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: id}}
}

func number(value int64) *types.AttributeValueMemberN {
	return &types.AttributeValueMemberN{Value: fmt.Sprint(value)}
}
