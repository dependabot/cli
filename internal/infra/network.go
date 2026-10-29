package infra

import (
	"context"
	"errors"
	"fmt"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/moby/moby/pkg/namesgenerator"
)

type Networks struct {
	NoInternet     network.CreateResponse
	Internet       network.CreateResponse
	cli            *client.Client
	noInternetName string
	internetName   string
}

func NewNetworks(ctx context.Context, cli *client.Client) (*Networks, error) {
	const bridge = "bridge"
	var createdNoInternetID string

	noInternetName := namesgenerator.GetRandomName(1)
	noInternet, err := cli.NetworkCreate(ctx, noInternetName, network.CreateOptions{
		Internal: true,
		Driver:   bridge,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create no-internet network: %w", err)
	}
	createdNoInternetID = noInternet.ID

	internetName := namesgenerator.GetRandomName(1)
	internet, err := cli.NetworkCreate(ctx, internetName, network.CreateOptions{
		Driver: bridge,
	})
	if err != nil {
		if removeErr := cli.NetworkRemove(context.Background(), createdNoInternetID); removeErr != nil {
			return nil, fmt.Errorf("failed to create internet network: %w (also failed to remove no-internet network %s: %v)", err, createdNoInternetID, removeErr)
		}
		return nil, fmt.Errorf("failed to create internet network: %w", err)
	}

	return &Networks{
		cli:            cli,
		NoInternet:     noInternet,
		Internet:       internet,
		noInternetName: noInternetName,
		internetName:   internetName,
	}, nil
}

func (n *Networks) Close() error {
	var closeErr error

	if err := n.cli.NetworkRemove(context.Background(), n.NoInternet.ID); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("failed to remove no-internet network %s: %w", n.NoInternet.ID, err))
	}
	if err := n.cli.NetworkRemove(context.Background(), n.Internet.ID); err != nil {
		closeErr = errors.Join(closeErr, fmt.Errorf("failed to remove internet network %s: %w", n.Internet.ID, err))
	}

	return closeErr
}
