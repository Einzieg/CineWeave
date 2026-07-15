package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.temporal.io/api/serviceerror"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
)

type fakeNamespaceService struct {
	describeErr error
	registerErr error
	registered  *workflowservice.RegisterNamespaceRequest
}

func (f *fakeNamespaceService) DescribeNamespace(context.Context, *workflowservice.DescribeNamespaceRequest, ...grpc.CallOption) (*workflowservice.DescribeNamespaceResponse, error) {
	return &workflowservice.DescribeNamespaceResponse{}, f.describeErr
}

func (f *fakeNamespaceService) RegisterNamespace(_ context.Context, request *workflowservice.RegisterNamespaceRequest, _ ...grpc.CallOption) (*workflowservice.RegisterNamespaceResponse, error) {
	f.registered = request
	return &workflowservice.RegisterNamespaceResponse{}, f.registerErr
}

func TestEnsureNamespaceReturnsWhenPresent(t *testing.T) {
	service := &fakeNamespaceService{}
	created, err := ensureNamespace(context.Background(), service, "default", 72*time.Hour)
	if err != nil || created || service.registered != nil {
		t.Fatalf("created=%v registered=%v err=%v", created, service.registered, err)
	}
}

func TestEnsureNamespaceRegistersWhenMissing(t *testing.T) {
	service := &fakeNamespaceService{describeErr: serviceerror.NewNamespaceNotFound("default")}
	created, err := ensureNamespace(context.Background(), service, "default", 72*time.Hour)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if service.registered == nil || service.registered.Namespace != "default" || service.registered.WorkflowExecutionRetentionPeriod.AsDuration() != 72*time.Hour {
		t.Fatalf("request=%+v", service.registered)
	}
}

func TestEnsureNamespaceDoesNotHideDescribeFailure(t *testing.T) {
	service := &fakeNamespaceService{describeErr: errors.New("unavailable")}
	if _, err := ensureNamespace(context.Background(), service, "default", 72*time.Hour); err == nil {
		t.Fatal("describe failure should be returned")
	}
}
