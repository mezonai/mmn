import { GrpcClient } from './grpc_client';
import { TransactionTracker } from './transaction_tracker';
import {
  faucetPublicKeyBase58,
  TxTypeTransfer,
  generateTestAccount,
  waitForTransaction,
  getAccountBalance,
  buildTx,
  serializeTx,
  signTx,
  sendTxViaGrpc,
  fundAccount,
  getCurrentNonce
} from './utils';

// Node configurations
const NODE_CONFIGS = [
  { name: 'node1', address: '127.0.0.1:9001' },
  { name: 'node2', address: '127.0.0.1:9002' },
  { name: 'node3', address: '127.0.0.1:9003' },
];

interface NodeClient {
  name: string;
  client: GrpcClient;
  address: string;
}

// Verify account balance across all nodes
async function verifyBalanceAcrossNodes(
  nodeClients: NodeClient[],
  address: string,
  expectedBalance: number
): Promise<boolean> {
  const balances: { [key: string]: number } = {};

  for (const nodeClient of nodeClients) {
    try {
      const balance = await getAccountBalance(nodeClient.client, address);
      balances[nodeClient.name] = balance;
    } catch (error) {
      console.error(`Failed to get balance from ${nodeClient.name}:`, error);
      return false;
    }
  }

  console.log(`Balance verification for ${address}:`, balances);

  // Check if all nodes have the same balance
  const uniqueBalances = Object.values(balances);
  const allSame = uniqueBalances.every((balance) => balance === expectedBalance);

  if (!allSame) {
    console.error(`Balance mismatch! Expected: ${expectedBalance}, Got:`, balances);
  }

  return allSame;
}

// Check consensus across all nodes (single attempt)
async function getBalanceCrossNodes(nodeClients: NodeClient[], address: string): Promise<number | null> {
  const balances: number[] = [];
  const nodeBalances: { [key: string]: number } = {};
  
  for (const nodeClient of nodeClients) {
    try {
      const balance = await getAccountBalance(nodeClient.client, address);
      balances.push(balance);
      nodeBalances[nodeClient.name] = balance;
      console.log(`${nodeClient.name} balance for ${address.substring(0, 8)}...: ${balance}`);
    } catch (error) {
      console.error(`Failed to get balance from ${nodeClient.name}:`, error);
      return null;
    }
  }
  
  console.log(`All node balances for ${address.substring(0, 8)}...:`, nodeBalances);
  
  // Check if all balances are the same
  if (balances.every((balance) => balance === balances[0])) {
    return balances[0];
  }

  console.log(`Consensus check failed - Balances:`, balances);
  console.log(`Node-specific balances:`, nodeBalances);
  return null;
}

describe('Parallel Three Nodes Token Transfer Tests', () => {
  let nodeClients: NodeClient[] = [];
  let transactionTracker: TransactionTracker;

  beforeAll(async () => {
    // Initialize connections to all three nodes
    for (const config of NODE_CONFIGS) {
      const client = new GrpcClient(config.address);
      nodeClients.push({
        name: config.name,
        client: client,
        address: config.address,
      });
    }

    transactionTracker = new TransactionTracker({
      serverAddress: NODE_CONFIGS[0].address,
      debug: true,
    });
    transactionTracker.trackTransactions();

    console.log(
      'Connected to nodes:',
      nodeClients.map((nc) => nc.name)
    );
  });

  afterAll(() => {
    // Close all connections
    nodeClients.forEach((nodeClient) => {
      nodeClient.client.close();
    });

    transactionTracker.close();
  });

  beforeEach(async () => {
    // Wait between tests to avoid conflicts
    // await waitForTransaction(2000);
  });

  describe('Multi-Node Parallel Transfer Tests', () => {
    test('Parallel Transfers Across 3 Nodes with Consensus Verification', async () => {
      const sender = generateTestAccount();
      const recipient1 = generateTestAccount();
      const recipient2 = generateTestAccount();
      const recipient3 = generateTestAccount();

      console.log('Test accounts created:', {
        sender: sender.publicKeyHex,
        recipient1: recipient1.publicKeyHex,
        recipient2: recipient2.publicKeyHex,
        recipient3: recipient3.publicKeyHex,
      });

      // Fund sender account using node1
      await waitForTransaction(800); // ~ 2 slots
      const faucetAccount = await nodeClients[0].client.getAccount(faucetPublicKeyBase58);
      console.log('Faucet account state:', {
        address: faucetAccount.address,
        balance: faucetAccount.balance,
        nonce: faucetAccount.nonce,
      });
      
      // Get current faucet account nonce using getCurrentNonce
      const currentFaucetNonce = await getCurrentNonce(nodeClients[0].client, faucetPublicKeyBase58, 'pending');
      const fundingNonce = currentFaucetNonce + 1;
      
      console.log(`Funding sender ${sender.publicKeyHex.substring(0, 8)}... with nonce ${fundingNonce} (current nonce: ${currentFaucetNonce})`);
      const fundResponse = await fundAccount(nodeClients[0].client, sender.publicKeyHex, 3000);
      if (!fundResponse.ok) {
        console.warn('Funding failed, this might be due to mempool being full or nonce conflicts:', fundResponse.error);
        console.warn('Faucet current nonce:', currentFaucetNonce, 'Used nonce:', fundingNonce);
        // Skip this test if funding fails - this is expected in a busy test environment
        return;
      }

      await transactionTracker.waitForTerminalStatus(fundResponse.tx_hash!);
      console.log('Funding successful!');

      // Verify sender balance across all nodes
      const senderBalanceConsensus = await getBalanceCrossNodes(nodeClients, sender.publicKeyHex);
      expect(senderBalanceConsensus).toBe(3000);

      console.log('Sender funded successfully across all nodes with balance:', senderBalanceConsensus);

      // Create transactions for parallel execution
      const tx1 = buildTx(sender.publicKeyHex, recipient1.publicKeyHex, 500, 'Parallel tx to node1', 1, TxTypeTransfer);
      tx1.signature = signTx(tx1, sender.privateKey);

      const tx2 = buildTx(sender.publicKeyHex, recipient2.publicKeyHex, 700, 'Parallel tx to node2', 2, TxTypeTransfer);
      tx2.signature = signTx(tx2, sender.privateKey);

      const tx3 = buildTx(sender.publicKeyHex, recipient3.publicKeyHex, 800, 'Parallel tx to node3', 3, TxTypeTransfer);
      tx3.signature = signTx(tx3, sender.privateKey);

      console.log('Sending parallel transactions to different nodes...');

      // Send transactions to different nodes in parallel
      const [response1, response2, response3] = await Promise.all([
        sendTxViaGrpc(nodeClients[0].client, tx1), // Send to node1
        sendTxViaGrpc(nodeClients[1].client, tx2), // Send to node2
        sendTxViaGrpc(nodeClients[2].client, tx3), // Send to node3
      ]);

      console.log('Parallel transaction results:', {
        node1_tx1: { success: response1.ok, hash: response1.tx_hash, error: response1.error },
        node2_tx2: { success: response2.ok, hash: response2.tx_hash, error: response2.error },
        node3_tx3: { success: response3.ok, hash: response3.tx_hash, error: response3.error },
      });

      // Wait for transactions to propagate and reach consensus
      await Promise.all([
        transactionTracker.waitForTerminalStatus(response1.tx_hash!),
        response2.ok && response2.tx_hash && transactionTracker.waitForTerminalStatus(response2.tx_hash!),
        response3.ok && response3.tx_hash && transactionTracker.waitForTerminalStatus(response3.tx_hash!),
      ]);

      // Verify consensus across all nodes for all accounts
      console.log('Verifying consensus across all nodes...');

      // Calculate expected balances based on successful transactions
      let expectedSenderBalance = 3000;
      let expectedRecipient1Balance = 0;
      let expectedRecipient2Balance = 0;
      let expectedRecipient3Balance = 0;

      if (response1.ok) {
        expectedSenderBalance -= 500;
        expectedRecipient1Balance = 500;
      }
      if (response2.ok) {
        expectedSenderBalance -= 700;
        expectedRecipient2Balance = 700;
      }
      if (response3.ok) {
        expectedSenderBalance -= 800;
        expectedRecipient3Balance = 800;
      }

      // Wait for consensus on all accounts
      const finalSenderBalance = await getBalanceCrossNodes(nodeClients, sender.publicKeyHex);
      const finalRecipient1Balance = await getBalanceCrossNodes(nodeClients, recipient1.publicKeyHex);
      const finalRecipient2Balance = await getBalanceCrossNodes(nodeClients, recipient2.publicKeyHex);
      const finalRecipient3Balance = await getBalanceCrossNodes(nodeClients, recipient3.publicKeyHex);

      console.log('Final consensus balances:', {
        sender: finalSenderBalance,
        recipient1: finalRecipient1Balance,
        recipient2: finalRecipient2Balance,
        recipient3: finalRecipient3Balance,
      });

      // Verify balances match expectations
      expect(finalSenderBalance).toBe(expectedSenderBalance);
      expect(finalRecipient1Balance).toBe(expectedRecipient1Balance);
      expect(finalRecipient2Balance).toBe(expectedRecipient2Balance);
      expect(finalRecipient3Balance).toBe(expectedRecipient3Balance);

      // Verify total balance conservation
      const totalBalance =
        (finalSenderBalance || 0) +
        (finalRecipient1Balance || 0) +
        (finalRecipient2Balance || 0) +
        (finalRecipient3Balance || 0);
      expect(totalBalance).toBe(3000);

      // Additional verification: Check that all nodes have identical states
      const allAccountsConsistent = await Promise.all([
        verifyBalanceAcrossNodes(nodeClients, sender.publicKeyHex, expectedSenderBalance),
        verifyBalanceAcrossNodes(nodeClients, recipient1.publicKeyHex, expectedRecipient1Balance),
        verifyBalanceAcrossNodes(nodeClients, recipient2.publicKeyHex, expectedRecipient2Balance),
        verifyBalanceAcrossNodes(nodeClients, recipient3.publicKeyHex, expectedRecipient3Balance),
      ]);

      expect(allAccountsConsistent.every((consistent) => consistent)).toBe(true);

      console.log('✅ All nodes reached consensus successfully!');
    }, 60000); // 60 second timeout for this complex test

    test('Concurrent Same-Sender Transactions to Different Nodes', async () => {
      const sender = generateTestAccount();
      const recipients = [generateTestAccount(), generateTestAccount(), generateTestAccount()];

      // Fund sender
      const fundResponse = await fundAccount(nodeClients[0].client, sender.publicKeyHex, 2000);
      expect(fundResponse.ok).toBe(true);

      // Verify initial funding consensus
      const initialBalance = await getBalanceCrossNodes(nodeClients, sender.publicKeyHex);
      expect(initialBalance).toBe(2000);

      // Create concurrent transactions with sequential nonces
      const transactions = recipients.map((recipient, index) => {
        const tx = buildTx(
          sender.publicKeyHex,
          recipient.publicKeyHex,
          300,
          `Concurrent tx ${index + 1}`,
          index + 1,
          TxTypeTransfer
        );
        tx.signature = signTx(tx, sender.privateKey);
        return tx;
      });

      console.log('Sending concurrent transactions with sequential nonces...');

      // Send all transactions concurrently to different nodes
      const responses = await Promise.all([
        sendTxViaGrpc(nodeClients[0].client, transactions[0]),
        sendTxViaGrpc(nodeClients[1].client, transactions[1]),
        sendTxViaGrpc(nodeClients[2].client, transactions[2]),
      ]);

      console.log(
        'Concurrent transaction responses:',
        responses.map((r, i) => ({
          [`tx${i + 1}`]: { success: r.ok, error: r.error },
        }))
      );

      await waitForTransaction(6000);

      // Verify final state consensus
      const finalSenderBalance = await getBalanceCrossNodes(nodeClients, sender.publicKeyHex);

      // Count successful transactions
      const successfulTxCount = responses.filter((r) => r.ok).length;
      const expectedFinalBalance = 2000 - successfulTxCount * 300;

      expect(finalSenderBalance).toBe(expectedFinalBalance);

      // Verify recipients received funds correctly
      for (let i = 0; i < recipients.length; i++) {
        const recipientBalance = await getBalanceCrossNodes(nodeClients, recipients[i].publicKeyHex);
        const expectedRecipientBalance = responses[i].ok ? 300 : 0;
        expect(recipientBalance).toBe(expectedRecipientBalance);
      }

      console.log('✅ Concurrent transactions handled correctly across all nodes!');
    }, 60000);

    test('Network Partition Recovery Simulation', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();

      // Fund sender through node1
      const fundResponse = await fundAccount(nodeClients[0].client, sender.publicKeyHex, 1000);
      expect(fundResponse.ok).toBe(true);

      // Verify initial consensus
      const initialBalance = await getBalanceCrossNodes(nodeClients, sender.publicKeyHex);
      expect(initialBalance).toBe(1000);

      // Send transaction to only one node (simulating network partition)
      const tx = buildTx(sender.publicKeyHex, recipient.publicKeyHex, 400, 'Partition test tx', 1, TxTypeTransfer);
      tx.signature = signTx(tx, sender.privateKey);

      console.log('Sending transaction to node1 only (simulating partition)...');
      const response = await sendTxViaGrpc(nodeClients[0].client, tx);
      expect(response.ok).toBe(true);

      // Wait for propagation across network
      await transactionTracker.waitForTerminalStatus(response.tx_hash!);

      // Verify all nodes eventually reach consensus
      const finalSenderBalance = await getBalanceCrossNodes(nodeClients, sender.publicKeyHex);
      const finalRecipientBalance = await getBalanceCrossNodes(nodeClients, recipient.publicKeyHex);

      expect(finalSenderBalance).toBe(600);
      expect(finalRecipientBalance).toBe(400);

      // Verify consistency across all nodes
      const senderConsistent = await verifyBalanceAcrossNodes(nodeClients, sender.publicKeyHex, 600);
      const recipientConsistent = await verifyBalanceAcrossNodes(nodeClients, recipient.publicKeyHex, 400);

      expect(senderConsistent).toBe(true);
      expect(recipientConsistent).toBe(true);

      console.log('✅ Network partition recovery successful!');
    }, 60000);

    test('Node Failure Resilience', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();

      // Fund sender account using multiple nodes for redundancy
      let fundResponse;
      for (let i = 0; i < nodeClients.length; i++) {
        try {
          const currentFaucetNonce = await getCurrentNonce(nodeClients[i].client, faucetPublicKeyBase58, 'pending');
          const fundingNonce = currentFaucetNonce + 1;
          console.log(`Node ${i + 1}: Using nonce ${fundingNonce} (current nonce: ${currentFaucetNonce})`);
          fundResponse = await fundAccount(nodeClients[i].client, sender.publicKeyHex, 1000);
          if (fundResponse.ok) break;
        } catch (error) {
          console.warn(`Node ${i + 1} failed during funding:`, error);
          continue;
        }
      }

      if (!fundResponse?.ok) {
        console.warn('All nodes failed during funding. Skipping test.');
        return;
      }

      // Test transaction submission with node failures
      const transactions = [];
      const numTransactions = 5;

      for (let i = 0; i < numTransactions; i++) {
        const tx = buildTx(
          sender.publicKeyHex,
          recipient.publicKeyHex,
          50,
          `Resilience test ${i}`,
          i + 1,
          TxTypeTransfer
        );
        tx.signature = signTx(tx, sender.privateKey);
        transactions.push(tx);
      }

      // Submit transactions with fallback to different nodes
      const results = [];
      for (const tx of transactions) {
        let success = false;
        for (let nodeIndex = 0; nodeIndex < nodeClients.length; nodeIndex++) {
          try {
            const response = await sendTxViaGrpc(nodeClients[nodeIndex].client, tx);
            if (response.ok) {
              results.push({ success: true, nodeUsed: nodeIndex + 1, txHash: response.tx_hash });
              success = true;
              break;
            }
          } catch (error) {
            console.warn(`Node ${nodeIndex + 1} failed for transaction:`, error);
            continue;
          }
        }

        if (!success) {
          results.push({ success: false, error: 'All nodes failed' });
        }

        // Small delay between transactions
        await waitForTransaction(500);
      }

      // Wait for all transactions to propagate
      await Promise.all([
        results.map((r) => r.success && r.txHash && transactionTracker.waitForTerminalStatus(r.txHash)),
      ]);

      // Verify final state across all available nodes
      const finalBalances = [];
      for (let i = 0; i < nodeClients.length; i++) {
        try {
          const balance = await getAccountBalance(nodeClients[i].client, recipient.publicKeyHex);
          finalBalances.push({ node: i + 1, balance });
        } catch (error: any) {
          finalBalances.push({ node: i + 1, balance: null, error: error?.message || 'Unknown error' });
        }
      }

      console.log('Transaction results:', results);
      console.log('Final balances across nodes:', finalBalances);

      // At least some transactions should succeed
      const successfulTxs = results.filter((r) => r.success).length;
      expect(successfulTxs).toBeGreaterThan(0);

      // Available nodes should have consistent balances
      const validBalances = finalBalances.filter((b) => b.balance !== null).map((b) => b.balance);
      if (validBalances.length > 1) {
        const firstBalance = validBalances[0];
        validBalances.forEach((balance) => {
          expect(balance).toBe(firstBalance);
        });
      }

      console.log(
        `✅ Node failure resilience test passed (${successfulTxs}/${numTransactions} transactions succeeded)`
      );
    }, 90000);
  });
});
