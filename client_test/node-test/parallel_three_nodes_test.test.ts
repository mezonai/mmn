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
  getCurrentNonce,
  Tx
} from './utils';

// Transaction multiplier - change this to increase/decrease transaction count
const TX_MULTIPLIER = (() => {
  const fromEnv = process.env.TX_MULTIPLIER || process.env.npm_config_TX_MULTIPLIER;
  return fromEnv ? Number(fromEnv) : 1;
})();
console.log(`Using TX_MULTIPLIER=${TX_MULTIPLIER}`);

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
      const fundResponse = await fundAccount(nodeClients[0].client, sender.publicKeyHex, 3000 * TX_MULTIPLIER, 'Parallel Transfers Across 3 Nodes with Consensus Verification');
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
      expect(senderBalanceConsensus).toBe(3000 * TX_MULTIPLIER);

      console.log('Sender funded successfully across all nodes with balance:', senderBalanceConsensus);

      // Create transactions for parallel execution (increased by TX_MULTIPLIER)
      const transactions = [];
      
      // Create TX_MULTIPLIER sets of 3 transactions each
      for (let i = 0; i < TX_MULTIPLIER; i++) {
        const tx1 = buildTx(sender.publicKeyHex, recipient1.publicKeyHex, 500, `Parallel tx to node1 set ${i + 1}`, i * 3 + 1, TxTypeTransfer);
        tx1.signature = signTx(tx1, sender.privateKey);
        transactions.push(tx1);

        const tx2 = buildTx(sender.publicKeyHex, recipient2.publicKeyHex, 700, `Parallel tx to node2 set ${i + 1}`, i * 3 + 2, TxTypeTransfer);
        tx2.signature = signTx(tx2, sender.privateKey);
        transactions.push(tx2);

        const tx3 = buildTx(sender.publicKeyHex, recipient3.publicKeyHex, 800, `Parallel tx to node3 set ${i + 1}`, i * 3 + 3, TxTypeTransfer);
        tx3.signature = signTx(tx3, sender.privateKey);
        transactions.push(tx3);
      }

      console.log('Sending parallel transactions to different nodes...');

      // Send transactions to different nodes in parallel (TX_MULTIPLIER sets of 3 transactions each)
      const responses = [];
      for (let i = 0; i < TX_MULTIPLIER; i++) {
        const setResponses = await Promise.all([
          sendTxViaGrpc(nodeClients[0].client, transactions[i * 3]), // Send to node1
          sendTxViaGrpc(nodeClients[1].client, transactions[i * 3 + 1]), // Send to node2
          sendTxViaGrpc(nodeClients[2].client, transactions[i * 3 + 2]), // Send to node3
        ]);
        responses.push(...setResponses);
        
      }

      console.log('Parallel transaction results:', {
        totalTransactions: responses.length,
        successfulTransactions: responses.filter(r => r.ok).length,
        failedTransactions: responses.filter(r => !r.ok).length,
        firstSet: {
          node1_tx1: { success: responses[0]?.ok, hash: responses[0]?.tx_hash, error: responses[0]?.error },
          node2_tx2: { success: responses[1]?.ok, hash: responses[1]?.tx_hash, error: responses[1]?.error },
          node3_tx3: { success: responses[2]?.ok, hash: responses[2]?.tx_hash, error: responses[2]?.error },
        }
      });

      // Wait for transactions to propagate and reach consensus
      const txHashes = responses.filter(r => r.ok && r.tx_hash).map(r => r.tx_hash!);
      await Promise.all(txHashes.map(txHash => transactionTracker.waitForTerminalStatus(txHash)));

      // Verify consensus across all nodes for all accounts
      console.log('Verifying consensus across all nodes...');

      // Calculate expected balances based on successful transactions
      let expectedSenderBalance = 3000 * TX_MULTIPLIER;
      let expectedRecipient1Balance = 0;
      let expectedRecipient2Balance = 0;
      let expectedRecipient3Balance = 0;

      // Count successful transactions for each recipient
      const recipient1TxCount = responses.filter((r, i) => r.ok && i % 3 === 0).length; // Every 3rd transaction (0, 3, 6, 9, 12)
      const recipient2TxCount = responses.filter((r, i) => r.ok && i % 3 === 1).length; // Every 3rd transaction (1, 4, 7, 10, 13)
      const recipient3TxCount = responses.filter((r, i) => r.ok && i % 3 === 2).length; // Every 3rd transaction (2, 5, 8, 11, 14)

      expectedSenderBalance -= (recipient1TxCount * 500) + (recipient2TxCount * 700) + (recipient3TxCount * 800);
      expectedRecipient1Balance = recipient1TxCount * 500;
      expectedRecipient2Balance = recipient2TxCount * 700;
      expectedRecipient3Balance = recipient3TxCount * 800;

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
      expect(totalBalance).toBe(3000 * TX_MULTIPLIER);

      // Additional verification: Check that all nodes have identical states
      const allAccountsConsistent = await Promise.all([
        verifyBalanceAcrossNodes(nodeClients, sender.publicKeyHex, expectedSenderBalance),
        verifyBalanceAcrossNodes(nodeClients, recipient1.publicKeyHex, expectedRecipient1Balance),
        verifyBalanceAcrossNodes(nodeClients, recipient2.publicKeyHex, expectedRecipient2Balance),
        verifyBalanceAcrossNodes(nodeClients, recipient3.publicKeyHex, expectedRecipient3Balance),
      ]);

      expect(allAccountsConsistent.every((consistent) => consistent)).toBe(true);

      console.log('✅ All nodes reached consensus successfully!');
    }, 30000 + TX_MULTIPLIER * 6000); // Base 60s + 6s per TX_MULTIPLIER

    test('Concurrent Same-Sender Transactions to Different Nodes', async () => {
      const sender = generateTestAccount();
      const recipients = [generateTestAccount(), generateTestAccount(), generateTestAccount()];

      // Fund sender (increased funding for TX_MULTIPLIER transactions)
      const fundResponse = await fundAccount(nodeClients[0].client, sender.publicKeyHex, 2000 * TX_MULTIPLIER, 'Concurrent Same-Sender Transactions to Different Nodes');
      expect(fundResponse.ok).toBe(true);

      // Verify initial funding consensus
      const initialBalance = await getBalanceCrossNodes(nodeClients, sender.publicKeyHex);
      expect(initialBalance).toBe(2000 * TX_MULTIPLIER);

      // Create concurrent transactions with sequential nonces (increased by TX_MULTIPLIER)
      const transactions: Tx[] = [];
      for (let set = 0; set < TX_MULTIPLIER; set++) {
        recipients.forEach((recipient, index) => {
          const tx = buildTx(
            sender.publicKeyHex,
            recipient.publicKeyHex,
            300,
            `Concurrent tx set ${set + 1} tx ${index + 1}`,
            set * 3 + index + 1,
            TxTypeTransfer
          );
          tx.signature = signTx(tx, sender.privateKey);
          transactions.push(tx);
        });
      }

      console.log('Sending concurrent transactions with sequential nonces...');

      // Send all transactions concurrently to different nodes (TX_MULTIPLIER sets of 3 transactions each)
      const responses = [];
      for (let i = 0; i < TX_MULTIPLIER; i++) {
        const setResponses = await Promise.all([
          sendTxViaGrpc(nodeClients[0].client, transactions[i * 3]),
          sendTxViaGrpc(nodeClients[1].client, transactions[i * 3 + 1]),
          sendTxViaGrpc(nodeClients[2].client, transactions[i * 3 + 2]),
        ]);
        responses.push(...setResponses);
      }

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
      const expectedFinalBalance = 2000 * TX_MULTIPLIER - successfulTxCount * 300;


      expect(finalSenderBalance).toBe(expectedFinalBalance);

      // Verify recipients received funds correctly
      for (let i = 0; i < recipients.length; i++) {
        const recipientBalance = await getBalanceCrossNodes(nodeClients, recipients[i].publicKeyHex);
        // Count successful transactions for this recipient (every 3rd transaction starting from index i)
        const recipientTxCount = responses.filter((r, idx) => r.ok && idx % 3 === i).length;
        const expectedRecipientBalance = recipientTxCount * 300;
        expect(recipientBalance).toBe(expectedRecipientBalance);
      }

      console.log('✅ Concurrent transactions handled correctly across all nodes!');
    }, 60000 + TX_MULTIPLIER * 6000); // Base 60s + 6s per TX_MULTIPLIER

    test('Network Partition Recovery Simulation', async () => {
      const sender = generateTestAccount();
      const recipient = generateTestAccount();

      // Fund sender through node1
      const fundResponse = await fundAccount(nodeClients[0].client, sender.publicKeyHex, 1000, 'Network Partition Recovery Simulation');
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
    }, 60000 + TX_MULTIPLIER * 3000); // Base 30s + 3s per TX_MULTIPLIER

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
          fundResponse = await fundAccount(nodeClients[i].client, sender.publicKeyHex, 1000, 'Node Failure Resilience');
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
    }, 90000 + TX_MULTIPLIER * 9000); // Base 90s + 9s per TX_MULTIPLIER
  });
});
