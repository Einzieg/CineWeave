package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

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
		return
	}
	log.Printf("Temporal namespace %s already exists", namespace)
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

func environment(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
