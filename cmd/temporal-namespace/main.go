package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	operatorservice "go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/serviceerror"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/durationpb"
)

type namespaceService interface {
	DescribeNamespace(context.Context, *workflowservice.DescribeNamespaceRequest, ...grpc.CallOption) (*workflowservice.DescribeNamespaceResponse, error)
	RegisterNamespace(context.Context, *workflowservice.RegisterNamespaceRequest, ...grpc.CallOption) (*workflowservice.RegisterNamespaceResponse, error)
}

type searchAttributeService interface {
	ListSearchAttributes(context.Context, *operatorservice.ListSearchAttributesRequest, ...grpc.CallOption) (*operatorservice.ListSearchAttributesResponse, error)
	AddSearchAttributes(context.Context, *operatorservice.AddSearchAttributesRequest, ...grpc.CallOption) (*operatorservice.AddSearchAttributesResponse, error)
}

var cineWeaveSearchAttributes = map[string]enumspb.IndexedValueType{
	"ProjectId":              enumspb.INDEXED_VALUE_TYPE_KEYWORD,
	"ProductionGenerationId": enumspb.INDEXED_VALUE_TYPE_KEYWORD,
	"EpisodeId":              enumspb.INDEXED_VALUE_TYPE_KEYWORD,
	"ProfileVersionId":       enumspb.INDEXED_VALUE_TYPE_KEYWORD,
	"RebuildId":              enumspb.INDEXED_VALUE_TYPE_KEYWORD,
}

func main() {
	address := environment("TEMPORAL_ADDRESS", "temporal:7233")
	namespace := environment("TEMPORAL_NAMESPACE", "default")
	retention, err := time.ParseDuration(environment("TEMPORAL_NAMESPACE_RETENTION", "72h"))
	if err != nil || retention <= 0 {
		log.Fatal("TEMPORAL_NAMESPACE_RETENTION must be a positive duration")
	}
	temporalClient, err := client.Dial(client.Options{HostPort: address, Namespace: namespace})
	if err != nil {
		log.Fatal(err)
	}
	defer temporalClient.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	created, err := ensureNamespace(ctx, temporalClient.WorkflowService(), namespace, retention)
	if err != nil {
		log.Fatal(err)
	}
	if created {
		log.Printf("Temporal namespace %s created", namespace)
	} else {
		log.Printf("Temporal namespace %s already exists", namespace)
	}
	added, err := ensureSearchAttributes(ctx, temporalClient.OperatorService(), namespace, cineWeaveSearchAttributes)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Temporal namespace %s search attributes ready; added=%d", namespace, added)
}

func ensureNamespace(ctx context.Context, service namespaceService, namespace string, retention time.Duration) (bool, error) {
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return false, fmt.Errorf("Temporal namespace is required")
	}
	_, err := service.DescribeNamespace(ctx, &workflowservice.DescribeNamespaceRequest{Namespace: namespace})
	if err == nil {
		return false, nil
	}
	var notFound *serviceerror.NamespaceNotFound
	if !errors.As(err, &notFound) {
		return false, fmt.Errorf("describe Temporal namespace %s: %w", namespace, err)
	}
	_, err = service.RegisterNamespace(ctx, &workflowservice.RegisterNamespaceRequest{
		Namespace:                        namespace,
		Description:                      "CineWeave workflow namespace",
		WorkflowExecutionRetentionPeriod: durationpb.New(retention),
	})
	if err != nil {
		var alreadyExists *serviceerror.NamespaceAlreadyExists
		if errors.As(err, &alreadyExists) {
			return false, nil
		}
		return false, fmt.Errorf("register Temporal namespace %s: %w", namespace, err)
	}
	return true, nil
}

func ensureSearchAttributes(
	ctx context.Context,
	service searchAttributeService,
	namespace string,
	required map[string]enumspb.IndexedValueType,
) (int, error) {
	response, err := service.ListSearchAttributes(ctx, &operatorservice.ListSearchAttributesRequest{Namespace: namespace})
	if err != nil {
		return 0, fmt.Errorf("list Temporal search attributes for %s: %w", namespace, err)
	}
	missing := make(map[string]enumspb.IndexedValueType)
	for name, valueType := range required {
		if existing, ok := response.CustomAttributes[name]; ok {
			if existing != valueType {
				return 0, fmt.Errorf("Temporal search attribute %s has type %s, want %s", name, existing, valueType)
			}
			continue
		}
		missing[name] = valueType
	}
	if len(missing) == 0 {
		return 0, nil
	}
	if _, err := service.AddSearchAttributes(ctx, &operatorservice.AddSearchAttributesRequest{
		Namespace: namespace, SearchAttributes: missing,
	}); err != nil {
		return 0, fmt.Errorf("add Temporal search attributes for %s: %w", namespace, err)
	}
	return len(missing), nil
}

func environment(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
