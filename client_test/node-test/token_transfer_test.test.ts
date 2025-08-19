import crypto from 'crypto';
import nacl from 'tweetnacl';
import { GrpcClient } from './grpc_client';

// Faucet keypair from genesis configuration
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

// Test account generation with enhanced metadata
function generateTestAccount() {
  const keyPair = nacl.sign.keyPair();
  const publicKeyHex = Buffer.from(keyPair.publicKey).toString('hex');
  const privateKeyDer = Buffer.concat([
    Buffer.from('302e020100300506032b657004220420', 'hex'),
    Buffer.from(keyPair.secretKey.slice(0, 32))
  ]);
  const privateKey = crypto.createPrivateKey({
    key: privateKeyDer,
    format: 'der',
    type: 'pkcs8',
  });
  return { 
    publicKeyHex, 
    privateKey, 
    keyPair,
    seed: Buffer.from(keyPair.secretKey.slice(0, 32)).toString('hex')
  };
}

// Helper function to wait for transaction processing
async function waitForTransaction(ms: number = 2000): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

// Helper function to get account balance with retry logic
async function getAccountBalance(grpcClient: GrpcClient, address: string, retries: number = 3): Promise<number> {
  for (let i = 0; i < retries; i++) {
    try {
      const account = await grpcClient.getAccount(address);
      return parseInt(account.balance);
    } catch (error) {
      if (i === retries - 1) throw error;
      await waitForTransaction(1000);
    }
  }
  return 0;
}

const GRPC_SERVER_ADDRESS = '127.0.0.1:9001';
const TxTypeTransfer = 0;

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
    type,
    sender,
    recipient,
    amount,
    timestamp: Date.now(),
    text_data,
    nonce,
    signature: '',
  };
}

function serializeTx(tx: Tx): Buffer {
  const data = `${tx.type}|${tx.sender}|${tx.recipient}|${tx.amount}|${tx.text_data}|${tx.nonce}`;
  return Buffer.from(data, 'utf8');
}

function signTx(tx: Tx, privateKey: crypto.KeyObject): string {
  const serializedData = serializeTx(tx);
  const signature = crypto.sign(null, serializedData, privateKey);
  return signature.toString('hex');
}

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
  try {
    const response = await grpcClient.addTransaction(txMsg, tx.signature);
    if (!response.ok && response.error) {
      console.error(`gRPC transaction failed: ${response.error}`, {
        tx: txMsg,
        signature: tx.signature
      });
    }
    return response;
  } catch (error) {
    console.error('gRPC call exception:', error, {
      tx: txMsg,
      signature: tx.signature
    });
    return { ok: false, error: error instanceof Error ? error.message : 'Unknown error' };
  }
}

// Enhanced funding function with balance verification
async function fundAccount(grpcClient: GrpcClient, recipientAddress: string, amount: number, nonce: number) {
  const fundTx = buildTx(faucetPublicKeyHex, recipientAddress, amount, 'Funding account', nonce, TxTypeTransfer);
  fundTx.signature = signTx(fundTx, faucetPrivateKey);
  
  const response = await sendTxViaGrpc(grpcClient, fundTx);
  
  // If successful, wait and verify the balance was updated
  if (response.ok) {
    await waitForTransaction(2000);
    try {
      const balance = await getAccountBalance(grpcClient, recipientAddress);
      console.log(`Account ${recipientAddress.substring(0, 8)}... funded with ${amount}, current balance: ${balance}`);
    } catch (error) {
      console.warn('Could not verify balance after funding:', error);
    }
  }
  
  return response;
}

// Helper function to verify transaction history
async function verifyTransactionHistory(
  grpcClient: GrpcClient, 
  address: string, 
  expectedCount: number,
  expectedTxHashes?: string[],
  expectedTransactions?: Array<{sender: string, recipient: string, amount: number}>
): Promise<boolean> {
  try {
    const history = await grpcClient.getTxHistory(address, 20, 0, 0);
    console.log(`Transaction history for ${address.substring(0, 8)}...: ${history.total} transactions`);
    
    // Check transaction count
    if (history.total < expectedCount) {
      console.warn(`Expected at least ${expectedCount} transactions, but found ${history.total}`);
      return false;
    }

    // If transaction hashes are provided, log them for verification
    if (expectedTxHashes && expectedTxHashes.length > 0) {
      console.log(`Expected transaction hashes: ${expectedTxHashes.join(', ')}`);
    }

    // Verify transaction details if provided
    if (expectedTransactions && expectedTransactions.length > 0) {
      console.log('Verifying transaction details:');
      for (let i = 0; i < Math.min(expectedTransactions.length, history.txs.length); i++) {
        const expected = expectedTransactions[i];
        const actual = history.txs[i];
        
        console.log(`Transaction ${i + 1}:`);
        console.log(`  Expected: ${expected.sender.substring(0, 8)}... -> ${expected.recipient.substring(0, 8)}... (${expected.amount})`);
        console.log(`  Actual: ${actual.sender.substring(0, 8)}... -> ${actual.recipient.substring(0, 8)}... (${actual.amount})`);
        console.log(`  Status: ${actual.status}, Nonce: ${actual.nonce}, Timestamp: ${actual.timestamp}`);
        
        // Verify transaction details match
        if (actual.sender !== expected.sender || 
            actual.recipient !== expected.recipient || 
            Math.abs(parseFloat(actual.amount) - expected.amount) > 0.001) {
          console.warn(`Transaction ${i + 1} details don't match expected values`);
          return false;
        }
      }
    }

    return true;
  } catch (error) {
    console.warn('Could not fetch transaction history:', error);
    return false;
  }
}

describe('Token Transfer Tests', () => {
  let grpcClient: GrpcClient;
  
  beforeAll(() => {
    grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS);
  });
  
  afterAll(() => {
    grpcClient.close();
  });
  
  beforeEach(async () => {
    // Wait between tests to avoid conflicts
    await new Promise(resolve => setTimeout(resolve, 1000));
  });

  describe('Success Cases', () => {
    test('Valid Transfer Transaction', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();
      
      // Check faucet account nonce first
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      console.log('Faucet account state:', { address: faucetAccount.address, balance: faucetAccount.balance, nonce: faucetAccount.nonce });
      
      // Fund sender account with correct nonce
      const nextNonce = parseInt(faucetAccount.nonce) + 1;
      console.log('Using nonce:', nextNonce);
      const fundResponse = await fundAccount(grpcClient, sender.publicKeyHex, 1000, nextNonce);
      expect(fundResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Verify sender balance before transfer
      const senderBalanceBefore = await getAccountBalance(grpcClient, sender.publicKeyHex);
      expect(senderBalanceBefore).toBe(1000);
      
      // Perform transfer
      const transferTx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 100, 'Valid transfer', 1, TxTypeTransfer);
      transferTx.signature = signTx(transferTx, sender.privateKey);
      
      const transferResponse = await sendTxViaGrpc(grpcClient, transferTx);
      expect(transferResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Verify balances after transfer
      const senderBalanceAfter = await getAccountBalance(grpcClient, sender.publicKeyHex);
      const recipientBalanceAfter = await getAccountBalance(grpcClient, recipient.publicKeyHex);
      
      expect(senderBalanceAfter).toBe(900);
      expect(recipientBalanceAfter).toBe(100);
      
      // Verify transaction history with detailed verification
      const expectedTransactions = [
        { sender: faucetPublicKeyHex, recipient: sender.publicKeyHex, amount: 1000 }, // funding
        { sender: sender.publicKeyHex, recipient: recipient.publicKeyHex, amount: 100 } // transfer
      ];
      const senderHasHistory = await verifyTransactionHistory(
        grpcClient, 
        sender.publicKeyHex, 
        2, 
        fundResponse.tx_hash && transferResponse.tx_hash ? [fundResponse.tx_hash, transferResponse.tx_hash] : undefined, 
        expectedTransactions
      );
      const recipientHasHistory = await verifyTransactionHistory(
        grpcClient, 
        recipient.publicKeyHex, 
        1, 
        transferResponse.tx_hash ? [transferResponse.tx_hash] : undefined,
        [{ sender: sender.publicKeyHex, recipient: recipient.publicKeyHex, amount: 100 }]
      );
      
      expect(senderHasHistory).toBe(true);
      expect(recipientHasHistory).toBe(true);
    });

    test('Transfer with Text Data', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();
      
      // Check faucet account nonce and fund sender account
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const fundResponse = await fundAccount(grpcClient, sender.publicKeyHex, 1000, parseInt(faucetAccount.nonce) + 1);
      expect(fundResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Perform transfer with text data
      const customMessage = 'Transfer with custom message - blockchain test';
      const transferTx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 50, customMessage, 1, TxTypeTransfer);
      transferTx.signature = signTx(transferTx, sender.privateKey);
      
      const response = await sendTxViaGrpc(grpcClient, transferTx);
      expect(response.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Verify balances
      const senderBalance = await getAccountBalance(grpcClient, sender.publicKeyHex);
      const recipientBalance = await getAccountBalance(grpcClient, recipient.publicKeyHex);
      
      expect(senderBalance).toBe(950);
      expect(recipientBalance).toBe(50);
    });

    test('Transfer Full Balance', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();
      
      // Check faucet account nonce and fund sender account
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const fundResponse = await fundAccount(grpcClient, sender.publicKeyHex, 500, parseInt(faucetAccount.nonce) + 1);
      expect(fundResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Transfer full balance
      const transferTx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 500, 'Full balance transfer', 1, TxTypeTransfer);
      transferTx.signature = signTx(transferTx, sender.privateKey);
      
      const response = await sendTxViaGrpc(grpcClient, transferTx);
      expect(response.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Verify sender has zero balance and recipient has full amount
      const senderBalance = await getAccountBalance(grpcClient, sender.publicKeyHex);
      const recipientBalance = await getAccountBalance(grpcClient, recipient.publicKeyHex);
      
      expect(senderBalance).toBe(0);
      expect(recipientBalance).toBe(500);
    });

    test('Transfer Between Multiple Accounts', async () => {
      const account1 = generateTestAccount();
      const account2 = generateTestAccount();
      const account3 = generateTestAccount();
      
      // Check faucet account nonce and fund first account
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const fundResponse = await fundAccount(grpcClient, account1.publicKeyHex, 1000, parseInt(faucetAccount.nonce) + 1);
      expect(fundResponse.ok).toBe(true);
      
      await new Promise(resolve => setTimeout(resolve, 2000));
      
      // Transfer from account1 to account2
      const transfer1 = buildTx(account1.publicKeyHex, account2.publicKeyHex, 300, 'Chain transfer 1', 1, TxTypeTransfer);
      transfer1.signature = signTx(transfer1, account1.privateKey);
      
      const response1 = await sendTxViaGrpc(grpcClient, transfer1);
      expect(response1.ok).toBe(true);
      
      await new Promise(resolve => setTimeout(resolve, 2000));
      
      // Transfer from account2 to account3
      const transfer2 = buildTx(account2.publicKeyHex, account3.publicKeyHex, 100, 'Chain transfer 2', 1, TxTypeTransfer);
      transfer2.signature = signTx(transfer2, account2.privateKey);
      
      const response2 = await sendTxViaGrpc(grpcClient, transfer2);
      expect(response2.ok).toBe(true);
    });
  });

  describe('Failure Cases', () => {
    test('Transfer with Insufficient Balance', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();
      
      // Check faucet account nonce and fund sender account with small amount
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const fundResponse = await fundAccount(grpcClient, sender.publicKeyHex, 50, parseInt(faucetAccount.nonce) + 1);
      expect(fundResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Verify sender has the expected balance before attempting transfer
      const senderBalance = await getAccountBalance(grpcClient, sender.publicKeyHex);
      expect(senderBalance).toBe(50);
      
      // Try to transfer more than available balance
      const transferTx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 100, 'Insufficient balance test', 1, TxTypeTransfer);
      transferTx.signature = signTx(transferTx, sender.privateKey);
      
      const response = await sendTxViaGrpc(grpcClient, transferTx);
      expect(response.ok).toBe(false);
      expect(response.error).toContain('insufficient balance');
      
      // Verify balances remain unchanged after failed transaction
      const senderBalanceAfter = await getAccountBalance(grpcClient, sender.publicKeyHex);
      const recipientBalanceAfter = await getAccountBalance(grpcClient, recipient.publicKeyHex);
      
      expect(senderBalanceAfter).toBe(50);
      expect(recipientBalanceAfter).toBe(0);
    });

    test('Transfer with Invalid Signature', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();
      const wrongSigner = generateTestAccount();
      
      // Check faucet account nonce and fund sender account
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const fundResponse = await fundAccount(grpcClient, sender.publicKeyHex, 1000, parseInt(faucetAccount.nonce) + 1);
      expect(fundResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Create transaction but sign with wrong private key
      const transferTx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 100, 'Invalid signature test', 1, TxTypeTransfer);
      transferTx.signature = signTx(transferTx, wrongSigner.privateKey); // Wrong signature
      
      const response = await sendTxViaGrpc(grpcClient, transferTx);
      expect(response.ok).toBe(false);
      expect(response.error).toContain('invalid signature');
      
      // Verify balances remain unchanged
      const senderBalance = await getAccountBalance(grpcClient, sender.publicKeyHex);
      const recipientBalance = await getAccountBalance(grpcClient, recipient.publicKeyHex);
      
      expect(senderBalance).toBe(1000);
      expect(recipientBalance).toBe(0);
    });

    test('Transfer with Zero Amount', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();
      
      // Check faucet account nonce and fund sender account
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const fundResponse = await fundAccount(grpcClient, sender.publicKeyHex, 1000, parseInt(faucetAccount.nonce) + 1);
      expect(fundResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Try to transfer zero amount
      const transferTx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 0, 'Zero amount test', 1, TxTypeTransfer);
      transferTx.signature = signTx(transferTx, sender.privateKey);
      
      const response = await sendTxViaGrpc(grpcClient, transferTx);
      expect(response.ok).toBe(false);
      expect(response.error).toContain('zero amount not allowed');
      
      // Verify balances remain unchanged
      const senderBalance = await getAccountBalance(grpcClient, sender.publicKeyHex);
      const recipientBalance = await getAccountBalance(grpcClient, recipient.publicKeyHex);
      
      expect(senderBalance).toBe(1000);
      expect(recipientBalance).toBe(0);
    });

    test('Transfer from Non-existent Account', async () => {
      const nonExistentSender = generateTestAccount();
      const recipient = generateTestAccount();
      
      // Don't fund the sender account
      const transferTx = buildTx(nonExistentSender.publicKeyHex, recipient.publicKeyHex, 100, 'Non-existent sender test', 0, TxTypeTransfer);
      transferTx.signature = signTx(transferTx, nonExistentSender.privateKey);
      
      const response = await sendTxViaGrpc(grpcClient, transferTx);
      expect(response.ok).toBe(false);
    });

    test('Duplicate Transfer Transaction', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();
      
      // Check faucet account nonce and fund sender account
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const fundResponse = await fundAccount(grpcClient, sender.publicKeyHex, 1000, parseInt(faucetAccount.nonce) + 1);
      expect(fundResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Create and send first transaction
      const transferTx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 100, 'Duplicate test', 1, TxTypeTransfer);
      transferTx.signature = signTx(transferTx, sender.privateKey);
      
      const firstResponse = await sendTxViaGrpc(grpcClient, transferTx);
      expect(firstResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Verify first transaction succeeded
      const senderBalanceAfterFirst = await getAccountBalance(grpcClient, sender.publicKeyHex);
      const recipientBalanceAfterFirst = await getAccountBalance(grpcClient, recipient.publicKeyHex);
      
      expect(senderBalanceAfterFirst).toBe(900);
      expect(recipientBalanceAfterFirst).toBe(100);
      
      // Try to send the same transaction again (same nonce)
      const duplicateResponse = await sendTxViaGrpc(grpcClient, transferTx);
      expect(duplicateResponse.ok).toBe(false);
      expect(duplicateResponse.error).toContain('invalid nonce');
      
      // Verify balances remain unchanged after duplicate attempt
      const senderBalanceFinal = await getAccountBalance(grpcClient, sender.publicKeyHex);
      const recipientBalanceFinal = await getAccountBalance(grpcClient, recipient.publicKeyHex);
      
      expect(senderBalanceFinal).toBe(900);
      expect(recipientBalanceFinal).toBe(100);
    });
  });

  describe('Edge Cases', () => {
    test('Multiple Sequential Transfers', async () => {
      const sender = generateTestAccount();
      const recipient1 = generateTestAccount();
      const recipient2 = generateTestAccount();
      const recipient3 = generateTestAccount();
      
      // Check faucet account nonce and fund sender account
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const fundResponse = await fundAccount(grpcClient, sender.publicKeyHex, 1000, parseInt(faucetAccount.nonce) + 1);
      expect(fundResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Perform multiple sequential transfers with correct nonces
      const transfers = [
        { recipient: recipient1.publicKeyHex, amount: 100 },
        { recipient: recipient2.publicKeyHex, amount: 200 },
        { recipient: recipient3.publicKeyHex, amount: 300 }
      ];
      
      for (let i = 0; i < transfers.length; i++) {
        const transfer = transfers[i];
        const senderNonce = i + 1; // Start from nonce 1 for first transfer
        const transferTx = buildTx(sender.publicKeyHex, transfer.recipient, transfer.amount, `Transfer ${senderNonce}`, senderNonce, TxTypeTransfer);
        transferTx.signature = signTx(transferTx, sender.privateKey);
        
        const response = await sendTxViaGrpc(grpcClient, transferTx);
        if (!response.ok) {
          console.log(`Transfer ${i + 1} failed:`, response.error);
        }
        expect(response.ok).toBe(true);
        
        await waitForTransaction(1500);
      }
      
      // Verify final balances
      const senderBalance = await getAccountBalance(grpcClient, sender.publicKeyHex);
      const recipient1Balance = await getAccountBalance(grpcClient, recipient1.publicKeyHex);
      const recipient2Balance = await getAccountBalance(grpcClient, recipient2.publicKeyHex);
      const recipient3Balance = await getAccountBalance(grpcClient, recipient3.publicKeyHex);
      
      expect(senderBalance).toBe(400); // 1000 - 100 - 200 - 300
      expect(recipient1Balance).toBe(100);
      expect(recipient2Balance).toBe(200);
      expect(recipient3Balance).toBe(300);
      
      // Verify transaction history for sender (funding + 3 transfers) with detailed verification
      const expectedSenderTransactions = [
        { sender: faucetPublicKeyHex, recipient: sender.publicKeyHex, amount: 1000 }, // funding
        { sender: sender.publicKeyHex, recipient: recipient1.publicKeyHex, amount: 100 }, // transfer 1
        { sender: sender.publicKeyHex, recipient: recipient2.publicKeyHex, amount: 200 }, // transfer 2
        { sender: sender.publicKeyHex, recipient: recipient3.publicKeyHex, amount: 300 }  // transfer 3
      ];
      const senderHasHistory = await verifyTransactionHistory(
        grpcClient, 
        sender.publicKeyHex, 
        4, 
        undefined, // We don't have the tx_hashes stored in this test
        expectedSenderTransactions
      );
      expect(senderHasHistory).toBe(true);
    });

    test('Transfer to Self', async () => {
      const account = generateTestAccount();
      
      // Check faucet account nonce and fund account
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const fundResponse = await fundAccount(grpcClient, account.publicKeyHex, 1000, parseInt(faucetAccount.nonce) + 1);
      expect(fundResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Verify initial balance
      const initialBalance = await getAccountBalance(grpcClient, account.publicKeyHex);
      expect(initialBalance).toBe(1000);
      
      // Transfer to self
      const transferTx = buildTx(account.publicKeyHex, account.publicKeyHex, 100, 'Self transfer', 1, TxTypeTransfer);
      transferTx.signature = signTx(transferTx, account.privateKey);
      
      const response = await sendTxViaGrpc(grpcClient, transferTx);
      expect(response.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Balance should remain the same (self transfer)
      const finalBalance = await getAccountBalance(grpcClient, account.publicKeyHex);
      expect(finalBalance).toBe(1000);
      
      // Verify transaction history (funding + self transfer) with detailed verification
      const expectedTransactions = [
        { sender: faucetPublicKeyHex, recipient: account.publicKeyHex, amount: 1000 }, // funding
        { sender: account.publicKeyHex, recipient: account.publicKeyHex, amount: 100 }  // self transfer
      ];
      const hasHistory = await verifyTransactionHistory(
        grpcClient, 
        account.publicKeyHex, 
        2, 
        fundResponse.tx_hash && response.tx_hash ? [fundResponse.tx_hash, response.tx_hash] : undefined,
        expectedTransactions
      );
      expect(hasHistory).toBe(true);
    });

    test('Large Amount Transfer', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();
      const largeAmount = 999999;
      
      // Check faucet account nonce and fund sender account with large amount
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const fundResponse = await fundAccount(grpcClient, sender.publicKeyHex, largeAmount, parseInt(faucetAccount.nonce) + 1);
      expect(fundResponse.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Verify sender has the large amount
      const senderBalance = await getAccountBalance(grpcClient, sender.publicKeyHex);
      expect(senderBalance).toBe(largeAmount);
      
      // Transfer large amount
      const transferAmount = 500000;
      const transferTx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, transferAmount, 'Large amount transfer', 1, TxTypeTransfer);
      transferTx.signature = signTx(transferTx, sender.privateKey);
      
      const response = await sendTxViaGrpc(grpcClient, transferTx);
      expect(response.ok).toBe(true);
      
      await waitForTransaction(2000);
      
      // Verify balances after large transfer
      const senderBalanceAfter = await getAccountBalance(grpcClient, sender.publicKeyHex);
      const recipientBalanceAfter = await getAccountBalance(grpcClient, recipient.publicKeyHex);
      
      expect(senderBalanceAfter).toBe(largeAmount - transferAmount);
      expect(recipientBalanceAfter).toBe(transferAmount);
    });
  });
});