package ledger

import (
	"errors"
	"fmt"
	"sync"

	"github.com/holiman/uint256"
	"github.com/mezonai/mmn/store"

	"github.com/mezonai/mmn/block"
	"github.com/mezonai/mmn/config"
	"github.com/mezonai/mmn/events"
	"github.com/mezonai/mmn/transaction"
	"github.com/mezonai/mmn/types"
	"github.com/mezonai/mmn/utils"
)

var (
	ErrAccountExisted = errors.New("account existed")
)

type Ledger struct {
	mu           sync.RWMutex
	txStore      store.TxStore
	accountStore store.AccountStore
	eventRouter  *events.EventRouter
}

func NewLedger(txStore store.TxStore, accountStore store.AccountStore, eventRouter *events.EventRouter) *Ledger {
	return &Ledger{
		txStore:      txStore,
		accountStore: accountStore,
		eventRouter:  eventRouter,
	}
}

// CreateAccount creates and stores a new account into db, return error if an account with the same addr existed
func (l *Ledger) CreateAccount(addr string, balance *uint256.Int) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	_, err := l.createAccountWithoutLocking(addr, balance)
	return err
}

// createAccountWithoutLocking creates account and store in db without locking ledger. This is useful
// when calling method has already acquired lock for ledger to avoid recursive locking and deadlock
func (l *Ledger) createAccountWithoutLocking(addr string, balance *uint256.Int) (*types.Account, error) {
	existed, err := l.accountStore.ExistsByAddr(addr)
	if err != nil {
		return nil, fmt.Errorf("could not check existence of account: %w", err)
	}
	if existed {
		return nil, ErrAccountExisted
	}

	account := &types.Account{
		Address: addr,
		Balance: balance,
		Nonce:   0,
	}
	err = l.accountStore.Store(account)
	if err != nil {
		return nil, fmt.Errorf("failed to store account: %w", err)
	}

	return account, nil
}

// CreateAccountsFromGenesis creates an account from genesis block (implements LedgerInterface)
func (l *Ledger) CreateAccountsFromGenesis(addrs []config.Address) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, addr := range addrs {
		_, err := l.createAccountWithoutLocking(addr.Address, addr.Amount)
		if err != nil {
			return fmt.Errorf("could not create genesis account %s: %w", addr.Address, err)
		}
	}
	return nil
}

// AccountExists checks if an account exists (implements LedgerInterface)
func (l *Ledger) AccountExists(addr string) (bool, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.accountStore.ExistsByAddr(addr)
}

// Balance returns current balance for addr
func (l *Ledger) Balance(addr string) (*uint256.Int, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	acc, err := l.accountStore.GetByAddr(addr)
	if err != nil {
		return uint256.NewInt(0), err
	}

	return acc.Balance, nil
}

func (l *Ledger) VerifyBlock(b *block.BroadcastedBlock) error {
	l.mu.RLock()
	base := l.accountStore
	l.mu.RUnlock()

	view := &LedgerView{
		base:    base,
		overlay: make(map[string]*types.SnapshotAccount),
	}
	for _, entry := range b.Entries {
		for _, tx := range entry.Transactions {
			if err := view.ApplyTx(tx); err != nil {
				return fmt.Errorf("verify fail: %v", err)
			}
		}
	}
	return nil
}

func (l *Ledger) ApplyBlock(b *block.Block) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Printf("[ledger] Applying block %d\n", b.Slot)

	for _, entry := range b.Entries {
		txs, err := l.txStore.GetBatch(entry.TxHashes)
		if err != nil {
			return err
		}

		for _, tx := range txs {
			// load account state
			sender, err := l.accountStore.GetByAddr(tx.Sender)
			if err != nil {
				return err
			}
			if sender == nil {
				if sender, err = l.createAccountWithoutLocking(tx.Sender, uint256.NewInt(0)); err != nil {
					return err
				}
			}
			recipient, err := l.accountStore.GetByAddr(tx.Recipient)
			if err != nil {
				return err
			}
			if recipient == nil {
				if recipient, err = l.createAccountWithoutLocking(tx.Recipient, uint256.NewInt(0)); err != nil {
					return err
				}
			}
			state := map[string]*types.Account{
				sender.Address:    sender,
				recipient.Address: recipient,
			}

			// try to apply tx
			if err := applyTx(state, tx); err != nil {
				// Publish specific transaction failure event
				if l.eventRouter != nil {
					txHash := tx.Hash()
					event := events.NewTransactionFailed(txHash, fmt.Sprintf("transaction application failed: %v", err))
					l.eventRouter.PublishTransactionEvent(event)
				}
				return fmt.Errorf("apply fail: %v", err)
			}
			fmt.Printf("Applied tx %s\n", tx.Hash())
			addHistory(state[tx.Sender], tx)
			if tx.Recipient != tx.Sender {
				addHistory(state[tx.Recipient], tx)
			}

			// commit the update
			if err := l.accountStore.StoreBatch([]*types.Account{sender, recipient}); err != nil {
				if l.eventRouter != nil {
					event := events.NewTransactionFailed("", fmt.Sprintf("WAL write failed for block %d: %v", b.Slot, err))
					l.eventRouter.PublishTransactionEvent(event)
				}
				return err
			}
		}
	}

	fmt.Printf("[ledger] Block %d applied\n", b.Slot)
	return nil
}

func (l *Ledger) NewSession() *Session {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return &Session{
		ledger: l,
		view: &LedgerView{
			base:    l.accountStore,
			overlay: make(map[string]*types.SnapshotAccount),
		},
	}
}

// GetAccount returns account with addr (nil if not exist)
func (l *Ledger) GetAccount(addr string) (*types.Account, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.accountStore.GetByAddr(addr)
}

// Apply transaction to ledger (after verifying signature). NOTE: this does not perform persisting operation into db
func applyTx(state map[string]*types.Account, tx *transaction.Transaction) error {
	sender, ok := state[tx.Sender]
	if !ok {
		state[tx.Sender] = &types.Account{Address: tx.Sender, Balance: uint256.NewInt(0), Nonce: 0}
		sender = state[tx.Sender]
	}
	recipient, ok := state[tx.Recipient]
	if !ok {
		state[tx.Recipient] = &types.Account{Address: tx.Recipient, Balance: uint256.NewInt(0), Nonce: 0}
		recipient = state[tx.Recipient]
	}

	if sender.Balance.Cmp(tx.Amount) < 0 {
		return fmt.Errorf("insufficient balance")
	}
	// Strict nonce validation to prevent duplicate transactions
	if tx.Nonce != sender.Nonce+1 {
		return fmt.Errorf("invalid nonce: expected %d, got %d", sender.Nonce+1, tx.Nonce)
	}
	sender.Balance.Sub(sender.Balance, tx.Amount)
	recipient.Balance.Add(recipient.Balance, tx.Amount)
	sender.Nonce = tx.Nonce
	return nil
}

func addHistory(acc *types.Account, tx *transaction.Transaction) {
	acc.History = append(acc.History, tx.Hash())
}

func (l *Ledger) GetTxByHash(hash string) (*transaction.Transaction, error) {
	tx, err := l.txStore.GetByHash(hash)
	if err != nil {
		return nil, err
	}
	return tx, nil
}

func (l *Ledger) GetTxs(addr string, limit uint32, offset uint32, filter uint32) (uint32, []*transaction.Transaction) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	txs := make([]*transaction.Transaction, 0)
	acc, err := l.accountStore.GetByAddr(addr)
	if err != nil {
		return 0, txs
	}

	// filter type: 0: all, 1: sender, 2: recipient
	filteredHistory := make([]*transaction.Transaction, 0)
	transactions, err := l.txStore.GetBatch(acc.History)
	if err != nil {
		return 0, txs
	}
	for _, tx := range transactions {
		if filter == 0 {
			filteredHistory = append(filteredHistory, tx)
		} else if filter == 1 && tx.Sender == addr {
			filteredHistory = append(filteredHistory, tx)
		} else if filter == 2 && tx.Recipient == addr {
			filteredHistory = append(filteredHistory, tx)
		}
	}

	total := uint32(len(filteredHistory))
	start := offset
	end := min(start+limit, total)
	txs = filteredHistory[start:end]

	return total, txs
}

// LedgerView is a view of the ledger to verify block before applying it to the ledger.
type LedgerView struct {
	base    store.AccountStore
	overlay map[string]*types.SnapshotAccount
}

func (lv *LedgerView) loadForRead(addr string) (*types.SnapshotAccount, bool) {
	if acc, ok := lv.overlay[addr]; ok {
		return acc, true
	}
	base, err := lv.base.GetByAddr(addr)
	// TODO: re-verify this, will returning nil for error case be ok?
	if err != nil || base == nil {
		return nil, false
	}

	cp := types.SnapshotAccount{Balance: base.Balance, Nonce: base.Nonce}
	lv.overlay[addr] = &cp
	return &cp, true
}

func (lv *LedgerView) loadOrCreate(addr string) *types.SnapshotAccount {
	if acc, ok := lv.loadForRead(addr); ok {
		return acc
	}
	cp := types.SnapshotAccount{Balance: uint256.NewInt(0), Nonce: 0}
	lv.overlay[addr] = &cp
	return &cp
}

func (lv *LedgerView) ApplyTx(tx *transaction.Transaction) error {
	// Validate zero amount transfers
	if tx.Amount.Cmp(uint256.NewInt(0)) == 0 {
		return fmt.Errorf("zero amount transfers are not allowed")
	}

	// Validate sender account existence
	if _, exists := lv.loadForRead(tx.Sender); !exists {
		return fmt.Errorf("sender account does not exist: %s", tx.Sender)
	}

	sender := lv.loadOrCreate(tx.Sender)
	recipient := lv.loadOrCreate(tx.Recipient)

	if sender.Balance.Cmp(tx.Amount) < 0 {
		return fmt.Errorf("insufficient balance")
	}
	// Strict nonce validation to prevent duplicate transactions (Ethereum standard)
	if tx.Nonce != sender.Nonce+1 {
		return fmt.Errorf("invalid nonce: expected %d, got %d", sender.Nonce+1, tx.Nonce)
	}

	sender.Balance.Sub(sender.Balance, tx.Amount)
	recipient.Balance.Add(recipient.Balance, tx.Amount)
	sender.Nonce = tx.Nonce
	return nil
}

type Session struct {
	ledger *Ledger
	view   *LedgerView
}

// Copy session with clone overlay
func (s *Session) CopyWithOverlayClone() *Session {
	overlayCopy := make(map[string]*types.SnapshotAccount, len(s.view.overlay))
	for k, v := range s.view.overlay {
		accCopy := *v
		overlayCopy[k] = &accCopy
	}

	return &Session{
		ledger: s.ledger,
		view: &LedgerView{
			base:    s.view.base,
			overlay: overlayCopy,
		},
	}
}

// Session API for filtering valid transactions
func (s *Session) FilterValid(raws [][]byte) ([]*transaction.Transaction, []error) {
	valid := make([]*transaction.Transaction, 0, len(raws))
	errs := make([]error, 0)
	for _, r := range raws {
		tx, err := utils.ParseTx(r)
		if err != nil || !tx.Verify() {
			fmt.Printf("Invalid tx: %v, %+v\n", err, tx)
			if s.ledger.eventRouter != nil {
				event := events.NewTransactionFailed(tx.Hash(), fmt.Sprintf("sig/format: %v", err))
				s.ledger.eventRouter.PublishTransactionEvent(event)
			}
			errs = append(errs, fmt.Errorf("sig/format: %w", err))
			continue
		}
		if err := s.view.ApplyTx(tx); err != nil {
			fmt.Printf("Invalid tx: %v, %+v\n", err, tx)
			if s.ledger.eventRouter != nil {
				event := events.NewTransactionFailed(tx.Hash(), err.Error())
				s.ledger.eventRouter.PublishTransactionEvent(event)
			}
			errs = append(errs, err)
			continue
		}
		valid = append(valid, tx)
	}
	return valid, errs
}
