package conformance_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/stretchr/testify/require"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const updateBrowserSignerProviderContractFixtureEnv = "PRIVACY_UPDATE_BROWSER_SIGNER_PROVIDER_CONTRACT_FIXTURES"

type browserSignerProviderContract struct {
	SchemaVersion      string                           `json:"schema_version"`
	RootSigner         browserRootSignerFixture         `json:"root_signer"`
	WalletInfoProvider browserWalletInfoProviderFixture `json:"wallet_info_provider"`
	ScanProvider       browserScanProviderFixture       `json:"scan_provider"`
	SendProvider       browserSendProviderFixture       `json:"send_provider"`
}

type browserRootSignerFixture struct {
	GetAccountResponse browserSignerAccountFixture `json:"get_account_response"`
	SignRequest        browserSignRequestFixture   `json:"sign_request"`
	SignResponse       browserSignResponseFixture  `json:"sign_response"`
	ExpectedDerived    browserExpectedDerived      `json:"expected_derived"`
}

type browserSignerAccountFixture struct {
	TransparentAddress   string `json:"transparent_address"`
	TransparentPubKeyHex string `json:"transparent_pubkey_hex"`
}

type browserSignRequestFixture struct {
	Method     string `json:"method"`
	MessageHex string `json:"message_hex"`
}

type browserSignResponseFixture struct {
	SignatureHex string `json:"signature_hex"`
}

type browserExpectedDerived struct {
	RootSeedHex         string `json:"root_seed_hex"`
	ShieldedAddress     string `json:"shielded_address"`
	DisclosurePubKeyHex string `json:"disclosure_pubkey_hex"`
}

type browserWalletInfoProviderFixture struct {
	TreeStateResponse        browserTreeStateFixture        `json:"tree_state_response"`
	CommitmentInfoRequest    browserCommitmentInfoRequest   `json:"commitment_info_request"`
	CommitmentInfoResponse   browserCommitmentInfoResponse  `json:"commitment_info_response"`
	DisclosureConfigResponse browserDisclosureConfigFixture `json:"disclosure_config_response"`
	CircuitConfigResponse    browserCircuitConfigFixture    `json:"circuit_config_response"`
}

type browserTreeStateFixture struct {
	RootHex         string `json:"root_hex"`
	LeafCount       uint64 `json:"leaf_count"`
	Depth           uint32 `json:"depth"`
	Initialized     bool   `json:"initialized"`
	MaxLeaves       uint64 `json:"max_leaves"`
	RemainingLeaves uint64 `json:"remaining_leaves"`
}

type browserCommitmentInfoRequest struct {
	CommitmentHex string `json:"commitment_hex"`
}

type browserCommitmentInfoResponse struct {
	Found     bool   `json:"found"`
	LeafIndex uint64 `json:"leaf_index"`
}

type browserDisclosureConfigFixture struct {
	PayloadVersion          string   `json:"payload_version"`
	AuditDisclosureRequired bool     `json:"audit_disclosure_required"`
	SupportedUserPolicies   []string `json:"supported_user_policies"`
	SupportedUserModes      []string `json:"supported_user_modes"`
}

type browserCircuitConfigFixture struct {
	SchemaVersion      string                           `json:"schema_version"`
	ActiveSetID        string                           `json:"active_set_id"`
	Curve              string                           `json:"curve"`
	ManifestFile       string                           `json:"manifest_file"`
	ManifestAvailable  bool                             `json:"manifest_available"`
	ChecksumSource     string                           `json:"checksum_source"`
	GeneratedAt        string                           `json:"generated_at"`
	Artifacts          []browserCircuitArtifactFixture  `json:"artifacts"`
	CircuitSetIdentity *privacytypes.CircuitSetIdentity `json:"circuit_set_identity"`
}

type browserCircuitArtifactFixture struct {
	CircuitID    string `json:"circuit_id"`
	ArtifactType string `json:"artifact_type"`
	Filename     string `json:"filename"`
	ChecksumEnv  string `json:"checksum_env"`
	SHA256       string `json:"sha256"`
}

type browserScanProviderFixture struct {
	LatestBlockHeightResponse   browserLatestBlockHeightFixture    `json:"latest_block_height_response"`
	ScanEventsRequest           browserScanEventsRequest           `json:"scan_events_request"`
	ScanEventsResponse          browserScanEventsResponse          `json:"scan_events_response"`
	SearchPrivacyEventsRequest  browserSearchPrivacyEventsRequest  `json:"search_privacy_events_request"`
	SearchPrivacyEventsResponse browserSearchPrivacyEventsResponse `json:"search_privacy_events_response"`
	CheckNullifiersRequest      browserCheckNullifiersRequest      `json:"check_nullifiers_request"`
	CheckNullifiersResponse     browserCheckNullifiersResponse     `json:"check_nullifiers_response"`
	CheckNullifierRequest       browserCheckNullifierRequest       `json:"check_nullifier_request"`
	CheckNullifierResponse      browserCheckNullifierResponse      `json:"check_nullifier_response"`
}

type browserLatestBlockHeightFixture struct {
	Height int64 `json:"height"`
}

type browserScanEventsRequest struct {
	AfterHeight   int64    `json:"after_height"`
	AfterSequence uint64   `json:"after_sequence"`
	Limit         uint64   `json:"limit"`
	EventTypes    []string `json:"event_types"`
}

type browserScanEventsResponse struct {
	Events            []browserScanEventFixture `json:"events"`
	NextHeight        int64                     `json:"next_height"`
	NextSequence      uint64                    `json:"next_sequence"`
	Limit             uint64                    `json:"limit"`
	HasMore           bool                      `json:"has_more"`
	ScanFormatVersion uint32                    `json:"scan_format_version"`
	ViewTagVersion    uint32                    `json:"view_tag_version"`
}

type browserScanEventFixture struct {
	Sequence       uint64                     `json:"sequence"`
	Height         int64                      `json:"height"`
	TxHashHex      string                     `json:"tx_hash_hex"`
	EventType      string                     `json:"event_type"`
	Outputs        []browserScanOutputFixture `json:"outputs"`
	NullifierHexes []string                   `json:"nullifier_hexes"`
}

type browserScanOutputFixture struct {
	OutputIndex      uint32 `json:"output_index"`
	CommitmentHex    string `json:"commitment_hex,omitempty"`
	EncryptedNoteHex string `json:"encrypted_note_hex,omitempty"`
	CipherTextHex    string `json:"cipher_text_hex,omitempty"`
	ViewTagHex       string `json:"view_tag_hex,omitempty"`
	LeafIndexFound   bool   `json:"leaf_index_found,omitempty"`
	LeafIndex        uint64 `json:"leaf_index,omitempty"`
}

type browserSearchPrivacyEventsRequest struct {
	AfterHeight int64    `json:"after_height"`
	Page        uint64   `json:"page"`
	Limit       uint64   `json:"limit"`
	EventTypes  []string `json:"event_types"`
}

type browserSearchPrivacyEventsResponse struct {
	Events  []browserPrivacyEventFixture `json:"events"`
	Page    uint64                       `json:"page"`
	Limit   uint64                       `json:"limit"`
	HasMore bool                         `json:"has_more"`
}

type browserPrivacyEventFixture struct {
	Sequence   uint64                           `json:"sequence"`
	Height     int64                            `json:"height"`
	TxHashHex  string                           `json:"tx_hash_hex"`
	EventType  string                           `json:"event_type"`
	Attributes []browserPrivacyAttributeFixture `json:"attributes"`
}

type browserPrivacyAttributeFixture struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type browserCheckNullifierRequest struct {
	NullifierHex string `json:"nullifier_hex"`
}

type browserCheckNullifierResponse struct {
	Used bool `json:"used"`
}

type browserCheckNullifiersRequest struct {
	Nullifiers []string `json:"nullifiers"`
}

type browserCheckNullifiersResponse struct {
	Statuses []browserNullifierStatusFixture `json:"statuses"`
}

type browserNullifierStatusFixture struct {
	Nullifier string `json:"nullifier"`
	Used      bool   `json:"used"`
}

type browserSendProviderFixture struct {
	AuditConfigResponse browserAuditConfigFixture `json:"audit_config_response"`
	MerklePathRequest   browserMerklePathRequest  `json:"merkle_path_request"`
	MerklePathResponse  browserMerklePathResponse `json:"merkle_path_response"`
}

type browserAuditConfigFixture struct {
	AuditMasterPubkeyHex string `json:"audit_master_pubkey_hex"`
	AuditKeyID           string `json:"audit_key_id"`
	AuditKeyEpoch        string `json:"audit_key_epoch"`
}

type browserMerklePathRequest struct {
	CommitmentHex string `json:"commitment_hex"`
}

type browserMerklePathResponse struct {
	RootHex    string   `json:"root_hex"`
	Path       []string `json:"path"`
	PathHelper []uint32 `json:"path_helper"`
}

func TestBrowserSignerProviderContractFixture(t *testing.T) {
	vectors := loadGoldenVectors(t)
	contract := loadBrowserSignerProviderContract(t)

	require.Equal(t, "v2", contract.SchemaVersion)

	require.Equal(t, vectors.SenderRootSeed.Address, contract.RootSigner.GetAccountResponse.TransparentAddress)
	require.Equal(t, vectors.SenderRootSeed.TransparentPubKeyHex, contract.RootSigner.GetAccountResponse.TransparentPubKeyHex)
	require.Equal(t, "sign_privacy_root", contract.RootSigner.SignRequest.Method)
	require.Equal(t, vectors.SenderRootSeed.SigningMessageHex, contract.RootSigner.SignRequest.MessageHex)
	require.Equal(t, vectors.SenderRootSeed.SignatureHex, contract.RootSigner.SignResponse.SignatureHex)
	require.Equal(t, vectors.SenderRootSeed.RootSeedHex, contract.RootSigner.ExpectedDerived.RootSeedHex)
	require.Equal(t, vectors.Sender.ShieldedAddress, contract.RootSigner.ExpectedDerived.ShieldedAddress)
	require.Equal(t, vectors.Sender.DisclosurePubKeyHex, contract.RootSigner.ExpectedDerived.DisclosurePubKeyHex)

	pubKeyBytes := mustDecodeHex(t, contract.RootSigner.GetAccountResponse.TransparentPubKeyHex)
	signingMessage := privacyidentity.BuildRootSigningMessage(contract.RootSigner.GetAccountResponse.TransparentAddress, pubKeyBytes)
	require.Equal(t, contract.RootSigner.SignRequest.MessageHex, hex.EncodeToString(signingMessage))
	rootSeed := privacyidentity.ComputeRootSeed(
		contract.RootSigner.GetAccountResponse.TransparentAddress,
		pubKeyBytes,
		mustDecodeHex(t, contract.RootSigner.SignResponse.SignatureHex),
	)
	require.Equal(t, contract.RootSigner.ExpectedDerived.RootSeedHex, hex.EncodeToString(rootSeed))

	require.NoError(t, validateCanonicalHex32(contract.WalletInfoProvider.TreeStateResponse.RootHex, "tree root"))
	require.Equal(t, uint32(32), contract.WalletInfoProvider.TreeStateResponse.Depth)
	require.True(t, contract.WalletInfoProvider.TreeStateResponse.Initialized)
	require.Equal(t, uint64(4294967296), contract.WalletInfoProvider.TreeStateResponse.MaxLeaves)
	require.Equal(t, uint64(4294967289), contract.WalletInfoProvider.TreeStateResponse.RemainingLeaves)
	require.Equal(t, vectors.Note.CommitmentHex, contract.WalletInfoProvider.CommitmentInfoRequest.CommitmentHex)
	require.True(t, contract.WalletInfoProvider.CommitmentInfoResponse.Found)
	require.Equal(t, privacytypes.DisclosurePayloadVersion, contract.WalletInfoProvider.DisclosureConfigResponse.PayloadVersion)
	require.True(t, contract.WalletInfoProvider.DisclosureConfigResponse.AuditDisclosureRequired)
	require.Equal(t, privacytypes.SupportedUserDisclosurePolicies(), contract.WalletInfoProvider.DisclosureConfigResponse.SupportedUserPolicies)
	require.Equal(t, privacytypes.SupportedUserDisclosureModes(), contract.WalletInfoProvider.DisclosureConfigResponse.SupportedUserModes)

	require.Equal(t, privacytypes.CircuitSetIdentitySchemaVersion, contract.WalletInfoProvider.CircuitConfigResponse.SchemaVersion)
	require.Equal(t, privacyzk.ActiveCircuitSetID, contract.WalletInfoProvider.CircuitConfigResponse.ActiveSetID)
	require.Equal(t, privacyzk.CircuitCurve, contract.WalletInfoProvider.CircuitConfigResponse.Curve)
	require.Empty(t, contract.WalletInfoProvider.CircuitConfigResponse.ManifestFile)
	require.False(t, contract.WalletInfoProvider.CircuitConfigResponse.ManifestAvailable)
	require.Equal(t, "consensus", contract.WalletInfoProvider.CircuitConfigResponse.ChecksumSource)

	require.NoError(t, privacytypes.ValidateCircuitSetIdentity(contract.WalletInfoProvider.CircuitConfigResponse.CircuitSetIdentity))
	require.Len(t, contract.WalletInfoProvider.CircuitConfigResponse.Artifacts, len(privacytypes.RequiredCircuitIdentityOrder))
	for i, artifact := range contract.WalletInfoProvider.CircuitConfigResponse.Artifacts {
		require.Equal(t, privacytypes.RequiredCircuitIdentityOrder[i], artifact.CircuitID)
		require.Equal(t, "verifying_key", artifact.ArtifactType)
		require.Empty(t, artifact.Filename)
		require.Empty(t, artifact.ChecksumEnv)
		require.Equal(t, contract.WalletInfoProvider.CircuitConfigResponse.CircuitSetIdentity.Circuits[i].VerifyingKeySha256, artifact.SHA256)
	}

	require.GreaterOrEqual(t, contract.ScanProvider.LatestBlockHeightResponse.Height, vectors.Scan.Height)
	require.Equal(t, int64(0), contract.ScanProvider.ScanEventsRequest.AfterHeight)
	require.Equal(t, uint64(0), contract.ScanProvider.ScanEventsRequest.AfterSequence)
	require.Equal(t, uint64(50), contract.ScanProvider.ScanEventsRequest.Limit)
	require.Equal(t, []string{privacytypes.EventTypeDeposit, privacytypes.EventTypeShieldedTransfer}, contract.ScanProvider.ScanEventsRequest.EventTypes)
	require.Len(t, contract.ScanProvider.ScanEventsResponse.Events, 2)
	require.Equal(t, vectors.Scan.TxHashHex, contract.ScanProvider.ScanEventsResponse.Events[0].TxHashHex)
	require.Equal(t, vectors.Scan.Height, contract.ScanProvider.ScanEventsResponse.Events[0].Height)
	require.Equal(t, privacytypes.EventTypeDeposit, contract.ScanProvider.ScanEventsResponse.Events[0].EventType)
	require.Len(t, contract.ScanProvider.ScanEventsResponse.Events[0].Outputs, 1)
	require.Equal(t, uint32(0), contract.ScanProvider.ScanEventsResponse.Events[0].Outputs[0].OutputIndex)
	require.Equal(t, vectors.Note.CommitmentHex, contract.ScanProvider.ScanEventsResponse.Events[0].Outputs[0].CommitmentHex)
	require.Equal(t, vectors.Note.EncryptedNoteHex, contract.ScanProvider.ScanEventsResponse.Events[0].Outputs[0].EncryptedNoteHex)
	require.Empty(t, contract.ScanProvider.ScanEventsResponse.Events[0].NullifierHexes)
	transferScanEvent := contract.ScanProvider.ScanEventsResponse.Events[1]
	require.Equal(t, privacytypes.EventTypeShieldedTransfer, transferScanEvent.EventType)
	require.Len(t, transferScanEvent.Outputs, 2)
	require.Len(t, transferScanEvent.NullifierHexes, 2)
	for _, output := range transferScanEvent.Outputs {
		require.NoError(t, validateCanonicalHex32(output.CommitmentHex, "transfer scan commitment"))
		require.NotEmpty(t, output.CipherTextHex)
		require.Len(t, output.ViewTagHex, privacytypes.ViewTagLength*2)
	}
	require.Equal(t, vectors.Scan.Height, contract.ScanProvider.ScanEventsResponse.NextHeight)
	require.Equal(t, uint64(2), contract.ScanProvider.ScanEventsResponse.NextSequence)
	require.Equal(t, uint64(50), contract.ScanProvider.ScanEventsResponse.Limit)
	require.False(t, contract.ScanProvider.ScanEventsResponse.HasMore)
	require.Equal(t, privacytypes.ScanFormatVersion, contract.ScanProvider.ScanEventsResponse.ScanFormatVersion)
	require.Equal(t, privacytypes.ViewTagVersion, contract.ScanProvider.ScanEventsResponse.ViewTagVersion)
	require.Equal(t, []string{privacytypes.EventTypeDeposit, privacytypes.EventTypeShieldedTransfer}, contract.ScanProvider.SearchPrivacyEventsRequest.EventTypes)
	require.Len(t, contract.ScanProvider.SearchPrivacyEventsResponse.Events, 1)
	require.Equal(t, vectors.Scan.TxHashHex, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].TxHashHex)
	require.Equal(t, vectors.Scan.Height, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].Height)
	require.Equal(t, privacytypes.EventTypeDeposit, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].EventType)
	require.Len(t, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].Attributes, 2)
	require.Equal(t, privacytypes.AttributeKeyCommitment, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].Attributes[0].Key)
	require.Equal(t, vectors.Note.CommitmentHex, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].Attributes[0].Value)
	require.Equal(t, privacytypes.AttributeKeyEncryptedNote, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].Attributes[1].Key)
	require.Equal(t, vectors.Note.EncryptedNoteHex, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].Attributes[1].Value)
	require.Equal(t, []string{vectors.Note.NullifierHex}, contract.ScanProvider.CheckNullifiersRequest.Nullifiers)
	require.Len(t, contract.ScanProvider.CheckNullifiersResponse.Statuses, 1)
	require.Equal(t, vectors.Note.NullifierHex, contract.ScanProvider.CheckNullifiersResponse.Statuses[0].Nullifier)
	require.False(t, contract.ScanProvider.CheckNullifiersResponse.Statuses[0].Used)
	require.Equal(t, vectors.Note.NullifierHex, contract.ScanProvider.CheckNullifierRequest.NullifierHex)
	require.False(t, contract.ScanProvider.CheckNullifierResponse.Used)

	require.Equal(t, vectors.Recipient.DisclosurePubKeyHex, contract.SendProvider.AuditConfigResponse.AuditMasterPubkeyHex)
	require.Equal(t, privacytypes.DefaultAuditKeyIDV1, contract.SendProvider.AuditConfigResponse.AuditKeyID)
	require.Equal(t, strconv.FormatUint(privacytypes.DefaultAuditKeyEpochV1, 10), contract.SendProvider.AuditConfigResponse.AuditKeyEpoch)
	require.Equal(t, vectors.Note.CommitmentHex, contract.SendProvider.MerklePathRequest.CommitmentHex)
	require.NoError(t, validateCanonicalHex32(contract.SendProvider.MerklePathResponse.RootHex, "merkle path root"))
	require.Equal(t, []string{"01", "02"}, contract.SendProvider.MerklePathResponse.Path)
	require.Equal(t, []uint32{0, 1}, contract.SendProvider.MerklePathResponse.PathHelper)
}

func TestBrowserSignerProviderAuditConfigMatchesGatewayJSON(t *testing.T) {
	contract := loadBrowserSignerProviderContract(t)
	response := &privacytypes.QueryAuditConfigResponse{
		AuditMasterPubkeyHex: contract.SendProvider.AuditConfigResponse.AuditMasterPubkeyHex,
		AuditKeyId:           contract.SendProvider.AuditConfigResponse.AuditKeyID,
		AuditKeyEpoch:        privacytypes.DefaultAuditKeyEpochV1,
	}

	payload, err := codec.ProtoMarshalJSON(response, nil)
	require.NoError(t, err)

	var gatewayResponse browserAuditConfigFixture
	require.NoError(t, json.Unmarshal(payload, &gatewayResponse))
	require.Equal(t, contract.SendProvider.AuditConfigResponse, gatewayResponse)
}

func TestBrowserSignerProviderContractFixtureDerivedShieldedAddress(t *testing.T) {
	contract := loadBrowserSignerProviderContract(t)

	rootSeed := mustDecodeHex(t, contract.RootSigner.ExpectedDerived.RootSeedHex)

	shieldedAddress, err := privacyidentity.DeriveShieldedAddress(rootSeed)
	require.NoError(t, err)
	require.Equal(t, contract.RootSigner.ExpectedDerived.ShieldedAddress, shieldedAddress)

	_, disclosurePubKey, _ := privacyidentity.DeriveDisclosureKeys(rootSeed)
	bz := disclosurePubKey.Bytes()
	require.Equal(t, contract.RootSigner.ExpectedDerived.DisclosurePubKeyHex, hex.EncodeToString(bz[:]))
}

func TestBrowserSignerProviderContractFixtureDisclosureConfigMatchesSDK(t *testing.T) {
	contract := loadBrowserSignerProviderContract(t)

	require.Equal(t, privacydisclosure.PayloadVersion, contract.WalletInfoProvider.DisclosureConfigResponse.PayloadVersion)
	require.Equal(t, privacytypes.SupportedUserDisclosurePolicies(), contract.WalletInfoProvider.DisclosureConfigResponse.SupportedUserPolicies)
	require.Equal(t, privacytypes.SupportedUserDisclosureModes(), contract.WalletInfoProvider.DisclosureConfigResponse.SupportedUserModes)
}

func TestBrowserSignerProviderContractMatchesReadonlyReferenceBundle(t *testing.T) {
	contract := loadBrowserSignerProviderContract(t)
	readonly := loadReadonlyReferenceBundle(t)

	require.Equal(t, contract.RootSigner.GetAccountResponse.TransparentAddress, readonly.Sender.TransparentAddress)
	require.Equal(t, contract.RootSigner.ExpectedDerived.ShieldedAddress, readonly.Sender.ShowAddress.Address)
	require.Equal(t, contract.RootSigner.ExpectedDerived.DisclosurePubKeyHex, readonly.Sender.ShowDisclosurePubKey.PublicKeyHex)
	require.Equal(t, contract.SendProvider.AuditConfigResponse.AuditMasterPubkeyHex, readonly.Recipient.ShowDisclosurePubKey.PublicKeyHex)
	require.Equal(t, contract.ScanProvider.CheckNullifierRequest.NullifierHex, readonly.Scan.DepositFound[0].Nullifier)
	require.Equal(t, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].TxHashHex, readonly.Scan.DepositFound[0].TxHash)
}

func TestWriteBrowserSignerProviderContractFixture(t *testing.T) {
	if os.Getenv(updateBrowserSignerProviderContractFixtureEnv) != "1" {
		t.Skipf("set %s=1 to rewrite the browser signer/provider contract fixture", updateBrowserSignerProviderContractFixtureEnv)
	}

	payload, err := json.MarshalIndent(buildBrowserSignerProviderContract(t), "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(browserSignerProviderContractFixturePath(t), append(payload, '\n'), 0o644))
}

func buildBrowserSignerProviderContract(t *testing.T) browserSignerProviderContract {
	t.Helper()

	vectors := loadGoldenVectors(t)

	circuitIdentity := &privacytypes.CircuitSetIdentity{
		SchemaVersion: privacytypes.CircuitSetIdentitySchemaVersion,
		CircuitSetId:  privacytypes.ActiveCircuitSetID,
		Curve:         privacytypes.CircuitCurveBN254,
	}
	artifacts := make([]browserCircuitArtifactFixture, 0, len(privacytypes.RequiredCircuitIdentityOrder))
	for i, circuitID := range privacytypes.RequiredCircuitIdentityOrder {
		schemaDigest, err := privacyzk.PublicInputSchemaSHA256(circuitID)
		require.NoError(t, err)
		vkDigest := strings.Repeat(string(rune('a'+i)), 64)
		circuitIdentity.Circuits = append(circuitIdentity.Circuits, &privacytypes.CircuitIdentity{
			CircuitId:               circuitID,
			VerifyingKeySha256:      vkDigest,
			PublicInputSchemaSha256: schemaDigest,
		})
		artifacts = append(artifacts, browserCircuitArtifactFixture{
			CircuitID:    circuitID,
			ArtifactType: "verifying_key",
			SHA256:       vkDigest,
		})
	}

	return browserSignerProviderContract{
		SchemaVersion: "v2",
		RootSigner: browserRootSignerFixture{
			GetAccountResponse: browserSignerAccountFixture{
				TransparentAddress:   vectors.SenderRootSeed.Address,
				TransparentPubKeyHex: vectors.SenderRootSeed.TransparentPubKeyHex,
			},
			SignRequest: browserSignRequestFixture{
				Method:     "sign_privacy_root",
				MessageHex: vectors.SenderRootSeed.SigningMessageHex,
			},
			SignResponse: browserSignResponseFixture{
				SignatureHex: vectors.SenderRootSeed.SignatureHex,
			},
			ExpectedDerived: browserExpectedDerived{
				RootSeedHex:         vectors.SenderRootSeed.RootSeedHex,
				ShieldedAddress:     vectors.Sender.ShieldedAddress,
				DisclosurePubKeyHex: vectors.Sender.DisclosurePubKeyHex,
			},
		},
		WalletInfoProvider: browserWalletInfoProviderFixture{
			TreeStateResponse: browserTreeStateFixture{
				RootHex:         "0000000000000000000000000000000000000000000000000000000000000005",
				LeafCount:       7,
				Depth:           32,
				Initialized:     true,
				MaxLeaves:       4294967296,
				RemainingLeaves: 4294967289,
			},
			CommitmentInfoRequest: browserCommitmentInfoRequest{
				CommitmentHex: vectors.Note.CommitmentHex,
			},
			CommitmentInfoResponse: browserCommitmentInfoResponse{
				Found:     true,
				LeafIndex: 9,
			},
			DisclosureConfigResponse: browserDisclosureConfigFixture{
				PayloadVersion:          privacytypes.DisclosurePayloadVersion,
				AuditDisclosureRequired: true,
				SupportedUserPolicies:   privacytypes.SupportedUserDisclosurePolicies(),
				SupportedUserModes:      privacytypes.SupportedUserDisclosureModes(),
			},
			CircuitConfigResponse: browserCircuitConfigFixture{
				SchemaVersion:      privacytypes.CircuitSetIdentitySchemaVersion,
				ActiveSetID:        privacyzk.ActiveCircuitSetID,
				Curve:              privacyzk.CircuitCurve,
				ManifestFile:       "",
				ManifestAvailable:  false,
				ChecksumSource:     "consensus",
				GeneratedAt:        "",
				Artifacts:          artifacts,
				CircuitSetIdentity: circuitIdentity,
			},
		},
		ScanProvider: browserScanProviderFixture{
			LatestBlockHeightResponse: browserLatestBlockHeightFixture{
				Height: vectors.Scan.Height,
			},
			ScanEventsRequest: browserScanEventsRequest{
				AfterHeight:   0,
				AfterSequence: 0,
				Limit:         50,
				EventTypes:    []string{privacytypes.EventTypeDeposit, privacytypes.EventTypeShieldedTransfer},
			},
			ScanEventsResponse: browserScanEventsResponse{
				Events: []browserScanEventFixture{
					{
						Sequence:  1,
						Height:    vectors.Scan.Height,
						TxHashHex: vectors.Scan.TxHashHex,
						EventType: privacytypes.EventTypeDeposit,
						Outputs: []browserScanOutputFixture{
							{
								OutputIndex:      0,
								CommitmentHex:    vectors.Note.CommitmentHex,
								EncryptedNoteHex: vectors.Note.EncryptedNoteHex,
								LeafIndexFound:   true,
								LeafIndex:        0,
							},
						},
						NullifierHexes: []string{},
					},
					{
						Sequence:  2,
						Height:    vectors.Scan.Height,
						TxHashHex: "DDEEFF",
						EventType: privacytypes.EventTypeShieldedTransfer,
						Outputs: []browserScanOutputFixture{
							{
								OutputIndex:    0,
								CommitmentHex:  "10b62825eac8a645278d300ec077885a123170460e9ca1048f4521a3f6fb6cb2",
								CipherTextHex:  "c0ffee",
								ViewTagHex:     "13eb",
								LeafIndexFound: true,
								LeafIndex:      1,
							},
							{
								OutputIndex:    1,
								CommitmentHex:  "211dee8f0fe0b284c4c953a66a5facd9bb055f101d8dc94c0f55ae762093314b",
								CipherTextHex:  "decafbad",
								ViewTagHex:     "0636",
								LeafIndexFound: true,
								LeafIndex:      2,
							},
						},
						NullifierHexes: []string{
							vectors.Note.NullifierHex,
							"0000000000000000000000000000000000000000000000000000000000000001",
						},
					},
				},
				NextHeight:        vectors.Scan.Height,
				NextSequence:      2,
				Limit:             50,
				HasMore:           false,
				ScanFormatVersion: privacytypes.ScanFormatVersion,
				ViewTagVersion:    privacytypes.ViewTagVersion,
			},
			SearchPrivacyEventsRequest: browserSearchPrivacyEventsRequest{
				AfterHeight: 0,
				Page:        1,
				Limit:       50,
				EventTypes:  []string{privacytypes.EventTypeDeposit, privacytypes.EventTypeShieldedTransfer},
			},
			SearchPrivacyEventsResponse: browserSearchPrivacyEventsResponse{
				Events: []browserPrivacyEventFixture{
					{
						Sequence:  1,
						Height:    vectors.Scan.Height,
						TxHashHex: vectors.Scan.TxHashHex,
						EventType: privacytypes.EventTypeDeposit,
						Attributes: []browserPrivacyAttributeFixture{
							{
								Key:   privacytypes.AttributeKeyCommitment,
								Value: vectors.Note.CommitmentHex,
							},
							{
								Key:   privacytypes.AttributeKeyEncryptedNote,
								Value: vectors.Note.EncryptedNoteHex,
							},
						},
					},
				},
				Page:    1,
				Limit:   50,
				HasMore: false,
			},
			CheckNullifierRequest: browserCheckNullifierRequest{
				NullifierHex: vectors.Note.NullifierHex,
			},
			CheckNullifierResponse: browserCheckNullifierResponse{
				Used: false,
			},
			CheckNullifiersRequest: browserCheckNullifiersRequest{
				Nullifiers: []string{vectors.Note.NullifierHex},
			},
			CheckNullifiersResponse: browserCheckNullifiersResponse{
				Statuses: []browserNullifierStatusFixture{
					{
						Nullifier: vectors.Note.NullifierHex,
						Used:      false,
					},
				},
			},
		},
		SendProvider: browserSendProviderFixture{
			AuditConfigResponse: browserAuditConfigFixture{
				AuditMasterPubkeyHex: vectors.Recipient.DisclosurePubKeyHex,
				AuditKeyID:           privacytypes.DefaultAuditKeyIDV1,
				AuditKeyEpoch:        strconv.FormatUint(privacytypes.DefaultAuditKeyEpochV1, 10),
			},
			MerklePathRequest: browserMerklePathRequest{
				CommitmentHex: vectors.Note.CommitmentHex,
			},
			MerklePathResponse: browserMerklePathResponse{
				RootHex:    "0000000000000000000000000000000000000000000000000000000000000005",
				Path:       []string{"01", "02"},
				PathHelper: []uint32{0, 1},
			},
		},
	}
}

func loadBrowserSignerProviderContract(t *testing.T) browserSignerProviderContract {
	t.Helper()

	bz, err := os.ReadFile(browserSignerProviderContractFixturePath(t))
	require.NoError(t, err)

	var fixture browserSignerProviderContract
	require.NoError(t, json.Unmarshal(bz, &fixture))
	return fixture
}

func browserSignerProviderContractFixturePath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Join(filepath.Dir(filename), "testdata", "privacy_browser_signer_provider_contract.json")
}

func validateCanonicalHex32(value string, fieldName string) error {
	_, err := privacyfield.DecodeCanonicalHex(value, fieldName)
	return err
}
