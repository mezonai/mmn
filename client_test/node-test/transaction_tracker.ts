import { EventEmitter } from 'events';
import { GrpcClient } from './grpc_client';

export enum TransactionStatus {
  PENDING = 0,
  CONFIRMED = 1,
  FINALIZED = 2,
  FAILED = 3,
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
      PENDING: TransactionStatus.PENDING,
      CONFIRMED: TransactionStatus.CONFIRMED,
      FINALIZED: TransactionStatus.FINALIZED,
      FAILED: TransactionStatus.FAILED,
    };

    // Add delay to simulate slower status consumption
    if (this.statusConsumptionDelay > 0) {
      console.log(
        `⏳ Slow consume: Waiting ${
          this.statusConsumptionDelay
        }ms before processing status update for ${txHash.substring(0, 16)}...`
      );
      await new Promise((resolve) => setTimeout(resolve, this.statusConsumptionDelay));
    }

    const statusEnum = !update.status ? TransactionStatus.FAILED : statusMap[update.status];

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
   * Check if a transaction status is terminal (FINALIZED, FAILED)
   */
  private isTerminalStatus(status: TransactionStatus): boolean {
    return status === TransactionStatus.FINALIZED || status === TransactionStatus.FAILED;
  }

  /**
   * Wait for a transaction to reach any terminal status (FINALIZED, FAILED)
   */
  async waitForTerminalStatus(txHash: string, timeoutMs: number = 30000): Promise<TransactionStatusInfo> {
    return new Promise((resolve, reject) => {
      const timeout = setTimeout(() => {
        this.off('statusChanged', statusListener);
        reject(new Error(`Timeout waiting for terminal status for ${txHash}`));
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
   * Check if a status represents a terminal state
   */
  static isTerminalStatus(status: TransactionStatus): boolean {
    return status === TransactionStatus.FINALIZED || status === TransactionStatus.FAILED;
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
