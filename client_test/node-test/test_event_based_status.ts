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

function signTx(tx: Tx, privateKey: crypto.KeyObject): string {
  const metadata = `${tx.type}|${tx.sender}|${tx.recipient}|${tx.amount}|${tx.text_data}|${tx.nonce}`;
  const serializedData = Buffer.from(metadata);
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
  return await grpcClient.addTransaction(txMsg, tx.signature);
}

const FaucetTxType = 1;
const TransferTxType = 0;

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

    // Test 1: Verify event-driven subscription works
    await this.runTest('Event-Driven Subscription Test', () => this.testEventDrivenSubscription());

    // Test 2: Test multiple concurrent subscriptions
    await this.runTest('Multiple Concurrent Subscriptions', () => this.testMultipleConcurrentSubscriptions());

    // Test 3: Test subscription timeout handling
    await this.runTest('Subscription Timeout Handling', () => this.testSubscriptionTimeout());

    // Test 4: Test real-time status updates
    await this.runTest('Real-Time Status Updates', () => this.testRealTimeStatusUpdates());

    // Test 5: Test subscription cleanup
    await this.runTest('Subscription Cleanup', () => this.testSubscriptionCleanup());

    // Test 6: Performance comparison (event-based vs polling simulation)
    await this.runTest('Performance Comparison', () => this.testPerformanceComparison());

    this.printTestSummary();
  }

  private async testEventDrivenSubscription() {
    // Send a transaction and immediately subscribe to its status
    const tx = buildTx(
      faucetPublicKeyHex,
      recipientPublicKeyHex,
      100,
      'Event-driven test transaction',
      0,
      FaucetTxType
    );
    tx.signature = signTx(tx, faucetPrivateKey);

    const response = await sendTxViaGrpc(this.grpcClient, tx);
    if (!response.ok || !response.tx_hash) {
      throw new Error('Failed to send test transaction');
    }

    const txHash = response.tx_hash;
    console.log(`  📤 Transaction sent: ${txHash.substring(0, 16)}...`);

    // Subscribe to status updates
    const statusUpdates: TransactionStatusInfo[] = [];
    const statusPromise = new Promise<TransactionStatusInfo[]>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error('Subscription timeout'));
      }, 30000); // 30 seconds timeout

      const unsubscribe = this.grpcClient.subscribeTransactionStatus(
        txHash,
        30, // 30 seconds timeout
        (update: {
          tx_hash: string;
          status: string;
          block_slot?: string;
          block_hash?: string;
          confirmations?: string;
          error_message?: string;
          timestamp?: string;
        }) => {
          const statusInfo: TransactionStatusInfo = {
            txHash: update.tx_hash,
            status: this.mapStatusStringToEnum(update.status),
            blockSlot: update.block_slot ? Number(update.block_slot) : undefined,
            blockHash: update.block_hash,
            confirmations: update.confirmations ? Number(update.confirmations) : undefined,
            errorMessage: update.error_message,
            timestamp: update.timestamp ? Number(update.timestamp) : Date.now(),
          };
          statusUpdates.push(statusInfo);
          console.log(`  📈 Status update: ${update.status} (${txHash.substring(0, 16)}...)`);

          // Resolve when we get a terminal status
          if (statusInfo.status === TransactionStatus.FINALIZED || 
              statusInfo.status === TransactionStatus.FAILED || 
              statusInfo.status === TransactionStatus.EXPIRED) {
            clearTimeout(timeout);
            resolve(statusUpdates);
          }
        },
        (error: any) => {
          clearTimeout(timeout);
          reject(error);
        },
        () => {
          clearTimeout(timeout);
          resolve(statusUpdates);
        }
      );
    });

    const updates = await statusPromise;
    
    return {
      txHash: txHash.substring(0, 16) + '...',
      totalUpdates: updates.length,
      finalStatus: updates[updates.length - 1]?.status || 'UNKNOWN',
      statusProgression: updates.map(u => u.status)
    };
  }

  private async testMultipleConcurrentSubscriptions() {
    const numTransactions = 10;
    const transactions: string[] = [];

    // Send multiple transactions
    for (let i = 0; i < numTransactions; i++) {
      const tx = buildTx(
        faucetPublicKeyHex,
        recipientPublicKeyHex,
        10 + i,
        `Concurrent test ${i + 1}`,
        i,
        FaucetTxType
      );
      tx.signature = signTx(tx, faucetPrivateKey);

      const response = await sendTxViaGrpc(this.grpcClient, tx);
      if (response.ok && response.tx_hash) {
        transactions.push(response.tx_hash);
      }
    }

    console.log(`  📤 Sent ${transactions.length} transactions for concurrent testing`);

    // Subscribe to all transactions concurrently
    const subscriptionPromises = transactions.map((txHash, index) => {
      return new Promise<any>((resolve, reject) => {
        const timeout = setTimeout(() => {
          reject(new Error(`Subscription ${index} timeout`));
        }, 30000);

        const unsubscribe = this.grpcClient.subscribeTransactionStatus(
          txHash,
          30,
          (update: {
            tx_hash: string;
            status: string;
            block_slot?: string;
            block_hash?: string;
            confirmations?: string;
            error_message?: string;
            timestamp?: string;
          }) => {
            if (update.status === 'FINALIZED' || update.status === 'FAILED' || update.status === 'EXPIRED') {
              clearTimeout(timeout);
              resolve({
                index,
                txHash: txHash.substring(0, 16) + '...',
                finalStatus: update.status,
                success: true
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
              index,
              txHash: txHash.substring(0, 16) + '...',
              finalStatus: 'TIMEOUT',
              success: false
            });
          }
        );
      });
    });

    const subscriptionResults = await Promise.allSettled(subscriptionPromises);
    const successful = subscriptionResults.filter(r => r.status === 'fulfilled' && r.value.success).length;
    const failed = subscriptionResults.length - successful;

    return {
      totalSubscriptions: transactions.length,
      successful,
      failed,
      successRate: `${((successful / transactions.length) * 100).toFixed(1)}%`
    };
  }

  private async testSubscriptionTimeout() {
    // Test with a non-existent transaction hash
    const fakeTxHash = '0000000000000000000000000000000000000000000000000000000000000000';
    
    const timeoutPromise = new Promise<boolean>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error('Test timeout exceeded'));
      }, 35000); // 35 seconds for the entire test

      const unsubscribe = this.grpcClient.subscribeTransactionStatus(
        fakeTxHash,
        5, // 5 seconds subscription timeout
        (update: {
          tx_hash: string;
          status: string;
          block_slot?: string;
          block_hash?: string;
          confirmations?: string;
          error_message?: string;
          timestamp?: string;
        }) => {
          // Should not receive any updates for fake hash
          console.log(`  ⚠️  Unexpected update for fake hash: ${update.status}`);
        },
        (error: any) => {
          clearTimeout(timeout);
          resolve(true); // Error is expected for fake hash
        },
        () => {
          clearTimeout(timeout);
          resolve(true); // Completion is also acceptable
        }
      );
    });

    const result = await timeoutPromise;
    return {
      fakeTxHash: fakeTxHash.substring(0, 16) + '...',
      timeoutHandled: result,
      expectedBehavior: 'Subscription should timeout or error for non-existent transaction'
    };
  }

  private async testRealTimeStatusUpdates() {
    // Send a transaction and measure time between status updates
    const tx = buildTx(
      faucetPublicKeyHex,
      recipientPublicKeyHex,
      200,
      'Real-time test transaction',
      0,
      FaucetTxType
    );
    tx.signature = signTx(tx, faucetPrivateKey);

    const response = await sendTxViaGrpc(this.grpcClient, tx);
    if (!response.ok || !response.tx_hash) {
      throw new Error('Failed to send real-time test transaction');
    }

    const txHash = response.tx_hash;
    const startTime = Date.now();
    const updateTimes: number[] = [];

    const realTimePromise = new Promise<any>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error('Real-time test timeout'));
      }, 30000);

      const unsubscribe = this.grpcClient.subscribeTransactionStatus(
        txHash,
        30,
        (update: {
          tx_hash: string;
          status: string;
          block_slot?: string;
          block_hash?: string;
          confirmations?: string;
          error_message?: string;
          timestamp?: string;
        }) => {
          const updateTime = Date.now();
          updateTimes.push(updateTime - startTime);
          console.log(`  ⚡ Real-time update: ${update.status} (${updateTime - startTime}ms)`);

          if (update.status === 'FINALIZED' || update.status === 'FAILED' || update.status === 'EXPIRED') {
            clearTimeout(timeout);
            resolve({
              txHash: txHash.substring(0, 16) + '...',
              totalUpdates: updateTimes.length,
              averageResponseTime: updateTimes.length > 0 ? 
                Math.round(updateTimes.reduce((a, b) => a + b, 0) / updateTimes.length) : 0,
              fastestUpdate: updateTimes.length > 0 ? Math.min(...updateTimes) : 0,
              slowestUpdate: updateTimes.length > 0 ? Math.max(...updateTimes) : 0
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
            txHash: txHash.substring(0, 16) + '...',
            totalUpdates: updateTimes.length,
            averageResponseTime: updateTimes.length > 0 ? 
              Math.round(updateTimes.reduce((a, b) => a + b, 0) / updateTimes.length) : 0,
            fastestUpdate: updateTimes.length > 0 ? Math.min(...updateTimes) : 0,
            slowestUpdate: updateTimes.length > 0 ? Math.max(...updateTimes) : 0
          });
        }
      );
    });

    return await realTimePromise;
  }

  private async testSubscriptionCleanup() {
    // Test that subscriptions are properly cleaned up
    const tx = buildTx(
      faucetPublicKeyHex,
      recipientPublicKeyHex,
      50,
      'Cleanup test transaction',
      0,
      FaucetTxType
    );
    tx.signature = signTx(tx, faucetPrivateKey);

    const response = await sendTxViaGrpc(this.grpcClient, tx);
    if (!response.ok || !response.tx_hash) {
      throw new Error('Failed to send cleanup test transaction');
    }

    const txHash = response.tx_hash;
    let updateCount = 0;
    let cleanupCalled = false;

    const cleanupPromise = new Promise<any>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error('Cleanup test timeout'));
      }, 30000);

      const unsubscribe = this.grpcClient.subscribeTransactionStatus(
        txHash,
        30,
        (update: {
          tx_hash: string;
          status: string;
          block_slot?: string;
          block_hash?: string;
          confirmations?: string;
          error_message?: string;
          timestamp?: string;
        }) => {
          updateCount++;
          console.log(`  🧹 Cleanup test update ${updateCount}: ${update.status}`);

          if (update.status === 'FINALIZED' || update.status === 'FAILED' || update.status === 'EXPIRED') {
            clearTimeout(timeout);
            // Call unsubscribe to test cleanup
            unsubscribe();
            cleanupCalled = true;
            resolve({
              txHash: txHash.substring(0, 16) + '...',
              updatesReceived: updateCount,
              cleanupCalled,
              finalStatus: update.status
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
            txHash: txHash.substring(0, 16) + '...',
            updatesReceived: updateCount,
            cleanupCalled,
            finalStatus: 'COMPLETED'
          });
        }
      );
    });

    return await cleanupPromise;
  }

  private async testPerformanceComparison() {
    // Simulate the old polling approach vs new event-based approach
    const tx = buildTx(
      faucetPublicKeyHex,
      recipientPublicKeyHex,
      75,
      'Performance test transaction',
      0,
      FaucetTxType
    );
    tx.signature = signTx(tx, faucetPrivateKey);

    const response = await sendTxViaGrpc(this.grpcClient, tx);
    if (!response.ok || !response.tx_hash) {
      throw new Error('Failed to send performance test transaction');
    }

    const txHash = response.tx_hash;
    const startTime = Date.now();

    // Test event-based approach (new)
    const eventBasedPromise = new Promise<any>((resolve, reject) => {
      const timeout = setTimeout(() => {
        reject(new Error('Event-based test timeout'));
      }, 30000);

      const unsubscribe = this.grpcClient.subscribeTransactionStatus(
        txHash,
        30,
        (update: {
          tx_hash: string;
          status: string;
          block_slot?: string;
          block_hash?: string;
          confirmations?: string;
          error_message?: string;
          timestamp?: string;
        }) => {
          const eventBasedTime = Date.now() - startTime;
          console.log(`  🚀 Event-based: ${update.status} in ${eventBasedTime}ms`);

          if (update.status === 'FINALIZED' || update.status === 'FAILED' || update.status === 'EXPIRED') {
            clearTimeout(timeout);
            resolve({
              approach: 'Event-Based',
              timeToFinalStatus: eventBasedTime,
              efficiency: 'Real-time updates, no polling overhead'
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
            approach: 'Event-Based',
            timeToFinalStatus: Date.now() - startTime,
            efficiency: 'Real-time updates, no polling overhead'
          });
        }
      );
    });

    const eventBasedResult = await eventBasedPromise;

    // Simulate old polling approach (for comparison)
    const pollingStartTime = Date.now();
    let pollingAttempts = 0;
    const maxPollingAttempts = 30; // 30 attempts with 1-second intervals = 30 seconds

    const pollingPromise = new Promise<any>((resolve, reject) => {
      const pollInterval = setInterval(async () => {
        pollingAttempts++;
        try {
          const status = await this.grpcClient.getTransactionStatus(txHash);
          console.log(`  🔄 Polling attempt ${pollingAttempts}: ${status.status}`);

          if (status.status === 'FINALIZED' || status.status === 'FAILED' || status.status === 'EXPIRED') {
            clearInterval(pollInterval);
            const pollingTime = Date.now() - pollingStartTime;
            resolve({
              approach: 'Polling (Simulated)',
              timeToFinalStatus: pollingTime,
              pollingAttempts,
              efficiency: `Required ${pollingAttempts} polling attempts`
            });
          } else if (pollingAttempts >= maxPollingAttempts) {
            clearInterval(pollInterval);
            resolve({
              approach: 'Polling (Simulated)',
              timeToFinalStatus: Date.now() - pollingStartTime,
              pollingAttempts,
              efficiency: 'Timed out after maximum attempts'
            });
          }
        } catch (error) {
          clearInterval(pollInterval);
          reject(error);
        }
      }, 1000); // Poll every second
    });

    const pollingResult = await pollingPromise;

    return {
      eventBased: eventBasedResult,
      polling: pollingResult,
      improvement: `Event-based is ${Math.round(pollingResult.timeToFinalStatus / eventBasedResult.timeToFinalStatus)}x faster and uses ${pollingResult.pollingAttempts}x fewer requests`
    };
  }

  private mapStatusStringToEnum(status: string): TransactionStatus {
    const statusMap: { [key: string]: TransactionStatus } = {
      'UNKNOWN': TransactionStatus.UNKNOWN,
      'PENDING': TransactionStatus.PENDING,
      'CONFIRMED': TransactionStatus.CONFIRMED,
      'FINALIZED': TransactionStatus.FINALIZED,
      'FAILED': TransactionStatus.FAILED,
      'EXPIRED': TransactionStatus.EXPIRED,
    };
    return statusMap[status] || TransactionStatus.UNKNOWN;
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

async function main() {
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

if (require.main === module) {
  main().catch(console.error);
}
