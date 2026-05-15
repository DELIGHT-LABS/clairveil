package conformance_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
	SchemaVersion     string                          `json:"schema_version"`
	ActiveSetID       string                          `json:"active_set_id"`
	Curve             string                          `json:"curve"`
	ManifestFile      string                          `json:"manifest_file"`
	ManifestAvailable bool                            `json:"manifest_available"`
	ChecksumSource    string                          `json:"checksum_source"`
	GeneratedAt       string                          `json:"generated_at"`
	Artifacts         []browserCircuitArtifactFixture `json:"artifacts"`
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
	SearchPrivacyEventsRequest  browserSearchPrivacyEventsRequest  `json:"search_privacy_events_request"`
	SearchPrivacyEventsResponse browserSearchPrivacyEventsResponse `json:"search_privacy_events_response"`
	CheckNullifierRequest       browserCheckNullifierRequest       `json:"check_nullifier_request"`
	CheckNullifierResponse      browserCheckNullifierResponse      `json:"check_nullifier_response"`
}

type browserLatestBlockHeightFixture struct {
	Height int64 `json:"height"`
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

type browserSendProviderFixture struct {
	AuditConfigResponse browserAuditConfigFixture `json:"audit_config_response"`
	MerklePathRequest   browserMerklePathRequest  `json:"merkle_path_request"`
	MerklePathResponse  browserMerklePathResponse `json:"merkle_path_response"`
}

type browserAuditConfigFixture struct {
	AuditMasterPubkeyHex string `json:"audit_master_pubkey_hex"`
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

	require.Equal(t, "v1", contract.SchemaVersion)

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

	require.Equal(t, privacyzk.CircuitConfigSchemaVersion, contract.WalletInfoProvider.CircuitConfigResponse.SchemaVersion)
	require.Equal(t, privacyzk.ActiveCircuitSetID, contract.WalletInfoProvider.CircuitConfigResponse.ActiveSetID)
	require.Equal(t, privacyzk.CircuitCurve, contract.WalletInfoProvider.CircuitConfigResponse.Curve)
	require.Equal(t, privacyzk.ArtifactManifestFile, contract.WalletInfoProvider.CircuitConfigResponse.ManifestFile)
	require.False(t, contract.WalletInfoProvider.CircuitConfigResponse.ManifestAvailable)
	require.Equal(t, privacyzk.ChecksumSourceNone, contract.WalletInfoProvider.CircuitConfigResponse.ChecksumSource)

	defaultArtifacts := privacyzk.DefaultArtifactDescriptors()
	require.Len(t, contract.WalletInfoProvider.CircuitConfigResponse.Artifacts, len(defaultArtifacts))
	for i, artifact := range contract.WalletInfoProvider.CircuitConfigResponse.Artifacts {
		require.Equal(t, defaultArtifacts[i].CircuitID, artifact.CircuitID)
		require.Equal(t, defaultArtifacts[i].ArtifactType, artifact.ArtifactType)
		require.Equal(t, defaultArtifacts[i].Filename, artifact.Filename)
		require.Equal(t, defaultArtifacts[i].ChecksumEnv, artifact.ChecksumEnv)
		require.Empty(t, artifact.SHA256)
	}

	require.GreaterOrEqual(t, contract.ScanProvider.LatestBlockHeightResponse.Height, vectors.Scan.Height)
	require.Equal(t, []string{privacytypes.EventTypeDeposit, privacytypes.EventTypeShieldedTransfer}, contract.ScanProvider.SearchPrivacyEventsRequest.EventTypes)
	require.Len(t, contract.ScanProvider.SearchPrivacyEventsResponse.Events, 1)
	require.Equal(t, vectors.Scan.TxHashHex, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].TxHashHex)
	require.Equal(t, vectors.Scan.Height, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].Height)
	require.Equal(t, privacytypes.EventTypeDeposit, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].EventType)
	require.Len(t, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].Attributes, 1)
	require.Equal(t, privacytypes.AttributeKeyEncryptedNote, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].Attributes[0].Key)
	require.Equal(t, vectors.Note.EncryptedNoteHex, contract.ScanProvider.SearchPrivacyEventsResponse.Events[0].Attributes[0].Value)
	require.Equal(t, vectors.Note.NullifierHex, contract.ScanProvider.CheckNullifierRequest.NullifierHex)
	require.False(t, contract.ScanProvider.CheckNullifierResponse.Used)

	require.Equal(t, vectors.Recipient.DisclosurePubKeyHex, contract.SendProvider.AuditConfigResponse.AuditMasterPubkeyHex)
	require.Equal(t, vectors.Note.CommitmentHex, contract.SendProvider.MerklePathRequest.CommitmentHex)
	require.NoError(t, validateCanonicalHex32(contract.SendProvider.MerklePathResponse.RootHex, "merkle path root"))
	require.Equal(t, []string{"01", "02"}, contract.SendProvider.MerklePathResponse.Path)
	require.Equal(t, []uint32{0, 1}, contract.SendProvider.MerklePathResponse.PathHelper)
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

	defaultArtifacts := privacyzk.DefaultArtifactDescriptors()
	artifacts := make([]browserCircuitArtifactFixture, 0, len(defaultArtifacts))
	for _, artifact := range defaultArtifacts {
		artifacts = append(artifacts, browserCircuitArtifactFixture{
			CircuitID:    artifact.CircuitID,
			ArtifactType: artifact.ArtifactType,
			Filename:     artifact.Filename,
			ChecksumEnv:  artifact.ChecksumEnv,
			SHA256:       "",
		})
	}

	return browserSignerProviderContract{
		SchemaVersion: "v1",
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
				SchemaVersion:     privacyzk.CircuitConfigSchemaVersion,
				ActiveSetID:       privacyzk.ActiveCircuitSetID,
				Curve:             privacyzk.CircuitCurve,
				ManifestFile:      privacyzk.ArtifactManifestFile,
				ManifestAvailable: false,
				ChecksumSource:    privacyzk.ChecksumSourceNone,
				GeneratedAt:       "",
				Artifacts:         artifacts,
			},
		},
		ScanProvider: browserScanProviderFixture{
			LatestBlockHeightResponse: browserLatestBlockHeightFixture{
				Height: vectors.Scan.Height,
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
		},
		SendProvider: browserSendProviderFixture{
			AuditConfigResponse: browserAuditConfigFixture{
				AuditMasterPubkeyHex: vectors.Recipient.DisclosurePubKeyHex,
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
