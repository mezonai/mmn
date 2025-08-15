/**
 * Comprehensive gRPC Blockchain Test Suite
 * 
 * Usage:
 *   npm run test                    # Run tests without debug output
 *   npm run test -- --debug         # Run tests with debug output (shows transaction updates)
 *   npm run test -- -d              # Short form for debug mode
 * 
 * Debug mode shows:
 *   - Raw transaction status updates from server
 *   - Processed transaction updates
 */

import crypto from 'crypto';
import nacl from 'tweetnacl';
import { GrpcClient } from './grpc_client';
import { TransactionTracker } from './transaction_tracker';
import { TransactionStatus } from './generated/tx';

// Fixed Ed25519 keypair for faucet (hardcoded for genesis config)
const faucetPrivateKeyHex =
  '302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee';
const faucetPrivateKeyDer = Buffer.from(faucetPrivateKeyHex, 'hex');
const faucetSeed = faucetPrivateKeyDer.slice(-32);
const faucetKeyPair = nacl.sign.keyPair.fromSeed(faucetSeed);
const faucetPublicKeyHex = Buffer.from(faucetKeyPair.publicKey).toString('hex');
const faucetPrivateKey = crypto.createPrivateKey({
  key: faucetPrivateKeyDer,
  format: 'der',
  type: 'pkcs8',
});

// Generate a new test account
function generateTestAccount() {
  const seed = crypto.randomBytes(32);
  const keyPair = nacl.sign.keyPair.fromSeed(seed);
  const publicKeyHex = Buffer.from(keyPair.publicKey).toString('hex');
  const privateKey = crypto.createPrivateKey({
    key: Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), seed]),
    format: 'der',
    type: 'pkcs8',
  });
  return { publicKeyHex, privateKey, keyPair, seed };
}

// Generate multiple test accounts
function generateTestAccounts(count: number) {
  return Array.from({ length: count }, () => generateTestAccount());
}

const GRPC_SERVER_ADDRESS = '127.0.0.1:9001';

interface Tx {
  type: number;
  sender: string;
  recipient: string;
  amount: number;
  timestamp: number;
  text_data: string;
  nonce: number;
  signature: string;
}

function buildTx(
  sender: string,
  recipient: string,
  amount: number,
  text_data: string,
  nonce: number,
  type: number
): Tx {
  return {
    type: type,
    sender: sender,
    recipient: recipient,
    amount: amount,
    timestamp: Math.floor(Date.now() / 1000),
    text_data: text_data,
    nonce: nonce,
    signature: '',
  };
}

function serializeTx(tx: Tx): Buffer {
  const metadata = `${tx.type}|${tx.sender}|${tx.recipient}|${tx.amount}|${tx.text_data}|${tx.nonce}`;
  return Buffer.from(metadata);
}

function signTx(tx: Tx, privateKey: crypto.KeyObject): string {
  const serializedData = serializeTx(tx);
  const signature = crypto.sign(null, serializedData, privateKey);
  return signature.toString('hex');
}

function verifyTx(tx: Tx, publicKeyHex: string): boolean {
  const spkiPrefix = Buffer.from('302a300506032b6570032100', 'hex');
  const publicKeyDer = Buffer.concat([spkiPrefix, Buffer.from(publicKeyHex, 'hex')]);
  const publicKey = crypto.createPublicKey({
    key: publicKeyDer,
    format: 'der',
    type: 'spki',
  });

  const signature = Buffer.from(tx.signature, 'hex');
  const serializedData = serializeTx(tx);
  return crypto.verify(null, serializedData, publicKey, signature);
}

// Helper function to get next nonce for an account (current nonce + 1)
async function getNextNonce(grpcClient: GrpcClient, address: string): Promise<number> {
  try {
    const accountInfo = await grpcClient.getAccount(address);
    return parseInt(accountInfo.nonce) + 1;
  } catch (error) {
    // If account doesn't exist, return 0 as starting nonce
    return 0;
  }
}

// gRPC methods
async function sendTxViaGrpc(grpcClient: GrpcClient, tx: Tx) {
  const txMsg = {
    type: tx.type,
    sender: tx.sender,
    recipient: tx.recipient,
    amount: tx.amount,
    timestamp: tx.timestamp,
    text_data: tx.text_data,
    nonce: tx.nonce,
  };
  return await grpcClient.addTransaction(txMsg, tx.signature);
}

const FaucetTxType = 1;
const TransferTxType = 0;

class TestSuite {
  private grpcClient: GrpcClient;
  private transactionTracker: TransactionTracker;
  private testResults: Map<string, boolean> = new Map();

  constructor(debug: boolean = false) {
    this.grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS, debug);
    this.transactionTracker = new TransactionTracker({ serverAddress: GRPC_SERVER_ADDRESS });
  }

  private logTest(name: string, success: boolean, details?: string) {
    this.testResults.set(name, success);
    const status = success ? '✓ PASS' : '✗ FAIL';
    console.log(`${status} ${name}${details ? ` - ${details}` : ''}`);
  }

  private async waitForTransactionFinalization(txHash: string, timeoutMs: number = 30000): Promise<void> {
    try {
      const status = await this.transactionTracker.waitForTerminalStatus(txHash, timeoutMs);
      
      if (status.status === TransactionStatus.FAILED) {
        throw new Error(`Transaction ${txHash.substring(0, 16)}... failed: ${status.errorMessage || 'Unknown error'}`);
      }
      
      console.log(`✅ Transaction ${txHash.substring(0, 16)}... finalized`);
    } catch (error) {
      throw new Error(`Transaction finalization error: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  private async waitForTransactionFailure(txHash: string, timeoutMs: number = 30000): Promise<void> {
    try {
      const status = await this.transactionTracker.waitForTerminalStatus(txHash, timeoutMs);
      
      if (status.status === TransactionStatus.FAILED) {
        console.log(`✅ Transaction ${txHash.substring(0, 16)}... failed as expected: ${status.errorMessage || 'Unknown error'}`);
        return;
      }
      
      throw new Error(`Transaction ${txHash.substring(0, 16)}... was expected to fail but reached status: ${status.status}`);
    } catch (error) {
      throw new Error(`Transaction failure check error: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  private async runTest(name: string, testFn: () => Promise<void>) {
    try {
      await testFn();
      this.logTest(name, true);
    } catch (error) {
      this.logTest(name, false, error instanceof Error ? error.message : String(error));
    }
  }

  async runAllTests() {
    console.log('=== COMPREHENSIVE gRPC-ONLY BLOCKCHAIN TEST SUITE ===\n');

    // Basic Functionality Tests
    await this.runTest('Basic Faucet Transaction', () => this.testBasicFaucetTransaction());
    await this.runTest('Basic Transfer Transaction', () => this.testBasicTransferTransaction());
    await this.runTest('Account Balance Verification', () => this.testAccountBalanceVerification());
    await this.runTest('Transaction History', () => this.testTransactionHistory());

    // Edge Cases
    await this.runTest('Self Transfer Transaction', () => this.testSelfTransferTransaction());
    await this.runTest('Duplicate Transaction', () => this.testDuplicateTransaction());

    // Error Handling
    await this.runTest('Non-existent Account Query', () => this.testNonExistentAccountQuery());
    await this.runTest('Invalid Transaction (Insufficient Balance)', () => this.testInvalidTransactionInsufficientBalance());

    // Multi-Account Tests
    await this.runTest('Multi-Account Transfer Chain', () => this.testMultiAccountTransferChain());

    // Filtering and Pagination
    await this.runTest('Transaction History Filtering', () => this.testTransactionHistoryFiltering());
    await this.runTest('Transaction History Pagination', () => this.testTransactionHistoryPagination());

    this.printTestSummary();
  }

  // Method to run a single test with debug output
  async runSingleTest(testName: string, testFn: () => Promise<void>, enableDebug: boolean = false) {
    if (enableDebug) {
      this.setDebug(true);
      console.log(`🔍 Running test "${testName}" with debug output...\n`);
    }
    
    await this.runTest(testName, testFn);
    
    if (enableDebug) {
      this.setDebug(false);
    }
  }

  private async testBasicFaucetTransaction() {
    const account = generateTestAccount();
    
    // Get next nonce for faucet account
    const nextNonce = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const tx = buildTx(faucetPublicKeyHex, account.publicKeyHex, 100, 'Basic faucet test', nextNonce, FaucetTxType);
    tx.signature = signTx(tx, faucetPrivateKey);

    const response = await sendTxViaGrpc(this.grpcClient, tx);
    if (!response.ok) throw new Error('Faucet transaction failed');

    // Wait for transaction to be finalized using transaction tracker
    if (response.tx_hash) {
      await this.waitForTransactionFinalization(response.tx_hash);
    } else {
      throw new Error('Transaction hash not returned from server');
    }
  }

  private async testBasicTransferTransaction() {
    const sender = generateTestAccount();
    const recipient = generateTestAccount();

    // First fund the sender
    const faucetNonce = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const faucetTx = buildTx(faucetPublicKeyHex, sender.publicKeyHex, 200, 'Fund sender', faucetNonce, FaucetTxType);
    faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
    const faucetResponse = await sendTxViaGrpc(this.grpcClient, faucetTx);
    if (!faucetResponse.ok) throw new Error('Faucet transaction failed');
    
    // Wait for faucet transaction to be finalized
    if (faucetResponse.tx_hash) {
      await this.waitForTransactionFinalization(faucetResponse.tx_hash);
    }

    // Then transfer
    const senderNonce = await getNextNonce(this.grpcClient, sender.publicKeyHex);
    const transferTx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 50, 'Basic transfer', senderNonce, TransferTxType);
    transferTx.signature = signTx(transferTx, sender.privateKey);

    const response = await sendTxViaGrpc(this.grpcClient, transferTx);
    if (!response.ok) throw new Error('Transfer transaction failed');

    // Wait for transfer transaction to be finalized
    if (response.tx_hash) {
      await this.waitForTransactionFinalization(response.tx_hash);
    } else {
      throw new Error('Transaction hash not returned from server');
    }
  }

  private async testAccountBalanceVerification() {
    const account = generateTestAccount();

    // Fund account
    const faucetNonce = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const faucetTx = buildTx(faucetPublicKeyHex, account.publicKeyHex, 300, 'Fund for balance test', faucetNonce, FaucetTxType);
    faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
    const response = await sendTxViaGrpc(this.grpcClient, faucetTx);
    if (!response.ok) throw new Error('Faucet transaction failed');

    // Wait for transaction to be finalized using transaction tracker
    if (response.tx_hash) {
      await this.waitForTransactionFinalization(response.tx_hash);
    } else {
      throw new Error('Transaction hash not returned from server');
    }

    // Verify balance via gRPC
    const accountInfo = await this.grpcClient.getAccount(account.publicKeyHex);
    if (parseInt(accountInfo.balance) !== 300) throw new Error(`Expected balance 300, got ${accountInfo.balance}`);
  }

  private async testTransactionHistory() {
    const account = generateTestAccount();

    // Create multiple transactions
    const faucetNonce = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const faucetTx = buildTx(faucetPublicKeyHex, account.publicKeyHex, 500, 'Fund for history test', faucetNonce, FaucetTxType);
    faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
    const faucetResponse = await sendTxViaGrpc(this.grpcClient, faucetTx);
    if (!faucetResponse.ok) throw new Error('Faucet transaction failed');

    // Wait for faucet transaction to be finalized
    if (faucetResponse.tx_hash) {
      await this.waitForTransactionFinalization(faucetResponse.tx_hash);
    }

    const accountNonce = await getNextNonce(this.grpcClient, account.publicKeyHex);
    const transferTx = buildTx(
      account.publicKeyHex,
      generateTestAccount().publicKeyHex,
      100,
      'History test transfer',
      accountNonce,
      TransferTxType
    );
    transferTx.signature = signTx(transferTx, account.privateKey);
    const transferResponse = await sendTxViaGrpc(this.grpcClient, transferTx);
    if (!transferResponse.ok) throw new Error('Transfer transaction failed');

    // Wait for transfer transaction to be finalized
    if (transferResponse.tx_hash) {
      await this.waitForTransactionFinalization(transferResponse.tx_hash);
    }

    // Check history via gRPC
    const history = await this.grpcClient.getTxHistory(account.publicKeyHex, 10, 0, 0);
    if (history.total < 2) throw new Error('Transaction history incomplete');
  }

  private async testSelfTransferTransaction() {
    const account = generateTestAccount();

    // Fund account
    const faucetNonce = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const faucetTx = buildTx(faucetPublicKeyHex, account.publicKeyHex, 100, 'Fund for self transfer', faucetNonce, FaucetTxType);
    faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
    const faucetResponse = await sendTxViaGrpc(this.grpcClient, faucetTx);
    if (!faucetResponse.ok) throw new Error('Faucet transaction failed');

    // Wait for faucet transaction to be finalized
    if (faucetResponse.tx_hash) {
      await this.waitForTransactionFinalization(faucetResponse.tx_hash);
    }

    // Self transfer
    const accountNonce = await getNextNonce(this.grpcClient, account.publicKeyHex);
    const selfTx = buildTx(account.publicKeyHex, account.publicKeyHex, 50, 'Self transfer', accountNonce, TransferTxType);
    selfTx.signature = signTx(selfTx, account.privateKey);

    const response = await sendTxViaGrpc(this.grpcClient, selfTx);
    if (!response.ok) throw new Error('Self transfer should be valid');

    // Wait for self transfer transaction to be finalized
    if (response.tx_hash) {
      await this.waitForTransactionFinalization(response.tx_hash);
    } else {
      throw new Error('Transaction hash not returned from server');
    }
  }

  private async testDuplicateTransaction() {
    const sender = generateTestAccount();
    const recipient = generateTestAccount();

    // Fund sender
    const faucetNonce = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const faucetTx = buildTx(faucetPublicKeyHex, sender.publicKeyHex, 100, 'Fund for duplicate test', faucetNonce, FaucetTxType);
    faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
    const faucetResponse = await sendTxViaGrpc(this.grpcClient, faucetTx);
    if (!faucetResponse.ok) throw new Error('Faucet transaction failed');

    console.log("Waiting for faucet transaction to be finalized");

    // Wait for faucet transaction to be finalized
    if (faucetResponse.tx_hash) {
      await this.waitForTransactionFinalization(faucetResponse.tx_hash);
    }

    // Create transaction
    const senderNonce = await getNextNonce(this.grpcClient, sender.publicKeyHex);
    const tx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 10, 'Duplicate test', senderNonce, TransferTxType);
    tx.signature = signTx(tx, sender.privateKey);

    // Send first time
    const response1 = await sendTxViaGrpc(this.grpcClient, tx);
    if (!response1.ok) throw new Error('First transaction should succeed');

    console.log("Waiting for first transaction to be finalized");

    // Wait for first transaction to be finalized
    if (response1.tx_hash) {
      await this.waitForTransactionFinalization(response1.tx_hash);
    }

    // Send duplicate
    const response2 = await sendTxViaGrpc(this.grpcClient, tx);
    if (!response2.ok) {
      console.log('✅ Duplicate transaction correctly rejected by server');
      return;
    }

    console.log("Waiting for second transaction to be failed");
    
    // If the server accepted the duplicate, wait for it to fail during processing
    if (response2.tx_hash) {
      await this.waitForTransactionFailure(response2.tx_hash);
    } else {
      throw new Error('Duplicate transaction was accepted but no transaction hash returned');
    }
  }

  private async testNonExistentAccountQuery() {
    const nonExistentAddress = '0000000000000000000000000000000000000000000000000000000000000000';

    try {
      await this.grpcClient.getAccount(nonExistentAddress);
      throw new Error('Non-existent account query should have failed');
    } catch (error) {
      // Expected to fail
    }
  }

  private async testInvalidTransactionInsufficientBalance() {
    const sender = generateTestAccount();
    const recipient = generateTestAccount();

    // Create a transaction with insufficient balance (no funding)
    const senderNonce = await getNextNonce(this.grpcClient, sender.publicKeyHex);
    const tx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 100, 'Insufficient balance test', senderNonce, TransferTxType);
    tx.signature = signTx(tx, sender.privateKey);

    const response = await sendTxViaGrpc(this.grpcClient, tx);
    if (!response.ok) {
      console.log('✅ Invalid transaction correctly rejected by server');
      return;
    }

    // If the server accepted the transaction, wait for it to fail during processing
    if (response.tx_hash) {
      await this.waitForTransactionFailure(response.tx_hash);
    } else {
      throw new Error('Invalid transaction was accepted but no transaction hash returned');
    }
  }

  private async testMultiAccountTransferChain() {
    // Create a chain of transfers: A -> B -> C -> D
    const accounts = generateTestAccounts(4);

    // Fund all accounts first to ensure they exist
    console.log('Funding all accounts for chain transfer test...');
    for (let i = 0; i < accounts.length; i++) {
      const faucetNonce = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
      const faucetTx = buildTx(faucetPublicKeyHex, accounts[i].publicKeyHex, 100, `Fund account ${i}`, faucetNonce, FaucetTxType);
      faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
      const response = await sendTxViaGrpc(this.grpcClient, faucetTx);
      if (!response.ok) throw new Error(`Faucet transaction for account ${i} failed`);
      
      // Wait for each faucet transaction to be finalized
      if (response.tx_hash) {
        await this.waitForTransactionFinalization(response.tx_hash);
      }
    }

    // Fund first account with additional amount for transfers
    const additionalFaucetNonce = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const additionalFundTx = buildTx(faucetPublicKeyHex, accounts[0].publicKeyHex, 900, 'Additional fund for chain', additionalFaucetNonce, FaucetTxType);
    additionalFundTx.signature = signTx(additionalFundTx, faucetPrivateKey);
    const additionalResponse = await sendTxViaGrpc(this.grpcClient, additionalFundTx);
    if (!additionalResponse.ok) throw new Error('Additional funding transaction failed');

    // Wait for additional funding transaction to be finalized
    if (additionalResponse.tx_hash) {
      await this.waitForTransactionFinalization(additionalResponse.tx_hash);
    }

    console.log('All funding transactions finalized, proceeding with chain transfers...');

    // Chain transfers
    for (let i = 0; i < accounts.length - 1; i++) {
      const accountNonce = await getNextNonce(this.grpcClient, accounts[i].publicKeyHex);
      const tx = buildTx(
        accounts[i].publicKeyHex,
        accounts[i + 1].publicKeyHex,
        200,
        `Chain transfer ${i}`,
        accountNonce,
        TransferTxType
      );
      tx.signature = signTx(tx, accounts[i].privateKey);

      const response = await sendTxViaGrpc(this.grpcClient, tx);
      if (!response.ok) throw new Error(`Chain transfer ${i} failed`);

      // Wait for each chain transfer to be finalized
      if (response.tx_hash) {
        await this.waitForTransactionFinalization(response.tx_hash);
      }
    }

    console.log('All chain transfers finalized, verifying balances...');

    // Verify balances after chain transfers
    // Expected balances:
    // Account 0: 100 (initial) + 900 (additional) - 200 (sent) = 800
    // Account 1: 100 (initial) + 200 (received) - 200 (sent) = 100
    // Account 2: 100 (initial) + 200 (received) - 200 (sent) = 100
    // Account 3: 100 (initial) + 200 (received) = 300

    const expectedBalances = [800, 100, 100, 300];

    for (let i = 0; i < accounts.length; i++) {
      const accountInfo = await this.grpcClient.getAccount(accounts[i].publicKeyHex);
      const expectedBalance = expectedBalances[i];
      
      console.log(`Account ${i} balance: ${accountInfo.balance}, expected: ${expectedBalance}`);
      
      if (parseInt(accountInfo.balance) !== expectedBalance) {
        throw new Error(
          `Account ${i} balance verification failed: expected ${expectedBalance}, got ${accountInfo.balance}`
        );
      }
    }

    console.log('✅ All account balances verified correctly after chain transfers');
  }

  private async testTransactionHistoryFiltering() {
    const account = generateTestAccount();
    const recipient = generateTestAccount();

    // Fund both accounts
    const faucetNonce1 = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const faucetTx1 = buildTx(faucetPublicKeyHex, account.publicKeyHex, 1000, 'Fund for filtering', faucetNonce1, FaucetTxType);
    faucetTx1.signature = signTx(faucetTx1, faucetPrivateKey);
    const response1 = await sendTxViaGrpc(this.grpcClient, faucetTx1);
    if (!response1.ok) throw new Error('First faucet transaction failed');

    const faucetNonce2 = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const faucetTx2 = buildTx(faucetPublicKeyHex, recipient.publicKeyHex, 100, 'Fund recipient', faucetNonce2, FaucetTxType);
    faucetTx2.signature = signTx(faucetTx2, faucetPrivateKey);
    const response2 = await sendTxViaGrpc(this.grpcClient, faucetTx2);
    if (!response2.ok) throw new Error('Second faucet transaction failed');

    // Wait for funding transactions to be finalized
    if (response1.tx_hash) {
      await this.waitForTransactionFinalization(response1.tx_hash);
    }
    if (response2.tx_hash) {
      await this.waitForTransactionFinalization(response2.tx_hash);
    }

    const accountNonce = await getNextNonce(this.grpcClient, account.publicKeyHex);
    const transferTx = buildTx(
      account.publicKeyHex,
      recipient.publicKeyHex,
      200,
      'Filtering transfer',
      accountNonce,
      TransferTxType
    );
    transferTx.signature = signTx(transferTx, account.privateKey);
    const transferResponse = await sendTxViaGrpc(this.grpcClient, transferTx);
    if (!transferResponse.ok) throw new Error('Transfer transaction failed');

    // Wait for transfer transaction to be finalized
    if (transferResponse.tx_hash) {
      await this.waitForTransactionFinalization(transferResponse.tx_hash);
    }

    // Test different filters
    const allTxs = await this.grpcClient.getTxHistory(account.publicKeyHex, 10, 0, 0);
    const sentTxs = await this.grpcClient.getTxHistory(account.publicKeyHex, 10, 0, 1);
    const receivedTxs = await this.grpcClient.getTxHistory(account.publicKeyHex, 10, 0, 2);

    console.log(`Filter results - All: ${allTxs.total}, Sent: ${sentTxs.total}, Received: ${receivedTxs.total}`);

    if (allTxs.total < sentTxs.total + receivedTxs.total) {
      throw new Error("Filter totals don't add up correctly");
    }
  }

  private async testTransactionHistoryPagination() {
    const account = generateTestAccount();
    const recipient = generateTestAccount();

    // Fund both accounts
    const faucetNonce1 = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const faucetTx1 = buildTx(faucetPublicKeyHex, account.publicKeyHex, 1000, 'Fund for pagination', faucetNonce1, FaucetTxType);
    faucetTx1.signature = signTx(faucetTx1, faucetPrivateKey);
    const response1 = await sendTxViaGrpc(this.grpcClient, faucetTx1);
    if (!response1.ok) throw new Error('First faucet transaction failed');

    const faucetNonce2 = await getNextNonce(this.grpcClient, faucetPublicKeyHex);
    const faucetTx2 = buildTx(faucetPublicKeyHex, recipient.publicKeyHex, 100, 'Fund recipient', faucetNonce2, FaucetTxType);
    faucetTx2.signature = signTx(faucetTx2, faucetPrivateKey);
    const response2 = await sendTxViaGrpc(this.grpcClient, faucetTx2);
    if (!response2.ok) throw new Error('Second faucet transaction failed');

    // Wait for funding transactions to be finalized
    if (response1.tx_hash) {
      await this.waitForTransactionFinalization(response1.tx_hash);
    }
    if (response2.tx_hash) {
      await this.waitForTransactionFinalization(response2.tx_hash);
    }

    // Create multiple transactions
    for (let i = 0; i < 5; i++) {
      const accountNonce = await getNextNonce(this.grpcClient, account.publicKeyHex);
      const tx = buildTx(
        account.publicKeyHex,
        recipient.publicKeyHex,
        10,
        `Pagination tx ${i}`,
        accountNonce,
        TransferTxType
      );
      tx.signature = signTx(tx, account.privateKey);
      const txResponse = await sendTxViaGrpc(this.grpcClient, tx);
      if (!txResponse.ok) throw new Error(`Pagination transaction ${i} failed`);

      // Wait for each transaction to be finalized
      if (txResponse.tx_hash) {
        await this.waitForTransactionFinalization(txResponse.tx_hash);
      }
    }

    // Test pagination
    const page1 = await this.grpcClient.getTxHistory(account.publicKeyHex, 3, 0, 0);
    const page2 = await this.grpcClient.getTxHistory(account.publicKeyHex, 3, 3, 0);

    console.log(`Pagination results - Page 1: ${page1.txs.length}, Page 2: ${page2.txs.length}`);

    if (page1.txs.length + page2.txs.length > 6) {
      throw new Error('Pagination returned too many transactions');
    }
  }

  private printTestSummary() {
    console.log('\n=== TEST SUMMARY ===');
    const totalTests = this.testResults.size;
    const passedTests = Array.from(this.testResults.values()).filter((result) => result).length;
    const failedTests = totalTests - passedTests;

    console.log(`Total Tests: ${totalTests}`);
    console.log(`Passed: ${passedTests}`);
    console.log(`Failed: ${failedTests}`);
    console.log(`Success Rate: ${((passedTests / totalTests) * 100).toFixed(1)}%`);

    if (failedTests > 0) {
      console.log('\nFailed Tests:');
      for (const [testName, result] of this.testResults) {
        if (!result) {
          console.log(`  - ${testName}`);
        }
      }
    }
  }

  setDebug(debug: boolean) {
    this.grpcClient.setDebug(debug);
  }

  close() {
    this.transactionTracker.close();
    this.grpcClient.close();
  }
}

async function main() {
  // Parse command line arguments for debug flag
  const args = process.argv.slice(2);
  const debug = args.includes('--debug') || args.includes('-d');
  
  if (debug) {
    console.log('🔍 Debug mode enabled - showing detailed transaction updates');
  }

  const testSuite = new TestSuite(debug);

  try {
    await testSuite.runAllTests();
  } catch (error) {
    console.error('Test suite failed:', error);
  } finally {
    testSuite.close();
    console.log('\n=== TEST SUITE COMPLETED ===');
  }
}

main().catch(console.error);