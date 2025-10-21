package celestia

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	coreda "github.com/evstack/ev-node/core/da"
	"github.com/evstack/ev-node/da/jsonrpc"
	evnode "github.com/evstack/ev-node/types/pb/evnode/v1"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"
)

// Client is a small wrapper around the evnode celestia jsonrpc client.
type Client struct {
	*jsonrpc.Client
	logger zerolog.Logger
}

func NewClient(ctx context.Context, url, token string, logger zerolog.Logger) (*Client, error) {
	// Use ev-node's DA client (which connects to celestia-node)
	client, err := jsonrpc.NewClient(ctx, logger, url, token, 0.0, 1.0, 1970176)
	if err != nil {
		return nil, fmt.Errorf("failed to create celestia client: %w", err)
	}

	return &Client{
		Client: client,
		logger: logger.With().Str("component", "celestia_client").Logger(),
	}, nil
}

// GetBlobsAtHeight retrieves all blobs at a specific DA height and namespace
func (c *Client) GetBlobsAtHeight(ctx context.Context, daHeight uint64, namespace []byte) ([][]byte, error) {
	// get blob IDs
	result, err := c.DA.GetIDs(ctx, daHeight, namespace)
	if err != nil {
		if strings.Contains(err.Error(), "blob: not found") {
			return nil, nil // No blobs at this height
		}
		if strings.Contains(err.Error(), "future") {
			return nil, fmt.Errorf("DA height %d is in the future", daHeight)
		}
		return nil, fmt.Errorf("failed to get blob IDs: %w", err)
	}

	if result == nil || len(result.IDs) == 0 {
		return nil, nil
	}

	// get actual blob data
	blobs, err := c.DA.Get(ctx, result.IDs, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to get blobs: %w", err)
	}

	return blobs, nil
}

// VerifyBlobAtHeight verifies that a specific blob exists at the given DA height
// by computing its commitment and checking if it matches any commitment at that height
func (c *Client) VerifyBlobAtHeight(ctx context.Context, blob []byte, daHeight uint64, namespace []byte) (bool, error) {
	if len(blob) == 0 {
		return true, nil // empty blobs are valid (nothing to verify)
	}

	// compute commitment for our blob
	commitments, err := c.DA.Commit(ctx, [][]byte{blob}, namespace)
	if err != nil {
		return false, fmt.Errorf("failed to compute commitment: %w", err)
	}

	if len(commitments) == 0 {
		return false, fmt.Errorf("no commitment generated")
	}

	commitment := commitments[0]
	c.logger.Debug().
		Str("our_commitment", fmt.Sprintf("%x", commitment)).
		Int("commitment_size", len(commitment)).
		Int("blob_size", len(blob)).
		Uint64("da_height", daHeight).
		Msg("computed commitment for blob")

	// get all IDs at the DA height
	result, err := c.DA.GetIDs(ctx, daHeight, namespace)
	if err != nil {
		// TODO: don't check string, use concrete error type
		if strings.Contains(err.Error(), "blob: not found") {
			c.logger.Debug().Uint64("da_height", daHeight).Msg("no blobs found at DA height")
			return false, nil // no blobs at this height
		}
		return false, fmt.Errorf("failed to get IDs: %w", err)
	}

	if result == nil || len(result.IDs) == 0 {
		c.logger.Debug().Uint64("da_height", daHeight).Msg("no IDs returned")
		return false, nil
	}

	c.logger.Debug().
		Int("num_ids", len(result.IDs)).
		Uint64("da_height", daHeight).
		Msg("checking commitments from Celestia")

	// check if our commitment matches any commitment at this height
	// ID format: height (8 bytes) + commitment
	for i, id := range result.IDs {
		_, cmt, err := coreda.SplitID(id)
		if err != nil {
			c.logger.Warn().Err(err).Msg("failed to split ID")
			continue
		}

		if equal := bytes.Equal(commitment, cmt); equal {
			c.logger.Info().
				Int("blob_index", i).
				Str("cmt", fmt.Sprintf("%x", cmt)).
				Msg("found matching commitment")
			return true, nil
		}
	}

	c.logger.Warn().
		Str("commitment", fmt.Sprintf("%x", commitment)).
		Int("checked_blobs", len(result.IDs)).
		Msg("no matching commitment found")
	return false, nil
}

// VerifyDataBlobAtHeight verifies a data blob, accounting for the SignedData wrapper
// ev-node submits Data wrapped in SignedData, but the Store API returns unwrapped Data
func (c *Client) VerifyDataBlobAtHeight(ctx context.Context, unwrappedDataBlob []byte, daHeight uint64, namespace []byte) (bool, error) {
	if len(unwrappedDataBlob) == 0 {
		return true, nil
	}

	c.logger.Debug().
		Int("unwrapped_size", len(unwrappedDataBlob)).
		Uint64("da_height", daHeight).
		Msg("verifying data blob (will check wrapped SignedData on Celestia)")

	// get all blobs at the DA height
	blobs, err := c.GetBlobsAtHeight(ctx, daHeight, namespace)
	if err != nil {
		return false, fmt.Errorf("failed to get blobs: %w", err)
	}

	if len(blobs) == 0 {
		c.logger.Debug().Uint64("da_height", daHeight).Msg("no blobs found")
		return false, nil
	}

	// try to unwrap each blob as SignedData and compare the inner Data
	for i, blob := range blobs {
		var signedData evnode.SignedData
		if err := proto.Unmarshal(blob, &signedData); err != nil {
			c.logger.Debug().
				Int("blob_index", i).
				Err(err).
				Msg("failed to unmarshal as SignedData, skipping")
			continue
		}

		if signedData.Data == nil {
			c.logger.Debug().Int("blob_index", i).Msg("SignedData has nil Data, skipping")
			continue
		}

		// marshal the inner Data to compare
		celestiaData, err := proto.Marshal(signedData.Data)
		if err != nil {
			c.logger.Debug().
				Int("blob_index", i).
				Err(err).
				Msg("failed to marshal inner Data")
			continue
		}

		c.logger.Debug().
			Int("blob_index", i).
			Int("celestia_data_size", len(celestiaData)).
			Int("evnode_data_size", len(unwrappedDataBlob)).
			Bool("matches", bytes.Equal(celestiaData, unwrappedDataBlob)).
			Msg("comparing unwrapped Data")

		if bytes.Equal(celestiaData, unwrappedDataBlob) {
			c.logger.Info().
				Int("blob_index", i).
				Msg("found matching Data (unwrapped from SignedData)")
			return true, nil
		}
	}

	c.logger.Warn().
		Int("checked_blobs", len(blobs)).
		Msg("no matching Data found in any SignedData wrapper")
	return false, nil
}
