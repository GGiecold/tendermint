package main

import (
	"fmt"
	"sort"
	"time"

	e2e "github.com/tendermint/tendermint/test/e2e/pkg"
)

func Start(testnet *e2e.Testnet) error {

	// Nodes are already sorted by name. Sort them by name then startAt,
	// which gives the overall order startAt, mode, name.
	nodeQueue := testnet.Nodes
	sort.SliceStable(nodeQueue, func(i, j int) bool {
		a, b := nodeQueue[i], nodeQueue[j]
		switch {
		case a.Mode == b.Mode:
			return false
		case a.Mode == e2e.ModeSeed:
			return true
		case a.Mode == e2e.ModeValidator && b.Mode == e2e.ModeFull:
			return true
		}
		return false
	})

	sort.SliceStable(nodeQueue, func(i, j int) bool {
		return nodeQueue[i].StartAt < nodeQueue[j].StartAt
	})
	if len(nodeQueue) == 0 {
		return fmt.Errorf("no nodes in testnet")
	}
	if nodeQueue[0].StartAt > 0 {
		return fmt.Errorf("no initial nodes in testnet")
	}

	// Start initial nodes (StartAt: 0)
	logger.Info("Starting initial network nodes...")
	for len(nodeQueue) > 0 && nodeQueue[0].StartAt == 0 {
		node := nodeQueue[0]
		nodeQueue = nodeQueue[1:]
		if err := execCompose(testnet.Dir, "up", "-d", node.Name); err != nil {
			return err
		}
		if _, err := waitForNode(node, 0, time.Minute); err != nil {
			return err
		}
		logger.Info(fmt.Sprintf("Node %v up on http://127.0.0.1:%v", node.Name, node.ProxyPort))
	}

	networkHeight := testnet.InitialHeight

	// Wait for initial height
	logger.Info("Waiting for initial height",
		"height", networkHeight,
		"nodes", len(testnet.Nodes)-len(nodeQueue),
		"pending", len(nodeQueue))

	block, blockID, err := waitForHeight(testnet, networkHeight)
	if err != nil {
		return err
	}

	// Update any state sync nodes with a trusted height and hash
	for _, node := range nodeQueue {
		if node.StateSync || node.Mode == e2e.ModeLight {
			err = UpdateConfigStateSync(node, block.Height, blockID.Hash.Bytes())
			if err != nil {
				return err
			}
		}
	}

	for _, node := range nodeQueue {
		if node.StartAt > networkHeight {
			// if we're starting a node that's ahead of
			// the last known height of the network, then
			// we should make sure that the rest of the
			// network has reached at least the height
			// that this node will start at before we
			// start the node.

			networkHeight = node.StartAt

			logger.Info("Waiting for network to advance before starting catch up node",
				"node", node.Name,
				"height", networkHeight)

			if _, _, err := waitForHeight(testnet, networkHeight); err != nil {
				return err
			}
		}

		logger.Info(fmt.Sprintf("Starting node %v at height %v...", node.Name, node.StartAt))
		if _, _, err := waitForHeight(testnet, node.StartAt); err != nil {
			return err
		}
		if err := execCompose(testnet.Dir, "up", "-d", node.Name); err != nil {
			return err
		}
		status, err := waitForNode(node, node.StartAt, 8*time.Minute)
		if err != nil {
			return err
		}
		logger.Info(fmt.Sprintf("Node %v up on http://127.0.0.1:%v at height %v",
			node.Name, node.ProxyPort, status.SyncInfo.LatestBlockHeight))
	}

	return nil
}
