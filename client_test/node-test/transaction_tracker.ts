import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import * as path from 'path';
import { EventEmitter } from 'events';
import { GrpcClient } from './grpc_client';

// Load proto files from local client_test/proto directory
const PROTO_PATH = path.join(__dirname, 'proto');

const accountPackageDefinition = protoLoader.loadSync(path.join(PROTO_PATH, 'account.proto'), {
  keepCase: true,
  longs: String,
  enums: String,
  defaults: true,
  oneofs: true,
});

// Define proper types for the account proto
interface AccountServiceClient {
  GetAccount: (
    request: { address: string },
    callback: (error: grpc.ServiceError | null, response: { address: string; balance: string; nonce: string }) => void
  ) => void;
  GetTxHistory: (
    request: { address: string; limit: number; offset: number; filter: number },
    callback: (
      error: grpc.ServiceError | null,
      response: {
        total: number;
        txs: Array<{
          sender: string;
          recipient: string;
          amount: string;
          nonce: string;
          timestamp: string;
          status: string;
        }>;
      }
    ) => void
  ) => void;
}

const accountProto = grpc.loadPackageDefinition(accountPackageDefinition) as unknown as {
  mmn: { AccountService: new (address: string, credentials: grpc.ChannelCredentials) => AccountServiceClient };
};

export enum TransactionStatus {
  UNKNOWN = 0,
  PENDING = 1,
  CONFIRMED = 2,
  FINALIZED = 3,
  FAILED = 4,
  EXPIRED = 5,
}

export interface TransactionStatusInfo {
  txHash: string;
  status: TransactionStatus;
  blockSlot?: number;
  blockHash?: string;
  confirmations?: number;
  errorMessage?: string;
  timestamp: number;
}

export interface TransactionTrackerOptions {
  serverAddress?: string;
  maxRetries?: number;
  timeout?: number; // milliseconds
}

export class TransactionTracker extends EventEmitter {
  private grpcClient: GrpcClient;
  private trackedTransactions: Map<string, TransactionStatusInfo> = new Map();
  private subscriptions: Map<string, () => void> = new Map();

  constructor(options: TransactionTrackerOptions = {}) {
    super();

    const {
      serverAddress = '127.0.0.1:9001',
    } = options;

    // Initialize gRPC client
    this.grpcClient = new GrpcClient(serverAddress);
  }

  /**
   * Get the current status of a transaction via server RPC
   */
  async getTransactionStatus(txHash: string): Promise<TransactionStatusInfo> {
    try {
      const resp = await this.grpcClient.getTransactionStatus(txHash);
      // Server returns string status values ("PENDING", "CONFIRMED", etc.), convert to enum
      const statusMap: { [key: string]: TransactionStatus } = {
        UNKNOWN: TransactionStatus.UNKNOWN,
        PENDING: TransactionStatus.PENDING,
        CONFIRMED: TransactionStatus.CONFIRMED,
        FINALIZED: TransactionStatus.FINALIZED,
        FAILED: TransactionStatus.FAILED,
        EXPIRED: TransactionStatus.EXPIRED,
      };
      const statusEnum = statusMap[resp.status || 'UNKNOWN'] || TransactionStatus.UNKNOWN;

      const info: TransactionStatusInfo = {
        txHash,
        status: statusEnum,
        blockSlot: resp.block_slot ? Number(resp.block_slot) : undefined,
        blockHash: resp.block_hash,
        confirmations: resp.confirmations ? Number(resp.confirmations) : undefined,
        errorMessage: resp.error_message,
        timestamp: resp.timestamp ? Number(resp.timestamp) : Date.now(),
      };
      return info;
    } catch (error) {
      console.error(`Error getting transaction status for ${txHash}:`, error);
      return {
        txHash,
        status: TransactionStatus.UNKNOWN,
        timestamp: Date.now(),
      };
    }
  }

  /**
   * Track a transaction using subscription instead of polling
   */
  async trackTransaction(txHash: string): Promise<void> {
    if (this.trackedTransactions.has(txHash)) {
      return; // Already tracking
    }

    // Get initial status
    const initialStatus = await this.getTransactionStatus(txHash);
    this.trackedTransactions.set(txHash, initialStatus);

    // Emit tracking started event
    this.emit('trackingStarted', txHash);

    // Subscribe to status updates
    const unsubscribe = this.grpcClient.subscribeTransactionStatus(
      txHash,
      (update: {
        tx_hash: string;
        status: string;
        block_slot?: string;
        block_hash?: string;
        confirmations?: string;
        error_message?: string;
        timestamp?: string;
      }) => {
        this.handleStatusUpdate(txHash, update);
      },
      (error: any) => {
        console.error(`Subscription error for ${txHash}:`, error);
      },
      () => {
        console.log(`Subscription completed for ${txHash}`);
      }
    );

    this.subscriptions.set(txHash, unsubscribe);
  }

  /**
   * Track all transactions using subscription to all transaction events
   */
  trackAllTransactions(): void {
    console.log('🔍 Starting to track all transactions...');
    
    // Subscribe to all transaction events
    const unsubscribe = this.grpcClient.subscribeToAllTransactionStatus(
      (update: {
        tx_hash: string;
        status: string;
        block_slot?: string;
        block_hash?: string;
        confirmations?: string;
        error_message?: string;
        timestamp?: string;
      }) => {
        // Handle status update for any transaction
        this.handleStatusUpdate(update.tx_hash, update);
      },
      (error: any) => {
        console.error('Subscription error for all transactions:', error);
        this.emit('error', error);
      },
      () => {
        console.log('Subscription completed for all transactions');
        this.emit('allTransactionsSubscriptionCompleted');
      }
    );

    // Store the unsubscribe function with a special key
    this.subscriptions.set('*all*', unsubscribe);
    
    this.emit('allTransactionsTrackingStarted');
  }


  /**
   * Stop tracking a specific transaction
   */
  stopTracking(txHash: string): void {
    // Cancel subscription if exists
    const unsubscribe = this.subscriptions.get(txHash);
    if (unsubscribe) {
      unsubscribe();
      this.subscriptions.delete(txHash);
    }

    this.trackedTransactions.delete(txHash);
    this.emit('trackingStopped', txHash);
  }

  /**
   * Stop tracking all transactions
   */
  stopTrackingAllTransactions(): void {
    console.log('🛑 Stopping tracking of all transactions...');
    
    // Cancel the all-transactions subscription
    const unsubscribe = this.subscriptions.get('*all*');
    if (unsubscribe) {
      unsubscribe();
      this.subscriptions.delete('*all*');
    }
    
    this.emit('allTransactionsTrackingStopped');
  }

  /**
   * Stop tracking all transactions
   */
  stopTrackingAll(): void {
    // Cancel all subscriptions
    for (const unsubscribe of this.subscriptions.values()) {
      unsubscribe();
    }
    this.subscriptions.clear();

    this.trackedTransactions.clear();
    this.emit('allTrackingStopped');
  }

  /**
   * Handle status updates from subscription
   */
  private handleStatusUpdate(
    txHash: string,
    update: {
      tx_hash: string;
      status: string;
      block_slot?: string;
      block_hash?: string;
      confirmations?: string;
      error_message?: string;
      timestamp?: string;
    }
  ): void {
    const statusMap: { [key: string]: TransactionStatus } = {
      UNKNOWN: TransactionStatus.UNKNOWN,
      PENDING: TransactionStatus.PENDING,
      CONFIRMED: TransactionStatus.CONFIRMED,
      FINALIZED: TransactionStatus.FINALIZED,
      FAILED: TransactionStatus.FAILED,
      EXPIRED: TransactionStatus.EXPIRED,
    };

    const newStatus: TransactionStatusInfo = {
      txHash,
      status: statusMap[update.status] || TransactionStatus.UNKNOWN,
      blockSlot: update.block_slot ? Number(update.block_slot) : undefined,
      blockHash: update.block_hash,
      confirmations: update.confirmations ? Number(update.confirmations) : undefined,
      errorMessage: update.error_message,
      timestamp: update.timestamp ? Number(update.timestamp) : Date.now(),
    };

    const oldStatus = this.trackedTransactions.get(txHash);

    if (oldStatus && oldStatus.status !== newStatus.status) {
      // Status changed, update and emit event
      this.trackedTransactions.set(txHash, newStatus);
      this.emit('statusChanged', txHash, oldStatus.status, newStatus.status);

      // Emit specific status events
      const statusString = TransactionStatus[newStatus.status];
      if (statusString) {
        this.emit(`status:${statusString.toLowerCase()}`, txHash, newStatus);
      }

      // Emit completion events
      if (newStatus.status === TransactionStatus.FINALIZED) {
        this.emit('transactionFinalized', txHash, newStatus);
        this.stopTracking(txHash);
      } else if (newStatus.status === TransactionStatus.FAILED) {
        this.emit('transactionFailed', txHash, newStatus);
        this.stopTracking(txHash);
      } else if (newStatus.status === TransactionStatus.EXPIRED) {
        this.emit('transactionExpired', txHash, newStatus);
        this.stopTracking(txHash);
      }
    }
  }

  /**
   * Get all tracked transactions
   */
  getTrackedTransactions(): Map<string, TransactionStatusInfo> {
    return new Map(this.trackedTransactions);
  }

  /**
   * Get tracked transaction status
   */
  getTrackedTransactionStatus(txHash: string): TransactionStatusInfo | undefined {
    return this.trackedTransactions.get(txHash);
  }

  /**
   * Wait for a transaction to reach a specific status using subscription
   */
  async waitForStatus(
    txHash: string,
    targetStatus: TransactionStatus,
    timeoutMs: number = 60000
  ): Promise<TransactionStatusInfo> {
    return new Promise((resolve, reject) => {
      const startTime = Date.now();
      const timeout = setTimeout(() => {
        reject(
          new Error(`Timeout waiting for transaction ${txHash} to reach status ${TransactionStatus[targetStatus]}`)
        );
      }, timeoutMs);

      // Check if we're already tracking this transaction
      const currentStatus = this.getTrackedTransactionStatus(txHash);
      if (currentStatus && currentStatus.status === targetStatus) {
        clearTimeout(timeout);
        console.log(`  ✅ Target status already reached for ${txHash.substring(0, 16)}...`);
        resolve(currentStatus);
        return;
      }

      // Set up a one-time event listener for status changes
      const statusListener = (changedTxHash: string, oldStatus: TransactionStatus, newStatus: TransactionStatus) => {
        if (changedTxHash === txHash && newStatus === targetStatus) {
          clearTimeout(timeout);
          this.off('statusChanged', statusListener);
          
          const finalStatus = this.getTrackedTransactionStatus(txHash);
          if (finalStatus) {
            console.log(`  ✅ Target status reached for ${txHash.substring(0, 16)}... via subscription`);
            resolve(finalStatus);
          } else {
            reject(new Error(`Status reached but not found in tracked transactions`));
          }
        }
      };

      // Listen for status changes
      this.on('statusChanged', statusListener);

      // Start tracking the transaction if not already tracking
      if (!this.getTrackedTransactionStatus(txHash)) {
        this.trackTransaction(txHash).catch((error) => {
          clearTimeout(timeout);
          this.off('statusChanged', statusListener);
          console.log(`  ❌ Error starting tracking for ${txHash.substring(0, 16)}...:`, error);
          reject(error);
        });
      }

      // Also check for timeout
      const checkTimeout = () => {
        if (Date.now() - startTime > timeoutMs) {
          clearTimeout(timeout);
          this.off('statusChanged', statusListener);
          console.log(`  ⏰ Timeout exceeded for ${txHash.substring(0, 16)}...`);
          reject(
            new Error(`Timeout waiting for transaction ${txHash} to reach status ${TransactionStatus[targetStatus]}`)
          );
        } else {
          setTimeout(checkTimeout, 1000); // Check every second
        }
      };
      checkTimeout();
    });
  }

  /**
   * Wait for a transaction to be confirmed
   */
  async waitForConfirmation(txHash: string, timeoutMs: number = 60000): Promise<TransactionStatusInfo> {
    return this.waitForStatus(txHash, TransactionStatus.CONFIRMED, timeoutMs);
  }

  /**
   * Wait for a transaction to be finalized
   */
  async waitForFinalization(txHash: string, timeoutMs: number = 120000): Promise<TransactionStatusInfo> {
    return this.waitForStatus(txHash, TransactionStatus.FINALIZED, timeoutMs);
  }

  /**
   * Get status string for enum value
   */
  static getStatusString(status: TransactionStatus): string {
    return TransactionStatus[status];
  }

  /**
   * Check if a status represents a terminal state
   */
  static isTerminalStatus(status: TransactionStatus): boolean {
    return (
      status === TransactionStatus.FINALIZED ||
      status === TransactionStatus.FAILED ||
      status === TransactionStatus.EXPIRED
    );
  }

  /**
   * Close the gRPC connection
   */
  close(): void {
    this.stopTrackingAll();
    if (this.grpcClient) {
      this.grpcClient.close();
    }
  }
}

// Utility functions for transaction status tracking
export class TransactionStatusUtils {
  /**
   * Create a simple transaction tracker with default settings
   */
  static createTracker(serverAddress?: string): TransactionTracker {
    return new TransactionTracker({ serverAddress });
  }

  /**
   * Track a transaction and return a promise that resolves when finalized
   */
  static async trackToFinalization(
    txHash: string,
    serverAddress?: string,
    timeoutMs: number = 120000
  ): Promise<TransactionStatusInfo> {
    const tracker = new TransactionTracker({ serverAddress });

    try {
      await tracker.trackTransaction(txHash);
      return await tracker.waitForFinalization(txHash, timeoutMs);
    } finally {
      tracker.close();
    }
  }

  /**
   * Track a transaction and return a promise that resolves when confirmed
   */
  static async trackToConfirmation(
    txHash: string,
    serverAddress?: string,
    timeoutMs: number = 60000
  ): Promise<TransactionStatusInfo> {
    const tracker = new TransactionTracker({ serverAddress });

    try {
      await tracker.trackTransaction(txHash);
      return await tracker.waitForConfirmation(txHash, timeoutMs);
    } finally {
      tracker.close();
    }
  }

  /**
   * Get transaction status with retry logic (using subscription)
   */
  static async getStatusWithRetry(
    txHash: string,
    serverAddress?: string,
    maxRetries: number = 5
  ): Promise<TransactionStatusInfo> {
    const tracker = new TransactionTracker({ serverAddress });

    try {
      // Use subscription-based tracking instead of polling
      await tracker.trackTransaction(txHash);
      
      // Wait for any status update (with timeout)
      return new Promise((resolve, reject) => {
        const timeout = setTimeout(() => {
          reject(new Error(`Timeout getting status for transaction ${txHash}`));
        }, 10000); // 10 second timeout

                 const statusListener = (changedTxHash: string, oldStatus: TransactionStatus, newStatus: TransactionStatus) => {
           if (changedTxHash === txHash) {
             clearTimeout(timeout);
             tracker.off('statusChanged', statusListener);
             
             const status = tracker.getTrackedTransactionStatus(txHash);
             if (status) {
               resolve(status);
             } else {
               reject(new Error(`Status not found for transaction ${txHash}`));
             }
           }
         };

         tracker.on('statusChanged', statusListener);
         
         // Also check current status immediately
         const currentStatus = tracker.getTrackedTransactionStatus(txHash);
         if (currentStatus) {
           clearTimeout(timeout);
           tracker.off('statusChanged', statusListener);
           resolve(currentStatus);
         }
      });
    } finally {
      tracker.close();
    }
  }
}
