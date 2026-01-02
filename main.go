package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"os"

	api "github.com/danielvollbro/gohl-api"
	"github.com/luthermonson/go-proxmox"
)

func main() {
	url := os.Getenv("GOHL_CONFIG_URL")
	tokenID := os.Getenv("GOHL_CONFIG_TOKEN_ID")
	secret := os.Getenv("GOHL_CONFIG_SECRET")

	if url == "" || tokenID == "" || secret == "" {
		fmt.Fprintf(os.Stderr, "Error: Missing Proxmox configuration (URL, USER, TOKEN)\n")
		os.Exit(1)
	}

	insecureClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	client := proxmox.NewClient(url,
		proxmox.WithClient(insecureClient),
		proxmox.WithAPIToken(tokenID, secret),
	)

	ctx := context.Background()
	version, err := client.Version(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Proxmox: %v\n", err)
		os.Exit(1)
	}

	checks := runChecks(ctx, client, version)

	report := api.ScanReport{
		PluginID: "provider-proxmox",
		Checks:   checks,
	}
	api.PrintReport(report)
}

func runChecks(ctx context.Context, client *proxmox.Client, version *proxmox.Version) []api.CheckResult {
	var checks []api.CheckResult

	checks = append(checks, api.CheckResult{
		ID:          "PVE-VER",
		Name:        "Proxmox Version",
		Description: fmt.Sprintf("Connected to Proxmox %s", version.Release),
		Passed:      true,
		Score:       5,
		MaxScore:    5,
	})

	nodes, err := client.Nodes(ctx)
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

	for _, nodeStatus := range nodes {
		pveNode, err := client.Node(ctx, nodeStatus.Node)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load node object for %s: %v\n", nodeStatus.Node, err)
			continue
		}

		storages, err := pveNode.Storages(ctx)
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
						ID:          fmt.Sprintf("PVE-DISK-%s-%s", nodeStatus.Node, storage.Storage),
						Name:        fmt.Sprintf("Storage: %s on %s", storage.Storage, nodeStatus.Node),
						Description: fmt.Sprintf("Usage: %.1f%% (%d GB free)", percent, (storage.Total-storage.Used)/1024/1024/1024),
						Passed:      passed,
						Score:       score,
						MaxScore:    10,
						Remediation: "Expand storage or delete old backups/ISOs.",
					})
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "Failed to read storage for node %s: %v\n", nodeStatus.Node, err)
		}
	}

	return checks
}
