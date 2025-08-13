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
  statusConsumptionDelay?: number; // milliseconds - delay between processing status updates
}

export class TransactionTracker extends EventEmitter {
  private grpcClient: GrpcClient;
  private trackedTransactions: Map<string, TransactionStatusInfo> = new Map();
  private subscriptions: Map<string, () => void> = new Map();
  private statusConsumptionDelay: number;
  private terminalCount: number = 0; // Track total terminal transactions
  private lastTerminalCountLog: number = 0; // Track last logged count to avoid duplicate logs

  constructor(options: TransactionTrackerOptions = {}) {
    super();

    const {
      serverAddress = '127.0.0.1:9001',
      statusConsumptionDelay = 0, // Default to no delay
    } = options;

    // Initialize gRPC client
    this.grpcClient = new GrpcClient(serverAddress);
    this.statusConsumptionDelay = statusConsumptionDelay;
  }

  /**
   * Track all transactions using subscription to all transaction events
   */
  trackTransactions(): void {
    console.log('🔍 Starting to track all transactions...');
    
    // Subscribe to all transaction events
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
        // Handle status update for any transaction
        await this.handleStatusUpdate(update.tx_hash, update);
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
   * Stop tracking all transactions
   */
  stopTracking(): void {
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
  private async handleStatusUpdate(
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
  ): Promise<void> {
    const statusMap: { [key: string]: TransactionStatus } = {
      UNKNOWN: TransactionStatus.UNKNOWN,
      PENDING: TransactionStatus.PENDING,
      CONFIRMED: TransactionStatus.CONFIRMED,
      FINALIZED: TransactionStatus.FINALIZED,
      FAILED: TransactionStatus.FAILED,

    };

    // Add delay to simulate slower status consumption
    if (this.statusConsumptionDelay > 0) {
      console.log(`⏳ Slow consume: Waiting ${this.statusConsumptionDelay}ms before processing status update for ${txHash.substring(0, 16)}...`);
      await new Promise(resolve => setTimeout(resolve, this.statusConsumptionDelay));
    }

    const statusEnum = statusMap[update.status || 'UNKNOWN'] || TransactionStatus.UNKNOWN;

    const newStatus: TransactionStatusInfo = {
      txHash,
      status: statusEnum,
      blockSlot: update.block_slot ? Number(update.block_slot) : undefined,
      blockHash: update.block_hash,
      confirmations: update.confirmations ? Number(update.confirmations) : undefined,
      errorMessage: update.error_message,
      timestamp: update.timestamp ? Number(update.timestamp) : Date.now(),
    };

    const oldStatus = this.trackedTransactions.get(txHash);
    this.trackedTransactions.set(txHash, newStatus);

    // Check if this is a new terminal status (status changed to terminal)
    const wasTerminal = oldStatus ? this.isTerminalStatus(oldStatus.status) : false;
    const isTerminal = this.isTerminalStatus(newStatus.status);
    
    if (!wasTerminal && isTerminal) {
      // Transaction just reached terminal status
      this.terminalCount++;
      this.logTerminalCount(newStatus.status);
    }

    // Emit status change event
    this.emit('statusChanged', txHash, newStatus, oldStatus);

    // Emit specific status events
    if (newStatus.status === TransactionStatus.FINALIZED) {
      this.emit('transactionFinalized', txHash, newStatus);
    } else if (newStatus.status === TransactionStatus.FAILED) {
      this.emit('transactionFailed', txHash, newStatus);
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
   * Get the current real-time terminal count
   */
  getCurrentTerminalCount(): number {
    return this.terminalCount;
  }

  /**
   * Reset the terminal count (useful for new test runs)
   */
  resetTerminalCount(): void {
    this.terminalCount = 0;
    this.lastTerminalCountLog = 0;
    console.log('🔄 Terminal count reset to 0');
  }

  /**
   * Get terminal status counts from tracked transactions
   */
  getTerminalStatusCounts(): {
    total: number;
    finalized: number;
    failed: number;
    pending: number;
    confirmed: number;
    terminal: number;
    nonTerminal: number;
  } {
    const transactions = Array.from(this.trackedTransactions.values());
    
    const finalized = transactions.filter(tx => tx.status === TransactionStatus.FINALIZED).length;
    const failed = transactions.filter(tx => tx.status === TransactionStatus.FAILED).length;
    const pending = transactions.filter(tx => tx.status === TransactionStatus.PENDING).length;
    const confirmed = transactions.filter(tx => tx.status === TransactionStatus.CONFIRMED).length;
    
    const terminal = finalized + failed;
    const nonTerminal = pending + confirmed;
    const total = this.trackedTransactions.size;
    
    return {
      total,
      finalized,
      failed,
      pending,
      confirmed,
      terminal,
      nonTerminal
    };
  }

  /**
   * Check if a transaction status is terminal (FINALIZED, FAILED)
   */
  private isTerminalStatus(status: TransactionStatus): boolean {
    return status === TransactionStatus.FINALIZED || 
           status === TransactionStatus.FAILED;
  }

  /**
   * Log the current terminal count when a new transaction reaches terminal status
   */
  private logTerminalCount(terminalStatus: TransactionStatus): void {
    const statusEmoji = terminalStatus === TransactionStatus.FINALIZED ? '✅' :
                       terminalStatus === TransactionStatus.FAILED ? '❌' : '⏰';
    const statusName = TransactionStatus[terminalStatus];
    
    console.log(`🎯 [REAL-TIME] Transaction reached ${statusName} status! Total terminal transactions: ${this.terminalCount} ${statusEmoji}`);
  }

  /**
   * Get current transaction status from server
   */
  async getCurrentStatus(txHash: string): Promise<TransactionStatusInfo | null> {
    try {
      const status = await this.grpcClient.getTransactionStatus(txHash);
      
      // Log the raw status response from the server (with BigInt handling)
      const serializableStatus = {
        tx_hash: status.tx_hash,
        status: status.status,
        block_slot: status.block_slot,
        block_hash: status.block_hash,
        confirmations: status.confirmations,
        error_message: status.error_message,
        timestamp: status.timestamp,
      };
      console.log(`📊 Current Status Response for ${txHash.substring(0, 16)}...:`, JSON.stringify(serializableStatus, null, 2));
      
      // Convert server status to our enum
      const statusMap: { [key: string]: TransactionStatus } = {
        UNKNOWN: TransactionStatus.UNKNOWN,
        PENDING: TransactionStatus.PENDING,
        CONFIRMED: TransactionStatus.CONFIRMED,
        FINALIZED: TransactionStatus.FINALIZED,
        FAILED: TransactionStatus.FAILED,

      };

      const statusEnum = statusMap[status.status || 'UNKNOWN'] || TransactionStatus.UNKNOWN;

      return {
        txHash: status.tx_hash,
        status: statusEnum,
        blockSlot: status.block_slot ? parseInt(status.block_slot) : undefined,
        blockHash: status.block_hash,
        confirmations: status.confirmations ? parseInt(status.confirmations) : undefined,
        errorMessage: status.error_message,
        timestamp: status.timestamp ? parseInt(status.timestamp) * 1000 : Date.now(), // Convert to milliseconds
      };
    } catch (error) {
      console.error(`Failed to get current status for ${txHash}:`, error);
      return null;
    }
  }

  /**
   * Wait for a transaction to reach a specific status
   */
  async waitForStatus(
    txHash: string,
    targetStatus: TransactionStatus,
    timeoutMs: number = 30000
  ): Promise<TransactionStatusInfo> {
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.off('statusChanged', statusListener);
        reject(new Error(`Timeout waiting for status ${TransactionStatus[targetStatus]} for ${txHash.substring(0, 16)}...`));
      }, timeoutMs);

      const statusListener = (eventTxHash: string, newStatus: TransactionStatusInfo) => {
        if (eventTxHash === txHash && newStatus.status === targetStatus) {
          clearTimeout(timeout);
          this.off('statusChanged', statusListener);
          resolve(newStatus);
        }
      };

      this.on('statusChanged', statusListener);

      // Check if already at target status
      const currentStatus = this.getTrackedTransactionStatus(txHash);
      if (currentStatus && currentStatus.status === targetStatus) {
        clearTimeout(timeout);
        this.off('statusChanged', statusListener);
        resolve(currentStatus);
        return;
      }

      // Start tracking all transactions if not already tracking
      if (!this.subscriptions.has('*all*')) {
        this.trackTransactions();
      }
    });
  }

  /**
   * Wait for a transaction to be finalized
   */
  async waitForFinalization(txHash: string, timeoutMs: number = 30000): Promise<TransactionStatusInfo> {
    return this.waitForStatus(txHash, TransactionStatus.FINALIZED, timeoutMs);
  }

  /**
   * Wait for a transaction to reach any terminal status (FINALIZED, FAILED)
   */
  async waitForTerminalStatus(txHash: string, timeoutMs: number = 30000): Promise<TransactionStatusInfo> {
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.off('statusChanged', statusListener);
        reject(new Error(`Timeout waiting for terminal status for ${txHash.substring(0, 16)}...`));
      }, timeoutMs);

      const statusListener = (eventTxHash: string, newStatus: TransactionStatusInfo) => {
        if (eventTxHash === txHash && TransactionTracker.isTerminalStatus(newStatus.status)) {
          clearTimeout(timeout);
          this.off('statusChanged', statusListener);
          resolve(newStatus);
        }
      };

      this.on('statusChanged', statusListener);

      // Check if already at terminal status
      const currentStatus = this.getTrackedTransactionStatus(txHash);
      if (currentStatus && TransactionTracker.isTerminalStatus(currentStatus.status)) {
        clearTimeout(timeout);
        this.off('statusChanged', statusListener);
        resolve(currentStatus);
        return;
      }

      // Start tracking all transactions if not already tracking
      if (!this.subscriptions.has('*all*')) {
        this.trackTransactions();
      }
    });
  }

  /**
   * Wait for a transaction to be confirmed
   */
  async waitForConfirmation(txHash: string, timeoutMs: number = 30000): Promise<TransactionStatusInfo> {
    return this.waitForStatus(txHash, TransactionStatus.CONFIRMED, timeoutMs);
  }

  /**
   * Wait for any status update for a transaction
   */
  async waitForAnyStatusUpdate(txHash: string, timeoutMs: number = 30000): Promise<TransactionStatusInfo> {
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.off('statusChanged', statusListener);
        reject(new Error(`Timeout waiting for any status update for ${txHash.substring(0, 16)}...`));
      }, timeoutMs);

      const statusListener = (eventTxHash: string, newStatus: TransactionStatusInfo) => {
        if (eventTxHash === txHash) {
          clearTimeout(timeout);
          this.off('statusChanged', statusListener);
          resolve(newStatus);
        }
      };

      this.on('statusChanged', statusListener);

      // Check if we already have a status
      const currentStatus = this.getTrackedTransactionStatus(txHash);
      if (currentStatus) {
        clearTimeout(timeout);
        this.off('statusChanged', statusListener);
        resolve(currentStatus);
        return;
      }

      // Start tracking all transactions if not already tracking
      if (!this.subscriptions.has('*all*')) {
        this.trackTransactions();
      }
    });
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
      status === TransactionStatus.FAILED
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
   * Static method to wait for a transaction to be finalized
   */
  static async waitForFinalization(
    serverAddress: string,
    txHash: string,
    timeoutMs: number = 30000
  ): Promise<TransactionStatusInfo> {
    const tracker = new TransactionTracker({ serverAddress });

    try {
      tracker.trackTransactions();
      return await tracker.waitForFinalization(txHash, timeoutMs);
    } finally {
      tracker.stopTracking();
    }
  }

  /**
   * Static method to wait for a transaction to be confirmed
   */
  static async waitForConfirmation(
    serverAddress: string,
    txHash: string,
    timeoutMs: number = 30000
  ): Promise<TransactionStatusInfo> {
    const tracker = new TransactionTracker({ serverAddress });

    try {
      tracker.trackTransactions();
      return await tracker.waitForConfirmation(txHash, timeoutMs);
    } finally {
      tracker.stopTracking();
    }
  }

  /**
   * Static method to wait for any status update
   */
  static async waitForAnyStatusUpdate(
    serverAddress: string,
    txHash: string,
    timeoutMs: number = 30000
  ): Promise<TransactionStatusInfo> {
    const tracker = new TransactionTracker({ serverAddress });

    try {
      tracker.trackTransactions();
      return await tracker.waitForAnyStatusUpdate(txHash, timeoutMs);
    } finally {
      tracker.stopTracking();
    }
  }
}
