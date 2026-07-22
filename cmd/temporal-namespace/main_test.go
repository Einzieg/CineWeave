package main

import (
	"context"
	"errors"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	operatorservice "go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/serviceerror"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
)

type fakeNamespaceService struct {
	describeErr error
	registerErr error
	registered  *workflowservice.RegisterNamespaceRequest
}

type fakeSearchAttributeService struct {
	custom map[string]enumspb.IndexedValueType
	added  *operatorservice.AddSearchAttributesRequest
}

func (f *fakeSearchAttributeService) ListSearchAttributes(context.Context, *operatorservice.ListSearchAttributesRequest, ...grpc.CallOption) (*operatorservice.ListSearchAttributesResponse, error) {
	return &operatorservice.ListSearchAttributesResponse{CustomAttributes: f.custom}, nil
}

func (f *fakeSearchAttributeService) AddSearchAttributes(_ context.Context, request *operatorservice.AddSearchAttributesRequest, _ ...grpc.CallOption) (*operatorservice.AddSearchAttributesResponse, error) {
	f.added = request
	return &operatorservice.AddSearchAttributesResponse{}, nil
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

func TestEnsureSearchAttributesAddsOnlyMissingKeywords(t *testing.T) {
	service := &fakeSearchAttributeService{custom: map[string]enumspb.IndexedValueType{
		"ProjectId": enumspb.INDEXED_VALUE_TYPE_KEYWORD,
	}}
	added, err := ensureSearchAttributes(context.Background(), service, "default", cineWeaveSearchAttributes)
	if err != nil {
		t.Fatalf("ensure search attributes: %v", err)
	}
	if added != len(cineWeaveSearchAttributes)-1 || service.added == nil || service.added.Namespace != "default" {
		t.Fatalf("added=%d request=%+v", added, service.added)
	}
	if _, exists := service.added.SearchAttributes["ProjectId"]; exists {
		t.Fatal("existing search attribute was submitted again")
	}
}

func TestEnsureSearchAttributesRejectsTypeDrift(t *testing.T) {
	service := &fakeSearchAttributeService{custom: map[string]enumspb.IndexedValueType{
		"ProjectId": enumspb.INDEXED_VALUE_TYPE_TEXT,
	}}
	if _, err := ensureSearchAttributes(context.Background(), service, "default", cineWeaveSearchAttributes); err == nil {
		t.Fatal("search attribute type drift was accepted")
	}
}
