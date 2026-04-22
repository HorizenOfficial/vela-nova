package cmd

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/HorizenOfficial/vela-common-go/common"
	"github.com/HorizenOfficial/vela-common-go/subgraph"

	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	"github.com/HorizenOfficial/vela/pkg/crypto"
)

var userEventsPageSize = 1000

// FetchAndDecryptUserEvents queries the subgraph for user events and decrypts them.
// The limit caps the number of decrypted events returned; limit <= 0 means no cap.
// The page size is internal and capped to avoid query errors.
// It applies the optional filter on decrypted payloads.
//
// eventSubTypes controls subtype filtering:
//   - nil or empty: no subtype filter — all events are returned.
//   - one or more entries: passed directly to the subgraph query as a
//     server-side filter. The subgraph client handles the [32]byte→hex wire
//     format conversion internally.
func FetchAndDecryptUserEvents(
	ctx context.Context,
	sg subgraph.Client,
	teePubKey *cryptotypes.PublicKeyP521,
	privKey cryptotypes.PrivateKeyP521,
	applicationID common.ApplicationIdType,
	eventSubTypes [][32]byte,
	limit int,
	filter func([]byte) bool,
) ([][]byte, error) {
	if sg == nil {
		return nil, fmt.Errorf("subgraph client is required")
	}
	if teePubKey == nil {
		return nil, fmt.Errorf("tee public key is required")
	}

	maxResults := limit
	if maxResults < 0 {
		maxResults = 0
	}
	pageSize := userEventsPageSize
	if pageSize <= 0 {
		pageSize = 1000
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	var decryptedEvents [][]byte
	var before *big.Int
	for {
		events, err := sg.GetUserEventsBySubTypes(ctx, applicationID, eventSubTypes, pageSize, before)
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			break
		}

		for _, ev := range events {
			plain, err := crypto.Decrypt(teePubKey, &privKey, ev.EncryptedData)
			if err != nil {
				if errors.Is(err, crypto.ErrDecrypt) {
					continue
				}
				continue
			}
			if filter != nil && !filter(plain) {
				continue
			}
			decryptedEvents = append(decryptedEvents, plain)
			if maxResults > 0 && len(decryptedEvents) >= maxResults {
				return decryptedEvents, nil
			}
		}

		if len(events) < pageSize {
			break
		}
		before = subgraph.ComputeSortKey(events[len(events)-1])
	}

	return decryptedEvents, nil
}
