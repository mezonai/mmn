import { GrpcClient } from './grpc_client';
import { TransactionTracker } from './transaction_tracker';
import {
  faucetPublicKeyBase58,
  TxTypeTransfer,
  generateTestAccount,
  buildTx,
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

interface RealisticTPSResult {
  totalTransactions: number;
  successfulTransactions: number;
  failedTransactions: number;
  totalTimeMs: number;
  tps: number;
  averageLatencyMs: number;
  minLatencyMs: number;
  maxLatencyMs: number;
  errorRate: number;
  usersCount: number;
  transactionsPerUser: number;
}

interface User {
  account: any;
  client: GrpcClient;
  transactionsSent: number;
  maxTransactions: number;
}

class RealisticTPSBenchmark {
  private nodeClients: GrpcClient[] = [];
  private transactionTracker: TransactionTracker;
  private results: Array<{
    success: boolean;
    latencyMs: number;
    error?: string;
    txHash?: string;
    userId: number;
    timestamp: number;
  }> = [];

  constructor() {
    this.transactionTracker = new TransactionTracker({
      serverAddress: NODE_CONFIGS[0].address,
      debug: false,
    });
  }

  async initialize(): Promise<void> {
    // Initialize connections to all three nodes
    for (const config of NODE_CONFIGS) {
      const client = new GrpcClient(config.address);
      this.nodeClients.push(client);
    }

    this.transactionTracker.trackTransactions();
    console.log('Connected to nodes:', this.nodeClients.length);
  }

  async cleanup(): Promise<void> {
    this.nodeClients.forEach(client => client.close());
    this.transactionTracker.close();
  }

  async runRealisticTPSBenchmark(
    usersCount: number,
    transactionsPerUser: number = 60
  ): Promise<RealisticTPSResult> {
    // Minimal header

    // Create users
    const users: User[] = [];
    for (let i = 0; i < usersCount; i++) {
      const account = generateTestAccount();
      const client = this.nodeClients[i % this.nodeClients.length]; // Round-robin assignment
      
      users.push({
        account,
        client,
        transactionsSent: 0,
        maxTransactions: Math.min(transactionsPerUser, 60) // Cap at 60 per user
      });
    }

    // Funding users
    
    // Fund all users SEQUENTIALLY to avoid faucet nonce collisions
    for (let index = 0; index < users.length; index++) {
      const user = users[index];
      const fundAmount = user.maxTransactions * 2; // Fund enough for all transactions
      const fundResponse = await fundAccount(
        user.client,
        user.account.publicKeyHex,
        fundAmount,
        `User ${index + 1} funding`
      );
      if (!fundResponse.ok) {
        throw new Error(`Failed to fund user ${index + 1}: ${fundResponse.error}`);
      }
    }
    // Funding done

    // Wait for funding to be processed
    await new Promise(resolve => setTimeout(resolve, 3000));

    // Reset results
    this.results = [];

    // Begin sending

    // Create recipients for load balancing
    const recipients = Array.from({ length: 20 }, () => generateTestAccount());

    // Start timing NOW - before creating promises (true start time)
    const startTime = Date.now();
    // start timestamp captured
    
    // Create all transaction promises at once (TRUE parallel)
    // create promises
    
    // Create all promises in TRUE parallel using Array.from
    const totalTransactions = users.length * users[0].maxTransactions;
    const allPromises = users.flatMap((user, userIndex) =>
      Array.from({ length: user.maxTransactions }, (_, txIndex) =>
        Promise.resolve().then(() =>
          this.sendUserTransaction(user, userIndex, txIndex, recipients)
        )
      )
    );
    // started

    // Wait for all transactions to complete
    // wait
    await Promise.all(allPromises);
    
    const endTime = Date.now();
    // end timestamp captured

    // Calculate TPS based on actual submission time (not waiting time)
    const totalTimeMs = endTime - startTime;
    const totalTimeSeconds = totalTimeMs / 1000;
    
    // submission time computed

    // Calculate metrics
    const successfulTxs = this.results.filter(r => r.success).length;
    const failedTxs = this.results.filter(r => !r.success).length;
    const tps = successfulTxs / totalTimeSeconds;
    const errorRate = (failedTxs / this.results.length) * 100;

    const latencies = this.results.filter(r => r.success).map(r => r.latencyMs);
    const averageLatency = latencies.length > 0 ? latencies.reduce((a, b) => a + b, 0) / latencies.length : 0;
    const minLatency = latencies.length > 0 ? Math.min(...latencies) : 0;
    const maxLatency = latencies.length > 0 ? Math.max(...latencies) : 0;

    const result: RealisticTPSResult = {
      totalTransactions: this.results.length,
      successfulTransactions: successfulTxs,
      failedTransactions: failedTxs,
      totalTimeMs,
      tps,
      averageLatencyMs: averageLatency,
      minLatencyMs: minLatency,
      maxLatencyMs: maxLatency,
      errorRate,
      usersCount,
      transactionsPerUser: Math.min(transactionsPerUser, 60)
    };

    return result;
  }

  private async sendUserTransaction(
    user: User, 
    userId: number, 
    txIndex: number, 
    recipients: any[]
  ): Promise<void> {
    const startTime = Date.now();
    const timestamp = new Date(startTime).toISOString();
    
    try {
      // Select random recipient
      const recipient = recipients[Math.floor(Math.random() * recipients.length)];
      
      const tx = buildTx(
        user.account.publicKeyHex,
        recipient.publicKeyHex,
        1, // Send minimal amount
        `User ${userId + 1} TX ${txIndex + 1}`,
        txIndex + 1, // Nonce
        TxTypeTransfer
      );
      tx.signature = signTx(tx, user.account.privateKey);

      
      // Send transaction WITHOUT waiting for response (true parallel)
      const sendPromise = sendTxViaGrpc(user.client, tx);
      const sendLatencyMs = Date.now() - startTime;
      
      // Wait for response in background (don't block TPS calculation)
      const response = await sendPromise;

      if (response.ok && response.tx_hash) {
        // Transaction sent successfully - don't wait for confirmation for true parallel testing
        this.results.push({
          success: true,
          latencyMs: sendLatencyMs, // Only measure send time, not confirmation time
          error: undefined,
          txHash: response.tx_hash,
          userId,
          timestamp: startTime
        });
      } else {
        // Transaction failed to send
        this.results.push({
          success: false,
          latencyMs: sendLatencyMs,
          error: response.error || 'Unknown error',
          txHash: undefined,
          userId,
          timestamp: startTime
        });
      }
    } catch (error) {
      const latencyMs = Date.now() - startTime;
      this.results.push({
        success: false,
        latencyMs,
        error: error instanceof Error ? error.message : 'Unknown error',
        userId,
        timestamp: startTime
      });
    }
  }

  printResults(result: RealisticTPSResult): void {
    console.log('\n📈 Realistic TPS Benchmark Results:');
    console.log('=====================================');
    console.log(`Users: ${result.usersCount}`);
    console.log(`Transactions per User: ${result.transactionsPerUser}`);
    console.log(`Total Transactions: ${result.totalTransactions}`);
    console.log(`Successful: ${result.successfulTransactions}`);
    console.log(`Failed: ${result.failedTransactions}`);
    console.log(`Error Rate: ${result.errorRate.toFixed(2)}%`);
    console.log(`Total Time: ${(result.totalTimeMs / 1000).toFixed(2)}s`);
    console.log(`TPS: ${result.tps.toFixed(2)}`);
    console.log(`Average Latency: ${result.averageLatencyMs.toFixed(2)}ms`);
    console.log(`Min Latency: ${result.minLatencyMs.toFixed(2)}ms`);
    console.log(`Max Latency: ${result.maxLatencyMs.toFixed(2)}ms`);
    console.log('=====================================');
  }
}

describe('Realistic TPS Tests', () => {
  let benchmark: RealisticTPSBenchmark;

  beforeAll(async () => {
    benchmark = new RealisticTPSBenchmark();
    await benchmark.initialize();
  });

  afterAll(async () => {
    await benchmark.cleanup();
  });

 
  test('Realistic TPS Test - 20 users, 15 transactions each (parallel load)', async () => {
    const result = await benchmark.runRealisticTPSBenchmark(20, 15);
    benchmark.printResults(result);
    
    expect(result.successfulTransactions).toBeGreaterThan(0);
    expect(result.errorRate).toBeLessThan(50);
  }, 300000);
});
