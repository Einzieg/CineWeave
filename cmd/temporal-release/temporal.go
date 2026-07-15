package main

import (
	"context"
	"fmt"

	"go.temporal.io/sdk/client"
)

func openSDKHandle(_ context.Context, cfg commandConfig) (deploymentHandle, func(), error) {
	temporalClient, err := client.Dial(client.Options{
		HostPort:  cfg.address,
		Namespace: cfg.namespace,
		Identity:  cfg.identity,
	})
	if err != nil {
		return nil, nil, err
	}

	handle := temporalClient.WorkerDeploymentClient().GetHandle(cfg.deploymentName)
	if handle == nil {
		temporalClient.Close()
		return nil, nil, fmt.Errorf("Temporal returned no handle for deployment %q", cfg.deploymentName)
	}
	return handle, temporalClient.Close, nil
}
