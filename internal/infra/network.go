package infra

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/namesgenerator"
)

type Networks struct {
	NoInternet     types.NetworkCreateResponse
	Internet       types.NetworkCreateResponse
	cli            *client.Client
	noInternetName string
	internetName   string
}

func NewNetworks(ctx context.Context, cli *client.Client) (*Networks, error) {
	const bridge = "bridge"

	noInternetName := namesgenerator.GetRandomName(1)
	noInternet, err := cli.NetworkCreate(ctx, noInternetName, types.NetworkCreate{
		Internal: true,
		Driver:   bridge,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create no-internet network: %w", err)
	}

	internetName := namesgenerator.GetRandomName(1)
	internet, err := cli.NetworkCreate(ctx, internetName, types.NetworkCreate{
		Driver: bridge,
	})
	if err != nil {
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
	if err := n.cli.NetworkRemove(context.Background(), n.NoInternet.ID); err != nil {
		return err
	}
	if err := n.cli.NetworkRemove(context.Background(), n.Internet.ID); err != nil {
		return err
	}
	return nil
}
