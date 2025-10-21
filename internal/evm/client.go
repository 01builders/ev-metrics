package evm

import (
	"context"
	"fmt"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
)

// Client is a wrapper around the Ethereum client.
type Client struct {
	*ethclient.Client
	logger zerolog.Logger
}

func NewClient(ctx context.Context, wsURL string, logger zerolog.Logger) (*Client, error) {
	client, err := ethclient.DialContext(ctx, wsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to EVM client: %w", err)
	}

	return &Client{
		Client: client,
		logger: logger.With().Str("component", "evm_client").Logger(),
	}, nil
}

// SubscribeNewHead subscribes to new block headers
//func (c *Client) SubscribeNewHead(ctx context.Context, headers chan<- *types.Header) error {
//	sub, err := c.Client.SubscribeNewHead(ctx, headers)
//	if err != nil {
//		return fmt.Errorf("failed to subscribe to new headers: %w", err)
//	}
//
//	c.logger.Info().Msg("subscribed to new block headers")
//
//	// Wait for subscription errors or context cancellation
//	select {
//	case err := <-sub.Err():
//		return fmt.Errorf("subscription error: %w", err)
//	case <-ctx.Done():
//		sub.Unsubscribe()
//		return ctx.Err()
//	}
//}
