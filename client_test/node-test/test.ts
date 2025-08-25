import crypto from 'crypto';
import nacl from 'tweetnacl';
import { GrpcClient } from './grpc_client';

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
  private testResults: Map<string, boolean> = new Map();

  constructor() {
    this.grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS);
  }

  private logTest(name: string, success: boolean, details?: string) {
    this.testResults.set(name, success);
    const status = success ? '✓ PASS' : '✗ FAIL';
    console.log(`${status} ${name}${details ? ` - ${details}` : ''}`);
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

    // Multi-Account Tests
    await this.runTest('Multi-Account Transfer Chain', () => this.testMultiAccountTransferChain());

    // Filtering and Pagination
    await this.runTest('Transaction History Filtering', () => this.testTransactionHistoryFiltering());
    await this.runTest('Transaction History Pagination', () => this.testTransactionHistoryPagination());

    this.printTestSummary();
  }

  private async testBasicFaucetTransaction() {
    const account = generateTestAccount();
    const tx = buildTx(faucetPublicKeyHex, account.publicKeyHex, 100, 'Basic faucet test', 0, FaucetTxType);
    tx.signature = signTx(tx, faucetPrivateKey);

    const response = await sendTxViaGrpc(this.grpcClient, tx);
    if (!response.ok) throw new Error('Faucet transaction failed');
  }

  private async testBasicTransferTransaction() {
    const sender = generateTestAccount();
    const recipient = generateTestAccount();

    // First fund the sender
    const faucetTx = buildTx(faucetPublicKeyHex, sender.publicKeyHex, 200, 'Fund sender', 0, FaucetTxType);
    faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
    await sendTxViaGrpc(this.grpcClient, faucetTx);

    // Then transfer
    const transferTx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 50, 'Basic transfer', 1, TransferTxType);
    transferTx.signature = signTx(transferTx, sender.privateKey);

    const response = await sendTxViaGrpc(this.grpcClient, transferTx);
    if (!response.ok) throw new Error('Transfer transaction failed');
  }

  private async testAccountBalanceVerification() {
    const account = generateTestAccount();

    // Fund account
    const faucetTx = buildTx(faucetPublicKeyHex, account.publicKeyHex, 300, 'Fund for balance test', 0, FaucetTxType);
    faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
    await sendTxViaGrpc(this.grpcClient, faucetTx);

    // Wait a bit for transaction to be processed
    await new Promise((resolve) => setTimeout(resolve, 10000));

    // Verify balance via gRPC
    const accountInfo = await this.grpcClient.getAccount(account.publicKeyHex);
    if (parseInt(accountInfo.balance) !== 300) throw new Error(`Expected balance 300, got ${accountInfo.balance}`);
  }

  private async testTransactionHistory() {
    const account = generateTestAccount();

    // Create multiple transactions
    const faucetTx = buildTx(faucetPublicKeyHex, account.publicKeyHex, 500, 'Fund for history test', 0, FaucetTxType);
    faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
    await sendTxViaGrpc(this.grpcClient, faucetTx);

    const transferTx = buildTx(
      account.publicKeyHex,
      generateTestAccount().publicKeyHex,
      100,
      'History test transfer',
      1,
      TransferTxType
    );
    transferTx.signature = signTx(transferTx, account.privateKey);
    await sendTxViaGrpc(this.grpcClient, transferTx);

    // Wait a bit for transaction to be processed
    await new Promise((resolve) => setTimeout(resolve, 10000));

    // Check history via gRPC
    const history = await this.grpcClient.getTxHistory(account.publicKeyHex, 10, 0, 0);
    if (history.total < 2) throw new Error('Transaction history incomplete');
  }

  private async testSelfTransferTransaction() {
    const account = generateTestAccount();

    // Fund account
    const faucetTx = buildTx(faucetPublicKeyHex, account.publicKeyHex, 100, 'Fund for self transfer', 0, FaucetTxType);
    faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
    await sendTxViaGrpc(this.grpcClient, faucetTx);

    // Self transfer
    const selfTx = buildTx(account.publicKeyHex, account.publicKeyHex, 50, 'Self transfer', 1, TransferTxType);
    selfTx.signature = signTx(selfTx, account.privateKey);

    const response = await sendTxViaGrpc(this.grpcClient, selfTx);
    if (!response.ok) throw new Error('Self transfer should be valid');
  }

  private async testDuplicateTransaction() {
    const sender = generateTestAccount();
    const recipient = generateTestAccount();

    // Fund sender
    const faucetTx = buildTx(faucetPublicKeyHex, sender.publicKeyHex, 100, 'Fund for duplicate test', 0, FaucetTxType);
    faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
    await sendTxViaGrpc(this.grpcClient, faucetTx);

    // Create transaction
    const tx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 10, 'Duplicate test', 1, TransferTxType);
    tx.signature = signTx(tx, sender.privateKey);

    // Send first time
    const response1 = await sendTxViaGrpc(this.grpcClient, tx);
    if (!response1.ok) throw new Error('First transaction should succeed');

    // Send duplicate
    const response2 = await sendTxViaGrpc(this.grpcClient, tx);
    if (response2.ok) throw new Error('Duplicate transaction should have failed');
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

  private async testMultiAccountTransferChain() {
    // Create a chain of transfers: A -> B -> C -> D
    const accounts = generateTestAccounts(4);

    // Fund all accounts first to ensure they exist
    console.log('Funding all accounts for chain transfer test...');
    for (let i = 0; i < accounts.length; i++) {
      const faucetTx = buildTx(faucetPublicKeyHex, accounts[i].publicKeyHex, 100, `Fund account ${i}`, 0, FaucetTxType);
      faucetTx.signature = signTx(faucetTx, faucetPrivateKey);
      await sendTxViaGrpc(this.grpcClient, faucetTx);
    }

    // Fund first account with additional amount for transfers
    const additionalFundTx = buildTx(faucetPublicKeyHex, accounts[0].publicKeyHex, 900, 'Additional fund for chain', 1, FaucetTxType);
    additionalFundTx.signature = signTx(additionalFundTx, faucetPrivateKey);
    await sendTxViaGrpc(this.grpcClient, additionalFundTx);

    // Wait for funding transactions to be processed
    console.log('Waiting for funding transactions to be processed...');
    await new Promise((resolve) => setTimeout(resolve, 5000));

    // Chain transfers
    for (let i = 0; i < accounts.length - 1; i++) {
      const tx = buildTx(
        accounts[i].publicKeyHex,
        accounts[i + 1].publicKeyHex,
        200,
        `Chain transfer ${i}`,
        i + 2, // Start from nonce 2 since we already sent funding transactions
        TransferTxType
      );
      tx.signature = signTx(tx, accounts[i].privateKey);

      const response = await sendTxViaGrpc(this.grpcClient, tx);
      if (!response.ok) throw new Error(`Chain transfer ${i} failed`);
    }

    // Wait for transactions to be processed
    await new Promise((resolve) => setTimeout(resolve, 10000));

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
    const faucetTx1 = buildTx(faucetPublicKeyHex, account.publicKeyHex, 1000, 'Fund for filtering', 0, FaucetTxType);
    faucetTx1.signature = signTx(faucetTx1, faucetPrivateKey);
    await sendTxViaGrpc(this.grpcClient, faucetTx1);

    const faucetTx2 = buildTx(faucetPublicKeyHex, recipient.publicKeyHex, 100, 'Fund recipient', 1, FaucetTxType);
    faucetTx2.signature = signTx(faucetTx2, faucetPrivateKey);
    await sendTxViaGrpc(this.grpcClient, faucetTx2);

    // Wait for funding transactions
    await new Promise((resolve) => setTimeout(resolve, 5000));

    const transferTx = buildTx(
      account.publicKeyHex,
      recipient.publicKeyHex,
      200,
      'Filtering transfer',
      1,
      TransferTxType
    );
    transferTx.signature = signTx(transferTx, account.privateKey);
    await sendTxViaGrpc(this.grpcClient, transferTx);

    // Wait for processing
    await new Promise((resolve) => setTimeout(resolve, 5000));

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
    const faucetTx1 = buildTx(faucetPublicKeyHex, account.publicKeyHex, 1000, 'Fund for pagination', 0, FaucetTxType);
    faucetTx1.signature = signTx(faucetTx1, faucetPrivateKey);
    await sendTxViaGrpc(this.grpcClient, faucetTx1);

    const faucetTx2 = buildTx(faucetPublicKeyHex, recipient.publicKeyHex, 100, 'Fund recipient', 1, FaucetTxType);
    faucetTx2.signature = signTx(faucetTx2, faucetPrivateKey);
    await sendTxViaGrpc(this.grpcClient, faucetTx2);

    // Wait for funding transactions
    await new Promise((resolve) => setTimeout(resolve, 5000));

    // Create multiple transactions
    for (let i = 0; i < 5; i++) {
      const tx = buildTx(
        account.publicKeyHex,
        recipient.publicKeyHex,
        10,
        `Pagination tx ${i}`,
        i + 1,
        TransferTxType
      );
      tx.signature = signTx(tx, account.privateKey);
      await sendTxViaGrpc(this.grpcClient, tx);
    }

    // Wait for processing
    await new Promise((resolve) => setTimeout(resolve, 5000));

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

  close() {
    this.grpcClient.close();
  }
}

async function main() {
  const testSuite = new TestSuite();

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