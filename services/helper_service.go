package services

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/davecgh/go-spew/spew"
	bin "github.com/gagliardetto/binary"
	"github.com/gagliardetto/solana-go"
	associatedtokenaccount "github.com/gagliardetto/solana-go/programs/associated-token-account"
	"github.com/gagliardetto/solana-go/programs/system"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/gagliardetto/solana-go/rpc"
	confirm "github.com/gagliardetto/solana-go/rpc/sendAndConfirmTransaction"
	"github.com/gagliardetto/solana-go/rpc/ws"
	"github.com/gagliardetto/solana-go/text"
)

type HelperService struct {
	wsClient  *ws.Client
	rpcClient *rpc.Client
	logger    *slog.Logger
	debug     bool
}

type TokenBalanceChange struct {
	MintStr string
	Change  int64
}

func NewHelperService(rpcClient *rpc.Client, wsClient *ws.Client /*, debug bool*/) *HelperService {
	var logger *slog.Logger
	logger = slog.New(
		slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug, // This enables debug logging
		}),
	)

	// if debug {
	//     // Create logger that shows debug messages
	//     logger = slog.New(
	//         slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	//             Level: slog.LevelDebug, // This enables debug logging
	//         }),
	//     )
	// } else {
	//     // Create logger that only shows errors
	//     logger = slog.New(
	//         slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
	//             Level: slog.LevelError,
	//         }),
	//     )
	// }
	return &HelperService{
		rpcClient: rpcClient,
		wsClient:  wsClient,
		logger:    logger,
		debug:     true,
	}
}

// func NewHelperService(rpcClient *rpc.Client) *HelperService {
// 	return NewHelperServiceWithDebug(rpcClient, true) // Default debug=true
// }

// func NewHelperServiceWithDebug(rpcClient *rpc.Client, debug bool) *HelperService {
// 	var logger *slog.Logger
// 	if debug {
// 		logger = slog.Default()
// 	} else {
// 		logger = slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
// 	}
// 	return &HelperService{
// 		rpcClient: rpcClient,
// 		logger:    logger,
// 		debug:     debug,
// 	}
// }

// func (hs *HelperService) GetOrCreateTokenAccount(
// 	ctx context.Context,
// 	walletKeyPair *solana.PrivateKey,
// 	tokenMint solana.PublicKey,
// ) (solana.PublicKey, string, error) {
// 	const tokenAccountSize = 165 // Standard token account size
// 	// 1. Find associated token account address
// 	ata, _, err := solana.FindAssociatedTokenAddress(walletKeyPair.PublicKey(), tokenMint)
// 	if err != nil {
// 		return solana.PublicKey{}, "", fmt.Errorf("failed to find ATA: %w", err)
// 	}

// 	// 2. Check if account already exists
// 	_, err = hs.rpcClient.GetTokenAccountBalance(ctx, ata, rpc.CommitmentConfirmed)
// 	if err == nil {
// 		return ata, "", nil // Account exists
// 	}

// 	// 3. Get required lamports for rent exemption
// 	rent, err := hs.rpcClient.GetMinimumBalanceForRentExemption(
// 		ctx,
// 		tokenAccountSize,
// 		rpc.CommitmentConfirmed,
// 	)
// 	if err != nil {
// 		return solana.PublicKey{}, "", fmt.Errorf("failed to get rent exemption: %w", err)
// 	}

// 	// 4. Create instructions
// 	instructions := []solana.Instruction{
// 		// Create the account
// 		system.NewCreateAccountInstruction(
// 			rent,
// 			tokenAccountSize,
// 			solana.TokenProgramID,
// 			walletKeyPair.PublicKey(),
// 			ata,
// 		).Build(),

// 		// Initialize the token account (corrected with all required parameters)
// 		token.NewInitializeAccountInstruction(
// 			ata,                       // account
// 			tokenMint,                 // mint
// 			walletKeyPair.PublicKey(), // owner
// 			solana.SysVarRentPubkey,   // rentSysvar (added this parameter)
// 		).Build(),
// 	}

// 	// 5. Get recent blockhash
// 	recentBlockhash, err := hs.rpcClient.GetRecentBlockhash(ctx, rpc.CommitmentConfirmed)
// 	if err != nil {
// 		return solana.PublicKey{}, "", fmt.Errorf("failed to get blockhash: %w", err)
// 	}

// 	// 6. Build transaction
// 	tx, err := solana.NewTransaction(
// 		instructions,
// 		recentBlockhash.Value.Blockhash,
// 		solana.TransactionPayer(walletKeyPair.PublicKey()),
// 	)
// 	if err != nil {
// 		return solana.PublicKey{}, "", fmt.Errorf("failed to create transaction: %w", err)
// 	}

// 	// 7. Sign transaction
// 	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
// 		if key == walletKeyPair.PublicKey() {
// 			return walletKeyPair
// 		}
// 		return nil
// 	})
// 	if err != nil {
// 		return solana.PublicKey{}, "", fmt.Errorf("failed to sign transaction: %w", err)
// 	}

// 	// 8. Send transaction with confirmation
// 	sig, err := hs.rpcClient.SendTransactionWithOpts(
// 		ctx,
// 		tx,
// 		rpc.TransactionOpts{
// 			SkipPreflight:       false,
// 			PreflightCommitment: rpc.CommitmentConfirmed,
// 		},
// 	)
// 	if err != nil {
// 		return solana.PublicKey{}, "", fmt.Errorf("failed to send transaction: %w", err)
// 	}

// 	return ata, sig.String(), nil
// }

func (hs *HelperService) GetOrCreateTokenAccount(ctx context.Context, payer *solana.PrivateKey, mint solana.PublicKey, owner solana.PublicKey) (solana.PublicKey, string, error) {

	// Calculates the deterministic address of an associated token account (ATA) for a given wallet and token mint
	// Uses the wallet's public key and token mint address to derive the ATA address
	// This is a pure calculation - no on-chain check occurs
	// Only if there's a problem with the input parameters (invalid public keys)
	// Never fails just because the account doesn't exist on-chain
	// Returns (address, bumpSeed, error)
	// 1. Find the associated token address
	hs.logDebug("GetOrCreateTokenAccount called",
		"mint", mint.String(),
		"owner", owner.String())
	ata, _, err := solana.FindAssociatedTokenAddress(owner, mint)
	if err != nil {
		hs.logger.Error("Failed to find ATA", "error", err)
		return solana.PublicKey{}, "", fmt.Errorf("failed to find ATA: %w", err)
	}

	// This RPC call actually checks if the account exists on-chain
	// If error occurs, it likely means the account doesn't exist
	// 2. Check if account already exists
	hs.logDebug("Checking if token account exists", "ata", ata.String())
	_, err = hs.rpcClient.GetTokenAccountBalance(context.Background(), ata, rpc.CommitmentConfirmed)
	if err == nil {
		hs.logDebug("Token account already exists", "ata", ata.String())
		return ata, "", nil // Account exists
	}

	// 3. Create the ATA if it doesn't exist
	hs.logInfo("Creating new associated token account", "ata", ata.String())
	createIx := associatedtokenaccount.NewCreateInstruction(
		payer.PublicKey(), // payer
		owner,             // owner
		mint,              // mint
	).Build()

	// 4. Get recent blockhash
	latestBlockhash, err := hs.rpcClient.GetLatestBlockhash(context.Background(), rpc.CommitmentConfirmed)
	if err != nil {
		hs.logger.Error("Failed to get recent blockhash", "error", err)
		return solana.PublicKey{}, "", fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	// 5. Build transaction
	tx, err := solana.NewTransaction(
		[]solana.Instruction{createIx},
		latestBlockhash.Value.Blockhash,
		solana.TransactionPayer(payer.PublicKey()),
	)
	if err != nil {
		hs.logger.Error("Failed to create transaction", "error", err)
		return solana.PublicKey{}, "", fmt.Errorf("failed to create transaction: %w", err)
	}

	// 6. Sign the transaction
	_, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
		if key == payer.PublicKey() {
			return payer
		}
		return nil
	})
	if err != nil {
		hs.logger.Error("Failed to sign transaction", "error", err)
		return solana.PublicKey{}, "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	spew.Dump(tx)
	// Pretty print the transaction:
	tx.EncodeTree(text.NewTreeEncoder(os.Stdout, "Transfer SOL"))
	// 7. Send with automatic confirmation
	sig, err := confirm.SendAndConfirmTransaction(
		ctx,
		hs.rpcClient,
		hs.wsClient,
		tx,
	)
	// opts := rpc.TransactionOpts{
	// 	SkipPreflight:       false,
	// 	PreflightCommitment: rpc.CommitmentConfirmed,
	// }

	// sig, err := hs.rpcClient.SendTransactionWithOpts(
	// 	ctx,
	// 	tx,
	// 	opts,
	// )
	// if err != nil {
	// 	hs.logger.Error("Failed to send transaction", "error", err)
	// 	return solana.PublicKey{}, "", fmt.Errorf("failed to send transaction: %w", err)
	// }

	hs.logInfo("Successfully created token account",
		"ata", ata.String(),
		"signature", sig.String())
	return ata, sig.String(), nil
}

func (hs *HelperService) DecodeTransaction(encodedTx string) (*solana.Transaction, error) {
	hs.logDebug("Decoding transaction", "encodedTx", encodedTx[:min(20, len(encodedTx))]+"...")
	txData, err := base64.StdEncoding.DecodeString(encodedTx)
	if err != nil {

		hs.logger.Error("Base64 decode failed", "error", err)
		return nil, fmt.Errorf("base64 decode failed: %w", err)
	}

	// Create a new bin decoder from the bytes
	decoder := bin.NewBinDecoder(txData)
	// Deserialize the transaction
	var tx solana.Transaction
	if err := decoder.Decode(&tx); err != nil {
		hs.logger.Error("Transaction deserialization failed", "error", err)
		return nil, fmt.Errorf("transaction deserialization failed: %w", err)
	}
	hs.logDebug("Transaction decoded successfully",
		"numInstructions", len(tx.Message.Instructions))
	return &tx, nil
}

func (hs *HelperService) ValidateTokenAddress(address string) (solana.PublicKey, error) {
	hs.logDebug("Validating token address", "address", address)
	pubkey, err := solana.PublicKeyFromBase58(address)
	if err != nil {
		hs.logger.Error("Invalid token address", "address", address, "error", err)
		return solana.PublicKey{}, fmt.Errorf("invalid token address: %w", err)
	}

	// Verify the token mint exists on chain
	mintInfo, err := hs.rpcClient.GetAccountInfo(context.Background(), pubkey)
	if err != nil {
		hs.logger.Error("Failed to verify token mint", "mint", pubkey.String(), "error", err)
		return solana.PublicKey{}, fmt.Errorf("failed to verify token mint: %w", err)
	}
	if mintInfo == nil {
		hs.logger.Error("Token mint account does not exist", "mint", pubkey.String())
		return solana.PublicKey{}, fmt.Errorf("token mint account does not exist")
	}
	hs.logDebug("Token address validated successfully", "mint", pubkey.String())
	return pubkey, nil
}

func (hs *HelperService) GetTokenDecimals(mint solana.PublicKey) (uint8, error) {
	hs.logDebug("Getting token decimals", "mint", mint.String())
	// Get the account info
	acc, err := hs.rpcClient.GetAccountInfo(context.Background(), mint)
	if err != nil {
		hs.logger.Error("Failed to get account info", "mint", mint.String(), "error", err)
		return 0, fmt.Errorf("failed to get account info: %w", err)
	}

	if acc == nil {
		hs.logger.Error("Token mint account not found", "mint", mint.String())
		return 0, fmt.Errorf("token mint account not found")
	}

	// Decode the mint data
	var mintData token.Mint
	if err := bin.NewBorshDecoder(acc.GetBinary()).Decode(&mintData); err != nil {
		hs.logger.Error("Failed to decode mint data", "mint", mint.String(), "error", err)
		return 0, fmt.Errorf("failed to decode mint data: %w", err)
	}
	hs.logDebug("Retrieved token decimals",
		"mint", mint.String(),
		"decimals", mintData.Decimals)
	return mintData.Decimals, nil
}

func (hs *HelperService) DeserializeAndExtractAccounts(encodedTx string) ([]solana.PublicKey, error) {
	hs.logDebug("Deserializing and extracting accounts from transaction")
	tx, err := hs.DecodeTransaction(encodedTx)
	if err != nil {
		return nil, err
	}

	var accounts []solana.PublicKey
	for _, instr := range tx.Message.Instructions {
		for _, keyIndex := range instr.Accounts {
			if int(keyIndex) < len(tx.Message.AccountKeys) {
				accountKey := tx.Message.AccountKeys[keyIndex]
				accounts = append(accounts, accountKey)
			}
		}
	}
	hs.logDebug("Extracted accounts from transaction",
		"numAccounts", len(accounts))
	return accounts, nil
}

func (hs *HelperService) GetTokenBalance(account solana.PublicKey) (uint64, error) {
	hs.logDebug("Getting token balance", "account", account.String())
	balance, err := hs.rpcClient.GetTokenAccountBalance(
		context.Background(),
		account,
		rpc.CommitmentConfirmed,
	)
	if err != nil {
		hs.logger.Error("Failed to get token balance", "account", account.String(), "error", err)
		return 0, fmt.Errorf("failed to get token balance: %w", err)
	}

	amount, err := strconv.ParseUint(balance.Value.Amount, 10, 64)
	if err != nil {
		hs.logger.Error("Failed to parse token balance",
			"account", account.String(),
			"rawAmount", balance.Value.Amount,
			"error", err)
		return 0, fmt.Errorf("failed to parse token balance: %w", err)
	}
	hs.logDebug("Retrieved token balance",
		"account", account.String(),
		"balance", amount)
	return amount, nil
}

func (hs *HelperService) CreateTipTransaction(ctx context.Context, feePayerPublicKey solana.PublicKey, tipAmount uint64, tipAccount solana.PublicKey) (*solana.Transaction, error) {
	recent, err := hs.rpcClient.GetLatestBlockhash(ctx, rpc.CommitmentFinalized)
	if err != nil {
		hs.logger.Error("failed to get recent blockhash", "error", err)
		return nil, fmt.Errorf("failed to get recent blockhash: %w", err)
	}

	tx, err := solana.NewTransaction(
		[]solana.Instruction{
			system.NewTransferInstruction(
				tipAmount,
				feePayerPublicKey,
				tipAccount,
			).Build(),
		},
		recent.Value.Blockhash,
		solana.TransactionPayer(feePayerPublicKey),
	)
	if err != nil {
		hs.logger.Error("failed to create transaction", "error", err)
		return nil, fmt.Errorf("failed to create transaction: %w", err)
	}

	// // Sign transaction
	// _, err = tx.Sign(func(key solana.PublicKey) *solana.PrivateKey {
	// 	if key == js.feePayer.PublicKey() {
	// 		return &js.feePayer
	// 	}
	// 	return nil
	// })
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to sign transaction: %w", err)
	// }
	// spew.Dump(tx)
	// // Pretty print the transaction:
	// tx.EncodeTree(text.NewTreeEncoder(os.Stdout, "Transfer SOL"))

	return tx, nil
}

func (hs *HelperService) contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func (hs *HelperService) remove(slice []string, item string) []string {
	for i, s := range slice {
		if s == item {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

// GetTokenBalanceChangeForMint extracts token balance changes from a transaction
func (hs *HelperService) GetTokenBalanceChangeForMint(txID string, mintAddress, ownerAddress string) (*TokenBalanceChange, error) {
	tx, err := hs.rpcClient.GetTransaction(context.Background(), solana.MustSignatureFromBase58(txID), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction: %w", err)
	}

	if tx.Meta == nil {
		return nil, errors.New("transaction metadata not available")
	}

	var preAmount, postAmount int64

	// Parse pre-token balances
	for _, balance := range tx.Meta.PreTokenBalances {
		if balance.Mint.String() == mintAddress && balance.Owner.String() == ownerAddress {
			amount, err := strconv.ParseInt(balance.UiTokenAmount.Amount, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse pre-token amount: %w", err)
			}
			preAmount = amount
			break
		}
	}

	// Parse post-token balances
	for _, balance := range tx.Meta.PostTokenBalances {
		if balance.Mint.String() == mintAddress && balance.Owner.String() == ownerAddress {
			amount, err := strconv.ParseInt(balance.UiTokenAmount.Amount, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse post-token amount: %w", err)
			}
			postAmount = amount
			break
		}
	}

	change := postAmount - preAmount
	if change == 0 {
		return nil, nil
	}

	return &TokenBalanceChange{
		MintStr: mintAddress,
		Change:  change,
	}, nil
}

type BalanceChange struct {
	MintAddress  string
	OwnerAddress string
	PreBalance   uint64
	PostBalance  uint64
	Change       int64 // PostBalance - PreBalance
}

type SolBalanceChange struct {
	AccountAddress string
	PreBalance     uint64
	PostBalance    uint64
	Change         int64
}

func (hs *HelperService) GetAllBalanceChanges(txID solana.Signature) (solChanges []SolBalanceChange, tokenChanges []BalanceChange, err error) {
	// Retry configuration
	maxRetries := 3
	retryDelay := 1 * time.Second

	// Use context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var tx *rpc.GetTransactionResult
	var parsedTx *solana.Transaction

	// Retry loop for GetTransaction
	for i := 0; i < maxRetries; i++ {
		tx, err = hs.rpcClient.GetTransaction(ctx, txID, nil)
		if err == nil {
			break
		}
		if i < maxRetries-1 {
			time.Sleep(retryDelay)
			continue
		}
		return nil, nil, fmt.Errorf("failed to get transaction after %d attempts: %w", maxRetries, err)
	}

	// Validate transaction metadata
	if tx.Meta == nil {
		return nil, nil, errors.New("transaction metadata not available")
	}

	// Try to get parsed transaction (but don't fail completely if this fails)
	parsedTx, parseErr := tx.Transaction.GetTransaction()
	if parseErr != nil {
		// We can still process token balances without parsedTx
		hs.logger.Warn("failed to parse transaction, will only return token balances", "error", parseErr)
	}

	// Process balances in parallel
	var wg sync.WaitGroup
	var solErr, tokenErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		if parsedTx != nil {
			solChanges, solErr = hs.processSolBalances(tx, parsedTx)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		tokenChanges, tokenErr = hs.processTokenBalances(tx)
	}()

	wg.Wait()

	// Handle partial results
	if solErr != nil && tokenErr != nil {
		return nil, nil, fmt.Errorf("both SOL and token balance processing failed: SOL error: %v, Token error: %v", solErr, tokenErr)
	}

	if solErr != nil {
		hs.logger.Warn("SOL balance processing failed, returning only token balances", "error", solErr)
		return nil, tokenChanges, nil
	}

	if tokenErr != nil {
		hs.logger.Warn("Token balance processing failed, returning only SOL balances", "error", tokenErr)
		return solChanges, nil, nil
	}

	return solChanges, tokenChanges, nil
}

func (hs *HelperService) processSolBalances(tx *rpc.GetTransactionResult, parsedTx *solana.Transaction) ([]SolBalanceChange, error) {
	balanceMap := make(map[string]SolBalanceChange)

	// Process pre-balances
	for i, balance := range tx.Meta.PreBalances {
		account := parsedTx.Message.AccountKeys[i]
		balanceMap[account.String()] = SolBalanceChange{
			AccountAddress: account.String(),
			PreBalance:     balance,
		}
	}

	// Process post-balances
	for i, balance := range tx.Meta.PostBalances {
		account := parsedTx.Message.AccountKeys[i]
		if existing, ok := balanceMap[account.String()]; ok {
			existing.PostBalance = balance
			existing.Change = int64(balance) - int64(existing.PreBalance)
			balanceMap[account.String()] = existing
		}
	}

	results := make([]SolBalanceChange, 0, len(balanceMap))
	for _, change := range balanceMap {
		results = append(results, change)
	}

	return results, nil
}

func (hs *HelperService) processTokenBalances(tx *rpc.GetTransactionResult) ([]BalanceChange, error) {
	balanceMap := make(map[string]BalanceChange)

	// Process pre-token balances
	for _, balance := range tx.Meta.PreTokenBalances {
		key := fmt.Sprintf("%s:%s", balance.Mint, balance.Owner)
		balanceMap[key] = BalanceChange{
			MintAddress:  balance.Mint.String(),
			OwnerAddress: balance.Owner.String(),
			PreBalance:   parseUint64(balance.UiTokenAmount.Amount),
		}
	}

	// Process post-token balances
	for _, balance := range tx.Meta.PostTokenBalances {
		key := fmt.Sprintf("%s:%s", balance.Mint, balance.Owner)
		if existing, ok := balanceMap[key]; ok {
			existing.PostBalance = parseUint64(balance.UiTokenAmount.Amount)
			existing.Change = int64(existing.PostBalance) - int64(existing.PreBalance)
			balanceMap[key] = existing
		} else {
			balanceMap[key] = BalanceChange{
				MintAddress:  balance.Mint.String(),
				OwnerAddress: balance.Owner.String(),
				PostBalance:  parseUint64(balance.UiTokenAmount.Amount),
			}
		}
	}

	results := make([]BalanceChange, 0, len(balanceMap))
	for _, change := range balanceMap {
		results = append(results, change)
	}

	return results, nil
}

// func (hs *HelperService) GetTokenBalanceChanges(txID solana.Signature) ([]BalanceChange, error) {
// 	tx, err := hs.rpcClient.GetTransaction(context.Background(), txID, nil)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get transaction: %w", err)
// 	}

// 	if tx.Meta == nil {
// 		return nil, errors.New("transaction metadata not available")
// 	}

// 	balanceMap := make(map[string]BalanceChange)

// 	// Process pre-token balances
// 	for _, balance := range tx.Meta.PreTokenBalances {
// 		key := fmt.Sprintf("%s:%s", balance.Mint, balance.Owner)
// 		balanceMap[key] = BalanceChange{
// 			MintAddress:  balance.Mint.String(),
// 			OwnerAddress: balance.Owner.String(),
// 			PreBalance:   parseUint64(balance.UiTokenAmount.Amount),
// 		}
// 	}

// 	// Process post-token balances
// 	for _, balance := range tx.Meta.PostTokenBalances {
// 		key := fmt.Sprintf("%s:%s", balance.Mint, balance.Owner)
// 		if existing, ok := balanceMap[key]; ok {
// 			existing.PostBalance = parseUint64(balance.UiTokenAmount.Amount)
// 			existing.Change = int64(existing.PostBalance) - int64(existing.PreBalance)
// 			balanceMap[key] = existing
// 		} else {
// 			balanceMap[key] = BalanceChange{
// 				MintAddress:  balance.Mint.String(),
// 				OwnerAddress: balance.Owner.String(),
// 				PostBalance:  parseUint64(balance.UiTokenAmount.Amount),
// 			}
// 		}
// 	}

// 	results := make([]BalanceChange, 0, len(balanceMap))
// 	for _, change := range balanceMap {
// 		results = append(results, change)
// 	}

// 	return results, nil
// }

// func (hs *HelperService) GetSolBalanceChanges(txID solana.Signature) ([]SolBalanceChange, error) {
// 	tx, err := hs.rpcClient.GetTransaction(context.Background(), txID, nil)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get transaction: %w", err)
// 	}

// 	if tx.Meta == nil {
// 		return nil, errors.New("transaction metadata not available")
// 	}

// 	// Get the parsed transaction
// 	parsedTx, err := tx.Transaction.GetTransaction()
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to get parsed transaction: %w", err)
// 	}

// 	balanceMap := make(map[string]SolBalanceChange)

// 	// Process pre-balances
// 	for i, balance := range tx.Meta.PreBalances {
// 		account := parsedTx.Message.AccountKeys[i]
// 		balanceMap[account.String()] = SolBalanceChange{
// 			AccountAddress: account.String(),
// 			PreBalance:     balance,
// 		}
// 	}

// 	// Process post-balances
// 	for i, balance := range tx.Meta.PostBalances {
// 		account := parsedTx.Message.AccountKeys[i]
// 		if existing, ok := balanceMap[account.String()]; ok {
// 			existing.PostBalance = balance
// 			existing.Change = int64(balance) - int64(existing.PreBalance)
// 			balanceMap[account.String()] = existing
// 		}
// 	}

// 	results := make([]SolBalanceChange, 0, len(balanceMap))
// 	for _, change := range balanceMap {
// 		results = append(results, change)
// 	}

// 	return results, nil
// }

func parseUint64(s string) uint64 {
	val, _ := strconv.ParseUint(s, 10, 64)
	return val
}

func (hs *HelperService) logDebug(msg string, args ...any) {
	if hs.debug {
		hs.logger.Debug(msg, args...)
	}
}

func (hs *HelperService) logInfo(msg string, args ...any) {
	if hs.debug {
		hs.logger.Info(msg, args...)
	}
}
