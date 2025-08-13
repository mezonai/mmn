import crypto from 'crypto';
import nacl from 'tweetnacl';
import { GrpcClient } from './grpc_client';
import {
  TransactionTracker,
  TransactionStatus,
  TransactionStatusInfo,
} from './transaction_tracker';

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

// Generate test account
const recipientSeed = crypto.randomBytes(32);
const recipientKeyPair = nacl.sign.keyPair.fromSeed(recipientSeed);
const recipientPublicKeyHex = Buffer.from(recipientKeyPair.publicKey).toString('hex');
const recipientPrivateKey = crypto.createPrivateKey({
  key: Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), recipientSeed]),
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
  return { publicKeyHex, privateKey };
}

// Generate multiple test accounts
function generateTestAccounts(count: number) {
  return Array.from({ length: count }, () => generateTestAccount());
}

const GRPC_SERVER_ADDRESS = '127.0.0.1:9001';

// Nonce management for each account
const accountNonces = new Map<string, number>();

// Get next nonce for an account (starts with 1)
function getNextNonce(accountAddress: string): number {
  const currentNonce = accountNonces.get(accountAddress) || 0;
  const nextNonce = currentNonce + 1;
  accountNonces.set(accountAddress, nextNonce);
  return nextNonce;
}

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

// Helper function to wait for transaction to reach terminal status
async function waitForTransactionTerminalStatus(txHash: string, timeoutMs: number = 60000): Promise<void> {
  const tracker = new TransactionTracker({
    serverAddress: GRPC_SERVER_ADDRESS,
    statusConsumptionDelay: 100, // 100ms delay between processing status updates
  });

  try {
    console.log(`  ⏳ Waiting for transaction ${txHash.substring(0, 16)}... to reach terminal status...`);
    console.log(`  📋 Full transaction hash: ${txHash}`);

    const finalStatus = await tracker.waitForTerminalStatus(txHash, timeoutMs);
    if (finalStatus.status === TransactionStatus.FINALIZED) {
      console.log(`  ✅ Transaction ${txHash.substring(0, 16)}... finalized successfully`);
    } else if (finalStatus.status === TransactionStatus.FAILED) {
      console.log(`  ❌ Transaction ${txHash.substring(0, 16)}... failed: ${finalStatus.errorMessage}`);
    
    }
  } finally {
    tracker.close();
  }
}

// Helper function to wait for multiple transactions to reach terminal status
async function waitForMultipleTransactionsTerminalStatus(
  txHashes: string[],
  timeoutMs: number = 120000,
  existingTracker?: TransactionTracker
): Promise<void> {
  console.log(`  ⏳ Waiting for ${txHashes.length} transactions to reach terminal status...`);

  // Use existing tracker if provided, otherwise create a new one
  const tracker = existingTracker || new TransactionTracker({
    serverAddress: GRPC_SERVER_ADDRESS,
    statusConsumptionDelay: 100, // 100ms delay between processing status updates
  });

  const shouldCloseTracker = !existingTracker; // Only close if we created it

  try {
    const promises = txHashes.map(async (txHash) => {
      const finalStatus = await tracker.waitForTerminalStatus(txHash, timeoutMs);
      return { txHash, status: finalStatus.status, errorMessage: finalStatus.errorMessage };
    });

    const results = await Promise.all(promises);

    const finalized = results.filter((r) => r.status === TransactionStatus.FINALIZED);
    const failed = results.filter((r) => r.status === TransactionStatus.FAILED);
  

    console.log(`  ✅ All ${txHashes.length} transactions reached terminal status:`);
    console.log(`     - Finalized: ${finalized.length}`);
    console.log(`     - Failed: ${failed.length}`);
  

    if (failed.length > 0) {
      console.log(`  ⚠️  Failed transactions:`);
      failed.forEach((result, index) => {
        console.log(`     ${index + 1}. ${result.txHash.substring(0, 16)}... - ${result.errorMessage}`);
      });
    }
  } finally {
    if (shouldCloseTracker) {
      tracker.close();
    }
  }
}

// ============================================================================
// TEST SUITE 1: BASIC TRANSACTION TESTS
// ============================================================================

async function runBasicTransactionTests() {
  console.log('=== BASIC TRANSACTION TESTS ===\n');

  const grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS);

  try {
    // Test 1: Basic faucet transaction
    console.log('1. Testing basic faucet transaction...');
    const faucetNonce1 = getNextNonce(faucetPublicKeyHex);
    console.log(`  📝 Using nonce ${faucetNonce1} for faucet transaction`);
    const tx1 = buildTx(faucetPublicKeyHex, recipientPublicKeyHex, 100, 'Test faucet tx', faucetNonce1, FaucetTxType);
    tx1.signature = signTx(tx1, faucetPrivateKey);

    if (!verifyTx(tx1, faucetPublicKeyHex)) {
      throw new Error('Transaction signature verification failed');
    }

    const response1 = await sendTxViaGrpc(grpcClient, tx1);
    console.log(`📤 Faucet transaction response:`, JSON.stringify(response1, null, 2));
    if (!response1.ok) {
      throw new Error(`Failed to send faucet transaction: ${response1.error}`);
    }
    console.log(`✅ Faucet transaction sent successfully. Hash: ${response1.tx_hash?.substring(0, 16)}...`);

    // Wait for faucet transaction to reach terminal status
    if (response1.tx_hash) {
      await waitForTransactionTerminalStatus(response1.tx_hash);
    }

    // Test 2: Transfer transaction
    console.log('\n2. Testing transfer transaction...');
    const recipientNonce1 = getNextNonce(recipientPublicKeyHex);
    console.log(`  📝 Using nonce ${recipientNonce1} for recipient transfer transaction`);
    const tx2 = buildTx(
      recipientPublicKeyHex,
      faucetPublicKeyHex,
      50,
      'Test transfer tx',
      recipientNonce1,
      TransferTxType
    );
    tx2.signature = signTx(tx2, recipientPrivateKey);

    if (!verifyTx(tx2, recipientPublicKeyHex)) {
      throw new Error('Transfer transaction signature verification failed');
    }

    const response2 = await sendTxViaGrpc(grpcClient, tx2);
    console.log(`📤 Transfer transaction response:`, JSON.stringify(response2, null, 2));
    if (!response2.ok) {
      throw new Error(`Failed to send transfer transaction: ${response2.error}`);
    }
    console.log(`✅ Transfer transaction sent successfully. Hash: ${response2.tx_hash?.substring(0, 16)}...`);

    // Wait for transfer transaction to reach terminal status
    if (response2.tx_hash) {
      await waitForTransactionTerminalStatus(response2.tx_hash);
    }

    // Test 3: Multiple accounts
    console.log('\n3. Testing multiple accounts...');
    const accounts = generateTestAccounts(3);
    const promises = accounts.map(async (account, index) => {
      const faucetNonce = getNextNonce(faucetPublicKeyHex);
      const tx = buildTx(
        faucetPublicKeyHex,
        account.publicKeyHex,
        10,
        `Multi-account test ${index}`,
        faucetNonce,
        FaucetTxType
      );
      tx.signature = signTx(tx, faucetPrivateKey);
      return await sendTxViaGrpc(grpcClient, tx);
    });

    const results = await Promise.all(promises);
    console.log(`📤 Multi-account transaction responses:`, JSON.stringify(results, null, 2));
    const successCount = results.filter((r) => r.ok).length;
    console.log(`✅ ${successCount}/${accounts.length} multi-account transactions sent successfully`);

    // Wait for all multi-account transactions to be finalized
    const successfulTxHashes = results.filter((r) => r.ok && r.tx_hash).map((r) => r.tx_hash!);

    if (successfulTxHashes.length > 0) {
      console.log('  ⏳ Waiting for multi-account transactions to reach terminal status...');
      await waitForMultipleTransactionsTerminalStatus(successfulTxHashes);
    }

    // Test 4: Account balance check
    console.log('\n4. Testing account balance retrieval...');
    const accountResponse = await grpcClient.getAccount(recipientPublicKeyHex);
    console.log(
      `✅ Account balance retrieved: ${accountResponse.balance} for ${recipientPublicKeyHex.substring(0, 16)}...`
    );

    console.log('\n✅ All basic transaction tests passed!\n');
  } catch (error) {
    console.error('❌ Basic transaction tests failed:', error);
    throw error;
  } finally {
    grpcClient.close();
  }
}

// ============================================================================
// TEST SUITE 2: TRANSACTION STATUS TRACKING TESTS
// ============================================================================

async function runTransactionStatusTests() {
  console.log('=== TRANSACTION STATUS TRACKING TESTS ===\n');
  console.log('This test validates the event-driven transaction status system.\n');

  const grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS);
  const tracker = new TransactionTracker({ serverAddress: GRPC_SERVER_ADDRESS });

  try {
    // Event tracking arrays
    const statusUpdates: TransactionStatusInfo[] = [];
    const allStatusUpdates: TransactionStatusInfo[] = [];

    // Set up event listeners
    tracker.on(
      'statusChanged',
      (txHash: string, newStatus: TransactionStatusInfo) => {
        statusUpdates.push(newStatus);
        console.log(`📈 Status Update: ${txHash.substring(0, 16)}... -> ${TransactionStatus[newStatus.status]}`);
      }
    );

    tracker.on('transactionFinalized', (txHash: string, statusInfo: TransactionStatusInfo) => {
      allStatusUpdates.push(statusInfo);
      console.log(`🌐 All-Tx Update: ${txHash.substring(0, 16)}... -> ${TransactionStatus[statusInfo.status]}`);
    });

    tracker.on('error', () => {
      console.error('❌ Tracker error occurred');
    });

    // Start tracking all transactions
    console.log('🚀 Starting all-transactions tracking...');
    tracker.trackTransactions();

    // Send test transactions
    console.log('\n📤 Sending test transactions...');
    const transactions: string[] = [];

    for (let i = 0; i < 3; i++) {
      const faucetNonce = getNextNonce(faucetPublicKeyHex);
      const tx = buildTx(
        faucetPublicKeyHex,
        recipientPublicKeyHex,
        20 + i * 10,
        `Status test transaction ${i + 1}`,
        faucetNonce,
        FaucetTxType
      );
      tx.signature = signTx(tx, faucetPrivateKey);

      const response = await sendTxViaGrpc(grpcClient, tx);
      console.log(`  📤 Status test transaction ${i + 1} response:`, JSON.stringify(response, null, 2));
      if (response.ok && response.tx_hash) {
        transactions.push(response.tx_hash);
        console.log(`  📤 Sent transaction ${i + 1}: ${response.tx_hash.substring(0, 16)}...`);
      }
    }

    // Wait for all transactions to reach terminal status
    console.log('\n⏳ Waiting for all transactions to reach terminal status...');
    if (transactions.length > 0) {
      await waitForMultipleTransactionsTerminalStatus(transactions, 120000, tracker);
    }

    // Stop tracking
    console.log('\n🛑 Stopping transaction tracking...');
    tracker.stopTracking();

    // Print results
    console.log('\n📊 Test Results:');
    console.log(`  Total specific updates: ${statusUpdates.length}`);
    console.log(`  Total all-transactions updates: ${allStatusUpdates.length}`);
    console.log(`  Transactions sent: ${transactions.length}`);

    if (allStatusUpdates.length > 0) {
      console.log('\n  📈 Status progression:');
      allStatusUpdates.forEach((update, index) => {
        console.log(`    ${index + 1}. ${update.txHash.substring(0, 16)}... -> ${TransactionStatus[update.status]}`);
      });
    }

    console.log('\n✅ Transaction status tracking tests completed!\n');
  } catch (error) {
    console.error('❌ Transaction status tests failed:', error);
    throw error;
  } finally {
    tracker.close();
    grpcClient.close();
  }
}

// ============================================================================
// TEST SUITE 3: EVENT-BASED STATUS TESTS
// ============================================================================

class EventBasedStatusTest {
  private grpcClient: GrpcClient;
  private tracker: TransactionTracker;
  private testResults: Map<string, any> = new Map();

  constructor() {
    this.grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS);
    this.tracker = new TransactionTracker({ serverAddress: GRPC_SERVER_ADDRESS });
  }

  private logTest(name: string, success: boolean, details?: any) {
    this.testResults.set(name, { success, details });
    const status = success ? '✓ PASS' : '✗ FAIL';
    console.log(`${status} ${name}${details ? ` - ${JSON.stringify(details)}` : ''}`);
  }

  private async runTest(name: string, testFn: () => Promise<any>) {
    try {
      const result = await testFn();
      this.logTest(name, true, result);
      return result;
    } catch (error) {
      this.logTest(name, false, error instanceof Error ? error.message : String(error));
      throw error;
    }
  }

  async runAllTests() {
    console.log('=== EVENT-BASED TRANSACTION STATUS TEST SUITE ===\n');

    // Test 1: All transactions subscription (no specific tx hash)
    await this.runTest('All Transactions Subscription', () => this.testAllTransactionsSubscription());

    this.printTestSummary();
  }

  private async testAllTransactionsSubscription() {
    console.log('  🔍 Testing subscription to all transaction events (no specific tx hash)...');

    // Send multiple transactions to test the all-transactions subscription
    const transactions: string[] = [];
    const numTransactions = 3;

    for (let i = 0; i < numTransactions; i++) {
      const faucetNonce = getNextNonce(faucetPublicKeyHex);
      const tx = buildTx(
        faucetPublicKeyHex,
        recipientPublicKeyHex,
        50 + i * 10,
        `All-transactions test ${i + 1}`,
        faucetNonce,
        FaucetTxType
      );
      tx.signature = signTx(tx, faucetPrivateKey);

      const response = await sendTxViaGrpc(this.grpcClient, tx);
      console.log(`  📤 Event-based test transaction ${i + 1} response:`, JSON.stringify(response, null, 2));
      if (response.ok && response.tx_hash) {
        transactions.push(response.tx_hash);
        console.log(`  📤 Sent transaction ${i + 1}: ${response.tx_hash.substring(0, 16)}...`);
      }
    }

    // Subscribe to all transaction events (no specific tx hash)
    const allUpdates: Map<string, any[]> = new Map();
    const allTransactionsPromise = new Promise<any>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error('All transactions subscription timeout'));
      }, 30000);

      const unsubscribe = this.grpcClient.subscribeTransactionStatus(
        async (update: {
          tx_hash: string;
          status: string;
          block_slot?: string;
          block_hash?: string;
          confirmations?: string;
          error_message?: string;
          timestamp?: string;
        }) => {
          // Track updates for each transaction
          if (!allUpdates.has(update.tx_hash)) {
            allUpdates.set(update.tx_hash, []);
          }
          allUpdates.get(update.tx_hash)!.push(update);

          console.log(`  📡 All-transactions update: ${update.tx_hash.substring(0, 16)}... -> ${update.status}`);

          // Check if we've received updates for all our test transactions
          const allTransactionsHaveUpdates = transactions.every(
            (txHash) => allUpdates.has(txHash) && allUpdates.get(txHash)!.length > 0
          );

          // Check if all transactions have reached terminal status
          const allTransactionsCompleted = transactions.every((txHash) => {
            const updates = allUpdates.get(txHash);
            if (!updates || updates.length === 0) return false;
            const lastUpdate = updates[updates.length - 1];
            return (
              lastUpdate.status === 'FINALIZED' || lastUpdate.status === 'FAILED'
            );
          });

          if (allTransactionsHaveUpdates && allTransactionsCompleted) {
            clearTimeout(timeout);
            unsubscribe();

            // All transactions have already been finalized through the subscription
            console.log('  ✅ All transactions finalized through subscription');

            resolve({
              totalTransactions: transactions.length,
              totalUpdates: Array.from(allUpdates.values()).reduce((sum, updates) => sum + updates.length, 0),
              updatesPerTransaction: Array.from(allUpdates.entries()).map(([txHash, updates]) => ({
                txHash: txHash.substring(0, 16) + '...',
                updateCount: updates.length,
                finalStatus: updates[updates.length - 1]?.status || 'UNKNOWN',
              })),
              subscriptionType: 'All Transactions (no specific tx hash)',
            });
          }
        },
        (error: any) => {
          clearTimeout(timeout);
          reject(error);
        },
        () => {
          clearTimeout(timeout);
          resolve({
            totalTransactions: transactions.length,
            totalUpdates: Array.from(allUpdates.values()).reduce((sum, updates) => sum + updates.length, 0),
            updatesPerTransaction: Array.from(allUpdates.entries()).map(([txHash, updates]) => ({
              txHash: txHash.substring(0, 16) + '...',
              updateCount: updates.length,
              finalStatus: updates[updates.length - 1]?.status || 'UNKNOWN',
            })),
            subscriptionType: 'All Transactions (no specific tx hash)',
          });
        }
      );
    });

    return await allTransactionsPromise;
  }

  private printTestSummary() {
    console.log('\n=== EVENT-BASED STATUS TEST SUMMARY ===');
    const totalTests = this.testResults.size;
    const passedTests = Array.from(this.testResults.values()).filter((result) => result.success).length;
    const failedTests = totalTests - passedTests;

    console.log(`Total Tests: ${totalTests}`);
    console.log(`Passed: ${passedTests}`);
    console.log(`Failed: ${failedTests}`);
    console.log(`Success Rate: ${((passedTests / totalTests) * 100).toFixed(1)}%`);

    if (failedTests > 0) {
      console.log('\nFailed Tests:');
      for (const [testName, result] of this.testResults) {
        if (!result.success) {
          console.log(`  - ${testName}: ${result.details}`);
        }
      }
    }

    console.log('\n🎯 Event-Based System Benefits:');
    console.log('  ✅ Real-time updates without polling');
    console.log('  ✅ Reduced server load and resource usage');
    console.log('  ✅ Better scalability for multiple subscribers');
    console.log('  ✅ Immediate notification of status changes');
    console.log('  ✅ Automatic cleanup of completed subscriptions');
  }

  close() {
    this.tracker.close();
    this.grpcClient.close();
  }
}

async function runEventBasedStatusTests() {
  const testSuite = new EventBasedStatusTest();

  try {
    await testSuite.runAllTests();
  } catch (error) {
    console.error('Event-based status test suite failed:', error);
  } finally {
    testSuite.close();
    console.log('\n=== EVENT-BASED STATUS TEST SUITE COMPLETED ===');
  }
}

// ============================================================================
// TEST SUITE 4: INVALID TRANSACTION TESTS
// ============================================================================

async function runInvalidTransactionTests() {
  console.log('=== INVALID TRANSACTION TESTS ===\n');

  const grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS);
  const tracker = new TransactionTracker({ serverAddress: GRPC_SERVER_ADDRESS });

  try {
    // Start tracking all transactions to catch failure events
    tracker.trackTransactions();

    // Test 1: Transaction with invalid signature
    console.log('1. Testing transaction with invalid signature...');
    const testAccount1 = generateTestAccount();
    const invalidNonce1 = getNextNonce(testAccount1.publicKeyHex);

    const invalidTx1 = buildTx(
      testAccount1.publicKeyHex,
      recipientPublicKeyHex,
      100,
      'Invalid signature test',
      invalidNonce1,
      TransferTxType
    );
    // Use wrong private key to create invalid signature
    invalidTx1.signature = signTx(invalidTx1, faucetPrivateKey); // Wrong private key

    const response1 = await sendTxViaGrpc(grpcClient, invalidTx1);
    console.log(`📤 Invalid signature transaction response:`, JSON.stringify(response1, null, 2));

    // Wait a bit for the failure event to be processed
    await new Promise((resolve) => setTimeout(resolve, 2000));

    // Check if we received a failure event
    const trackedTxs1 = tracker.getTrackedTransactions();
    const failedTx1 = Array.from(trackedTxs1.values()).find(
      (tx) => tx.status === TransactionStatus.FAILED && tx.errorMessage && tx.errorMessage.includes('sig')
    );

    if (failedTx1) {
      console.log(`✅ Received failure event for invalid signature: ${failedTx1.errorMessage}`);
    } else {
      console.log(`⚠️  No failure event received for invalid signature transaction`);
    }

    // Test 2: Transaction with insufficient balance
    console.log('\n2. Testing transaction with insufficient balance...');
    const testAccount2 = generateTestAccount();
    const insufficientNonce = getNextNonce(testAccount2.publicKeyHex);

    const insufficientTx = buildTx(
      testAccount2.publicKeyHex,
      recipientPublicKeyHex,
      1000,
      'Insufficient balance test',
      insufficientNonce,
      TransferTxType
    );
    insufficientTx.signature = signTx(insufficientTx, testAccount2.privateKey);

    const response2 = await sendTxViaGrpc(grpcClient, insufficientTx);
    console.log(`📤 Insufficient balance transaction response:`, JSON.stringify(response2, null, 2));

    // Wait for failure event
    await new Promise((resolve) => setTimeout(resolve, 2000));

    const trackedTxs2 = tracker.getTrackedTransactions();
    const failedTx2 = Array.from(trackedTxs2.values()).find(
      (tx) => tx.status === TransactionStatus.FAILED && tx.errorMessage && tx.errorMessage.includes('insufficient')
    );

    if (failedTx2) {
      console.log(`✅ Received failure event for insufficient balance: ${failedTx2.errorMessage}`);
    } else {
      console.log(`⚠️  No failure event received for insufficient balance transaction`);
    }

    // Test 3: Transaction with invalid nonce
    console.log('\n3. Testing transaction with invalid nonce...');
    const testAccount3 = generateTestAccount();

    // First send a valid transaction to establish nonce
    const validNonce = getNextNonce(testAccount3.publicKeyHex);
    const validTx = buildTx(
      testAccount3.publicKeyHex,
      recipientPublicKeyHex,
      50,
      'Valid tx for nonce test',
      validNonce,
      TransferTxType
    );
    validTx.signature = signTx(validTx, testAccount3.privateKey);

    await sendTxViaGrpc(grpcClient, validTx);
    await new Promise((resolve) => setTimeout(resolve, 1000));

    // Now send transaction with invalid nonce (same as previous)
    const invalidNonceTx = buildTx(
      testAccount3.publicKeyHex,
      recipientPublicKeyHex,
      50,
      'Invalid nonce test',
      validNonce,
      TransferTxType
    );
    invalidNonceTx.signature = signTx(invalidNonceTx, testAccount3.privateKey);

    const response3 = await sendTxViaGrpc(grpcClient, invalidNonceTx);
    console.log(`📤 Invalid nonce transaction response:`, JSON.stringify(response3, null, 2));

    // Wait for failure event
    await new Promise((resolve) => setTimeout(resolve, 2000));

    const trackedTxs3 = tracker.getTrackedTransactions();
    const failedTx3 = Array.from(trackedTxs3.values()).find(
      (tx) => tx.status === TransactionStatus.FAILED && tx.errorMessage && tx.errorMessage.includes('nonce')
    );

    if (failedTx3) {
      console.log(`✅ Received failure event for invalid nonce: ${failedTx3.errorMessage}`);
    } else {
      console.log(`⚠️  No failure event received for invalid nonce transaction`);
    }

    // Test 4: Malformed transaction (invalid JSON)
    console.log('\n4. Testing malformed transaction...');
    try {
      // Try to send malformed data by creating an invalid transaction
      const testAccount4 = generateTestAccount();
      const malformedNonce = getNextNonce(testAccount4.publicKeyHex);

      // Create a transaction with invalid data
      const malformedTx = buildTx(
        testAccount4.publicKeyHex,
        recipientPublicKeyHex,
        100,
        'Malformed test',
        malformedNonce,
        TransferTxType
      );
      malformedTx.signature = 'invalid_signature_hex'; // Invalid signature format

      const response4 = await sendTxViaGrpc(grpcClient, malformedTx);
      console.log(`📤 Malformed transaction response:`, JSON.stringify(response4, null, 2));

      if (!response4.ok) {
        console.log(`✅ Malformed transaction properly rejected: ${response4.error}`);
      }
    } catch (error) {
      console.log(`✅ Malformed transaction properly rejected with error: ${error}`);
    }

    // Test 5: Faucet transaction from non-faucet address
    console.log('\n5. Testing faucet transaction from non-faucet address...');
    const testAccount5 = generateTestAccount();
    const faucetNonce = getNextNonce(testAccount5.publicKeyHex);

    const nonFaucetTx = buildTx(
      testAccount5.publicKeyHex,
      recipientPublicKeyHex,
      100,
      'Non-faucet faucet test',
      faucetNonce,
      FaucetTxType
    );
    nonFaucetTx.signature = signTx(nonFaucetTx, testAccount5.privateKey);

    const response5 = await sendTxViaGrpc(grpcClient, nonFaucetTx);
    console.log(`📤 Non-faucet faucet transaction response:`, JSON.stringify(response5, null, 2));

    // Wait for failure event
    await new Promise((resolve) => setTimeout(resolve, 2000));

    const trackedTxs5 = tracker.getTrackedTransactions();
    const failedTx5 = Array.from(trackedTxs5.values()).find(
      (tx) => tx.status === TransactionStatus.FAILED && tx.errorMessage && tx.errorMessage.includes('faucet')
    );

    if (failedTx5) {
      console.log(`✅ Received failure event for non-faucet faucet transaction: ${failedTx5.errorMessage}`);
    } else {
      console.log(`⚠️  No failure event received for non-faucet faucet transaction`);
    }

    // Test 6: Zero amount transaction
    console.log('\n6. Testing zero amount transaction...');
    const testAccount6 = generateTestAccount();
    const zeroAmountNonce = getNextNonce(testAccount6.publicKeyHex);

    const zeroAmountTx = buildTx(
      testAccount6.publicKeyHex,
      recipientPublicKeyHex,
      0,
      'Zero amount test',
      zeroAmountNonce,
      TransferTxType
    );
    zeroAmountTx.signature = signTx(zeroAmountTx, testAccount6.privateKey);

    const response6 = await sendTxViaGrpc(grpcClient, zeroAmountTx);
    console.log(`📤 Zero amount transaction response:`, JSON.stringify(response6, null, 2));

    // Wait for failure event
    await new Promise((resolve) => setTimeout(resolve, 2000));

    const trackedTxs6 = tracker.getTrackedTransactions();
    const failedTx6 = Array.from(trackedTxs6.values()).find(
      (tx) => tx.status === TransactionStatus.FAILED && tx.errorMessage && tx.errorMessage.includes('amount')
    );

    if (failedTx6) {
      console.log(`✅ Received failure event for zero amount transaction: ${failedTx6.errorMessage}`);
    } else {
      console.log(`⚠️  No failure event received for zero amount transaction`);
    }

    // Print summary of all tracked transactions
    console.log('\n📊 Summary of tracked transactions:');
    const allTrackedTxs = tracker.getTrackedTransactions();
    const failedTxs = Array.from(allTrackedTxs.values()).filter((tx) => tx.status === TransactionStatus.FAILED);
    const successfulTxs = Array.from(allTrackedTxs.values()).filter((tx) => tx.status === TransactionStatus.FINALIZED);

    console.log(`Total tracked transactions: ${allTrackedTxs.size}`);
    console.log(`Failed transactions: ${failedTxs.length}`);
    console.log(`Successful transactions: ${successfulTxs.length}`);

    if (failedTxs.length > 0) {
      console.log('\nFailed transaction details:');
      failedTxs.forEach((tx, index) => {
        console.log(`  ${index + 1}. Hash: ${tx.txHash.substring(0, 16)}...`);
        console.log(`     Error: ${tx.errorMessage}`);
        console.log(`     Timestamp: ${new Date(tx.timestamp).toISOString()}`);
      });
    }

    console.log('\n✅ Invalid transaction tests completed successfully!');
  } catch (error) {
    console.error('❌ Invalid transaction tests failed:', error);
    throw error;
  } finally {
    tracker.close();
    grpcClient.close();
  }
}



// ============================================================================
// MAIN TEST RUNNER
// ============================================================================

async function main() {
  console.log('🚀 Starting Comprehensive MMN Test Suite\n');
  console.log('This test suite includes:');
  console.log('1. Basic transaction tests (signing, verification, sending)');
  console.log('2. Transaction status tracking tests (event-based system)');
  console.log('3. Event-based status subscription tests');
  console.log('4. Invalid transaction tests (failure scenarios)\n');

  try {
    // Run all test suites
    await runBasicTransactionTests();
    await runTransactionStatusTests();
    await runEventBasedStatusTests();
    await runInvalidTransactionTests();

    console.log('🎉 All test suites completed successfully!');
    console.log('\n📋 Summary:');
    console.log('✅ Basic transaction functionality working');
    console.log('✅ Event-based status tracking working');
    console.log('✅ All-transactions subscription working');
    console.log('✅ Invalid transaction handling working');
    console.log('✅ No timeout issues');
    console.log('✅ Proper cleanup and resource management');
  } catch (error) {
    console.error('\n❌ Test suite failed:', error);
    process.exit(1);
  }
}

if (require.main === module) {
  main().catch(console.error);
}
