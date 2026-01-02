package main

import (
	"context"
	"testing"

	"github.com/luthermonson/go-proxmox"
)

type MockClient struct {
	Version     string
	Nodes       []*proxmox.NodeStatus
	Storages    map[string][]*proxmox.Storage
	ShouldError bool
}

func (m *MockClient) GetVersion(ctx context.Context) (string, error) {
	return m.Version, nil
}

func (m *MockClient) GetNodes(ctx context.Context) ([]*proxmox.NodeStatus, error) {
	return m.Nodes, nil
}

func (m *MockClient) GetNodeStorage(ctx context.Context, nodeName string) ([]*proxmox.Storage, error) {
	return m.Storages[nodeName], nil
}

func TestRunChecks_HealthyCluster(t *testing.T) {
	mock := &MockClient{
		Version: "8.1.0",
		Nodes: []*proxmox.NodeStatus{
			{Node: "pve-01", Status: "online", CPU: 0.1},
		},
		Storages: map[string][]*proxmox.Storage{
			"pve-01": {
				{Storage: "local-zfs", Type: "zfspool", Total: 1000, Used: 500}, // 50% usage
			},
		},
	}

	// 2. Run Analysis
	checks := runChecks(context.Background(), mock)

	// 3. Assertions
	if len(checks) != 3 {
		t.Fatalf("Expected 3 checks, got %d", len(checks))
	}

	if checks[0].ID != "PVE-VER" || !checks[0].Passed {
		t.Error("Version check failed")
	}

	if checks[1].ID != "PVE-NODE-pve-01" || !checks[1].Passed {
		t.Error("Node check failed")
	}

	if checks[2].ID != "PVE-DISK-pve-01-local-zfs" || !checks[2].Passed {
		t.Error("Storage check failed")
	}
}

func TestRunChecks_FullDisk(t *testing.T) {
	mock := &MockClient{
		Nodes: []*proxmox.NodeStatus{{Node: "pve-01", Status: "online"}},
		Storages: map[string][]*proxmox.Storage{
			"pve-01": {
				{Storage: "full-disk", Type: "dir", Total: 100, Used: 99}, // 99% usage
			},
		},
	}

	checks := runChecks(context.Background(), mock)

	storageCheck := checks[2]

	if storageCheck.Passed {
		t.Error("Storage check should fail on 99% usage")
	}
	if storageCheck.Score != 0 {
		t.Error("Score should be 0 for full disk")
	}
}
