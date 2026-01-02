package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"strings"

	api "github.com/danielvollbro/gohl-api"
	"github.com/luthermonson/go-proxmox"
)

type ClusterClient interface {
	GetVersion(ctx context.Context) (string, error)
	GetNodes(ctx context.Context) ([]*proxmox.NodeStatus, error)
	GetNodeStorage(ctx context.Context, nodeName string) ([]*proxmox.Storage, error)
}

type ProxmoxClient struct {
	client *proxmox.Client
}

func (p *ProxmoxClient) GetVersion(ctx context.Context) (string, error) {
	v, err := p.client.Version(ctx)
	if err != nil {
		return "", err
	}
	return v.Release, nil
}

func (p *ProxmoxClient) GetNodes(ctx context.Context) ([]*proxmox.NodeStatus, error) {
	return p.client.Nodes(ctx)
}

func (p *ProxmoxClient) GetNodeStorage(ctx context.Context, nodeName string) ([]*proxmox.Storage, error) {
	node, err := p.client.Node(ctx, nodeName)
	if err != nil {
		return nil, err
	}
	return node.Storages(ctx)
}

func main() {
	url := os.Getenv("GOHL_CONFIG_URL")
	tokenID := os.Getenv("GOHL_CONFIG_TOKEN_ID")
	secret := os.Getenv("GOHL_CONFIG_SECRET")

	if url == "" || tokenID == "" || secret == "" {
		fmt.Fprintf(os.Stderr, "Error: Missing Proxmox configuration (URL, USER, TOKEN)\n")
		os.Exit(1)
	}

	if !strings.HasSuffix(url, "/api2/json") {
		url = strings.TrimRight(url, "/") + "/api2/json"
	}

	insecureClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	client := proxmox.NewClient(url,
		proxmox.WithHTTPClient(insecureClient),
		proxmox.WithAPIToken(tokenID, secret),
	)

	wrapper := &ProxmoxClient{client: client}

	ctx := context.Background()
	checks := runChecks(ctx, wrapper)

	api.PrintReport(api.ScanReport{
		PluginID: "provider-proxmox",
		Checks:   checks,
	})
}

func runChecks(ctx context.Context, client ClusterClient) []api.CheckResult {
	var checks []api.CheckResult

	version, err := client.GetVersion(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Proxmox: %v\n", err)
		os.Exit(1)
	}

	checks = append(checks, api.CheckResult{
		ID:          "PVE-VER",
		Name:        "Proxmox Version",
		Description: fmt.Sprintf("Connected to Proxmox %s", version),
		Passed:      true,
		Score:       5,
		MaxScore:    5,
	})

	nodes, err := client.GetNodes(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch nodes: %v\n", err)
		return checks
	}

	for _, node := range nodes {
		isOnline := node.Status == "online"
		score := 0
		if isOnline {
			score = 20
		}

		checks = append(checks, api.CheckResult{
			ID:          fmt.Sprintf("PVE-NODE-%s", node.Node),
			Name:        fmt.Sprintf("Node Status: %s", node.Node),
			Description: fmt.Sprintf("Checking if node %s is online. CPU: %.1f%%", node.Node, node.CPU*100),
			Passed:      isOnline,
			Score:       score,
			MaxScore:    20,
			Remediation: "Start the node or check network connectivity.",
		})
	}

	for _, node := range nodes {
		storages, err := client.GetNodeStorage(ctx, node.Node)
		if err == nil {
			for _, storage := range storages {
				if storage.Type == "zfspool" || storage.Type == "dir" || storage.Type == "lvm" || storage.Type == "nfs" {

					percent := 0.0
					if storage.Total > 0 {
						percent = (float64(storage.Used) / float64(storage.Total)) * 100
					}

					passed := percent < 90.0
					score := 0
					if passed {
						score = 10
					}

					checks = append(checks, api.CheckResult{
						ID:          fmt.Sprintf("PVE-DISK-%s-%s", node.Node, storage.Storage),
						Name:        fmt.Sprintf("Storage: %s on %s", storage.Storage, node.Node),
						Description: fmt.Sprintf("Usage: %.1f%% (%d GB free)", percent, (storage.Total-storage.Used)/1024/1024/1024),
						Passed:      passed,
						Score:       score,
						MaxScore:    10,
						Remediation: "Expand storage or delete old backups/ISOs.",
					})
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "Failed to read storage for node %s: %v\n", node.Node, err)
		}
	}

	return checks
}
