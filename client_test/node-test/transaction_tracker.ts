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
  pollInterval?: number; // milliseconds
  maxRetries?: number;
  timeout?: number; // milliseconds
}

export class TransactionTracker extends EventEmitter {
  private grpcClient: GrpcClient;
  private trackedTransactions: Map<string, TransactionStatusInfo> = new Map();
  private subscriptions: Map<string, () => void> = new Map();
  private pollInterval: number;

  constructor(options: TransactionTrackerOptions = {}) {
    super();

    const {
      serverAddress = '127.0.0.1:9001',
      pollInterval = 2000, // 2 seconds
    } = options;

    this.pollInterval = pollInterval;

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
      300, // 5 minutes timeout
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
   * Stop tracking all transactions
   */
  stopTrackingAll(): void {
    // Cancel all subscriptions
    for (const unsubscribe of this.subscriptions.values()) {
      unsubscribe();
    }
    this.subscriptions.clear();

    this.trackedTransactions.clear();
    this.emit('trackingStoppedAll');
  }

  /**
   * Get all tracked transactions
   */
  getTrackedTransactions(): Map<string, TransactionStatusInfo> {
    return new Map(this.trackedTransactions);
  }

  /**
   * Wait for a transaction to reach a specific status
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

      const checkStatus = async () => {
        try {
          const statusInfo = await this.getTransactionStatus(txHash);

          if (statusInfo.status === targetStatus) {
            clearTimeout(timeout);
            resolve(statusInfo);
            return;
          }

          // Check if we've exceeded timeout
          if (Date.now() - startTime > timeoutMs) {
            clearTimeout(timeout);
            reject(
              new Error(`Timeout waiting for transaction ${txHash} to reach status ${TransactionStatus[targetStatus]}`)
            );
            return;
          }

          // Continue polling
          setTimeout(checkStatus, this.pollInterval);
        } catch (error) {
          clearTimeout(timeout);
          reject(error);
        }
      };

      checkStatus();
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
   * Get transaction status with retry logic
   */
  static async getStatusWithRetry(
    txHash: string,
    serverAddress?: string,
    maxRetries: number = 5
  ): Promise<TransactionStatusInfo> {
    const tracker = new TransactionTracker({ serverAddress });

    for (let i = 0; i < maxRetries; i++) {
      try {
        const status = await tracker.getTransactionStatus(txHash);
        return status;
      } catch (error) {
        if (i === maxRetries - 1) throw error;
        await new Promise((resolve) => setTimeout(resolve, 1000 * (i + 1))); // Exponential backoff
      }
    }

    throw new Error(`Failed to get status for transaction ${txHash} after ${maxRetries} retries`);
  }
}
