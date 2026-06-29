package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	ethCommon "github.com/ethereum/go-ethereum/common"
	ethCrypto "github.com/ethereum/go-ethereum/crypto"
	authorityapi "github.com/HorizenOfficial/vela/pkg/authorityservice/api"
	"github.com/HorizenOfficial/vela/pkg/blockchain"
	"github.com/HorizenOfficial/vela/pkg/common"
	cryptotypes "github.com/HorizenOfficial/vela/pkg/common/crypto"
	"github.com/HorizenOfficial/vela/pkg/crypto"
)

// AuthorityClient wraps authenticated calls to the authority service (nonce and report retrieval).
// WASM upload does not require authentication and is handled separately in the deployapp command.
type AuthorityClient struct {
	BaseURL    string
	ChainID    uint64
	AppID      common.ApplicationIdType
	KeySecp    *cryptotypes.PrivateKeySecp256k1
	HTTPClient *http.Client
}

func NewAuthorityClient(baseURL string, chainID uint64, appID common.ApplicationIdType, key *cryptotypes.PrivateKeySecp256k1) *AuthorityClient {
	return &AuthorityClient{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		ChainID:    chainID,
		AppID:      appID,
		KeySecp:    key,
		HTTPClient: http.DefaultClient,
	}
}

// FetchNonce calls GET /nonce.
func (c *AuthorityClient) FetchNonce(ctx context.Context) (*authorityapi.NonceResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/nonce", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting nonce: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nonce request failed: %s", bytes.TrimSpace(body))
	}

	var n authorityapi.NonceResponse
	if err := json.NewDecoder(resp.Body).Decode(&n); err != nil {
		return nil, fmt.Errorf("decoding nonce response: %w", err)
	}
	return &n, nil
}

// FetchReport calls POST /getreport and returns the parsed report.
func (c *AuthorityClient) FetchReport(ctx context.Context, reportIDHex string, nonce *authorityapi.NonceResponse) (*common.DeanonymizationReport, error) {
	if c.KeySecp == nil {
		return nil, fmt.Errorf("secp key not configured")
	}

	// Some UIs prefix the report ID with "<appId>_" (e.g., "1_<hex>"); strip it if present.
	if parts := strings.SplitN(reportIDHex, "_", 2); len(parts) == 2 {
		if app, err := strconv.ParseUint(parts[0], 10, 64); err == nil && app == uint64(c.AppID) {
			reportIDHex = parts[1]
		}
	}

	reportID, err := authorityapi.ParseRequestID(reportIDHex)
	if err != nil {
		return nil, fmt.Errorf("invalid report id: %w", err)
	}

	nonceBytes, err := hex.DecodeString(nonce.Nonce)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce from server: %w", err)
	}

	msg := authorityapi.BuildMessage(c.ChainID, c.AppID, reportID, nonceBytes)
	prefix := fmt.Sprintf("\x19Ethereum Signed Message:\n%d", len(msg))
	hash := ethCrypto.Keccak256Hash([]byte(prefix), msg)

	sig, err := ethCrypto.Sign(hash.Bytes(), c.KeySecp.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("signing request: %w", err)
	}

	body := authorityapi.GetReportRequest{
		ChainID:   c.ChainID,
		AppID:     uint64(c.AppID),
		ReportID:  reportIDHex,
		Salt:      nonce.Salt,
		Nonce:     nonce.Nonce,
		Timestamp: nonce.Timestamp,
		Signature: hex.EncodeToString(sig),
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding getreport request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/getreport", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting report: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("getreport failed: %s", bytes.TrimSpace(bodyBytes))
	}

	var r authorityapi.GetReportResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decoding getreport response: %w", err)
	}
	return convertReportResponse(&r)
}

// ParseDeanonymizationReport decodes a DeanonymizationReport from JSON bytes.
func ParseDeanonymizationReport(data []byte) (*common.DeanonymizationReport, error) {
	var report common.DeanonymizationReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("error unmarshalling deanonymization report: %w", err)
	}
	return &report, nil
}

// DecryptReport decrypts a deanonymization report using the TEE public key.
func DecryptReport(ctx context.Context, bc blockchain.Client, privP521 *cryptotypes.PrivateKeyP521, report *common.DeanonymizationReport) (*common.DecryptedReport, error) {
	if privP521 == nil {
		return nil, fmt.Errorf("P521 key not configured")
	}
	pubKey, err := bc.GetTeePublicKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("retrieving TEE public key: %w", err)
	}

	decryptedBytes, err := crypto.Decrypt(pubKey, privP521, report.EncryptedReport)
	if err != nil {
		return nil, fmt.Errorf("decrypting report: %w", err)
	}

	var dec common.DecryptedReport
	if err := json.Unmarshal(decryptedBytes, &dec); err != nil {
		return nil, fmt.Errorf("parsing decrypted report: %w", err)
	}
	return &dec, nil
}

func (c *AuthorityClient) httpClient() *http.Client {
	if c.HTTPClient == nil {
		return http.DefaultClient
	}
	return c.HTTPClient
}

func convertReportResponse(resp *authorityapi.GetReportResponse) (*common.DeanonymizationReport, error) {
	appID, err := strconv.ParseUint(resp.ApplicationID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid applicationId in response: %w", err)
	}
	reportID, err := authorityapi.ParseRequestID(resp.ReportID)
	if err != nil {
		return nil, fmt.Errorf("invalid reportId in response: %w", err)
	}
	encBytes, err := hex.DecodeString(resp.EncryptedReport)
	if err != nil {
		return nil, fmt.Errorf("invalid encryptedReport in response: %w", err)
	}

	return &common.DeanonymizationReport{
		ApplicationID:   common.ApplicationIdType(appID),
		ReportID:        reportID,
		Authority:       ethCommon.HexToAddress(resp.Authority),
		EncryptedReport: encBytes,
	}, nil
}
