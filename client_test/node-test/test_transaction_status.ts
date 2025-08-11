import crypto from 'crypto';
import nacl from 'tweetnacl';
import { GrpcClient } from './grpc_client';
import {
  TransactionTracker,
  TransactionStatus,
  TransactionStatusUtils,
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

// Note: serializeTx function removed - we now use server-returned transaction hashes

function signTx(tx: Tx, privateKey: crypto.KeyObject): string {
  // Serialize transaction data for signing (same format as before)
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

async function main() {
  console.log('=== TRANSACTION STATUS TRACKING TEST (Event-Based System) ===\n');
  console.log('This test validates the new event-driven transaction status system that replaces polling.\n');

  const grpcClient = new GrpcClient(GRPC_SERVER_ADDRESS);
  const tracker = new TransactionTracker({ serverAddress: GRPC_SERVER_ADDRESS });

  // Event tracking arrays
  const statusChangedEvents: Array<{ txHash: string; oldStatus: TransactionStatus; newStatus: TransactionStatus }> = [];
  const finalizedEvents: Array<{ txHash: string; statusInfo: TransactionStatusInfo }> = [];
  const failedEvents: Array<{ txHash: string; statusInfo: TransactionStatusInfo }> = [];

  let completedTransactions = 0;
  const totalTransactions = 100;
  const txHashes: string[] = [];

  try {
    console.log(`🚀 Sending ${totalTransactions} transactions...`);
    
    // Send 100 transactions
    for (let i = 0; i < totalTransactions; i++) {
      const tx = buildTx(
        faucetPublicKeyHex, 
        recipientPublicKeyHex, 
        10 + i, // Varying amounts
        `Batch test transaction ${i + 1}`, 
        i, // Incrementing nonce
        FaucetTxType
      );
      tx.signature = signTx(tx, faucetPrivateKey);

      const response = await sendTxViaGrpc(grpcClient, tx);
      if (!response.ok || !response.tx_hash) {
        console.error(`Failed to send transaction ${i + 1}: ${response.error}`);
        continue;
      }
      
      txHashes.push(response.tx_hash);
      
      // Add small delay to avoid overwhelming the server
      if (i % 10 === 0) {
        console.log(`📤 Sent ${i + 1}/${totalTransactions} transactions`);
        await new Promise(resolve => setTimeout(resolve, 100));
      }
    }

    console.log(`✅ All ${txHashes.length} transactions sent successfully`);
    console.log(`📊 Tracking ${txHashes.length} transactions...`);

    // Track all transactions
    await tracker.trackTransactions(txHashes);

    // Set up event listeners
    tracker.on('trackingStarted', (txHash: string) => {
      console.log(`🔄 Started tracking transaction: ${txHash.substring(0, 16)}...`);
    });

    // Test event-driven tracking
    tracker.on('statusChanged', (txHash: string, oldStatus: TransactionStatus, newStatus: TransactionStatus) => {
      const oldStatusStr = oldStatus !== undefined ? TransactionTracker.getStatusString(oldStatus) : 'UNKNOWN';
      const newStatusStr = TransactionTracker.getStatusString(newStatus);
      console.log(`📈 Status changed: ${oldStatusStr} -> ${newStatusStr} (${txHash.substring(0, 16)}...)`);
      statusChangedEvents.push({ txHash, oldStatus, newStatus });
    });

    tracker.on('transactionFinalized', (txHash: string, statusInfo: TransactionStatusInfo) => {
      completedTransactions++;
      console.log(`🎉 Transaction finalized (${completedTransactions}/${totalTransactions}): ${txHash.substring(0, 16)}...`);
      finalizedEvents.push({ txHash, statusInfo });
      
      if (completedTransactions >= totalTransactions) {
        console.log(`✅ All ${totalTransactions} transactions completed!`);
      }
    });

    tracker.on('transactionFailed', (txHash: string, statusInfo: TransactionStatusInfo) => {
      completedTransactions++;
      console.log(`💥 Transaction failed (${completedTransactions}/${totalTransactions}): ${txHash.substring(0, 16)}...`);
      failedEvents.push({ txHash, statusInfo });
      
      if (completedTransactions >= totalTransactions) {
        console.log(`✅ All ${totalTransactions} transactions completed!`);
      }
    });

    // Wait for all transactions to complete
    const maxWaitTime = 300000; // 5 minutes
    const startTime = Date.now();
    
    while (completedTransactions < totalTransactions && (Date.now() - startTime) < maxWaitTime) {
      await new Promise((resolve) => setTimeout(resolve, 1000));
      
      // Progress update every 10 seconds
      if ((Date.now() - startTime) % 10000 < 1000) {
        console.log(`⏳ Progress: ${completedTransactions}/${totalTransactions} completed (${Math.round(completedTransactions/totalTransactions*100)}%)`);
      }
    }
    
    if (completedTransactions >= totalTransactions) {
      console.log(`🎯 SUCCESS: All ${totalTransactions} transactions processed!`);
    } else {
      console.log(`⏰ TIMEOUT: Only ${completedTransactions}/${totalTransactions} transactions completed`);
    }

    // Summary of test results
    console.log('\n=== TEST SUMMARY ===');
    console.log(`📊 Total transactions sent: ${txHashes.length}`);
    console.log(`📊 Total transactions completed: ${completedTransactions}`);
    console.log(`📊 Status change events: ${statusChangedEvents.length}`);
    console.log(`🎉 Finalized transactions: ${finalizedEvents.length}`);
    console.log(`💥 Failed transactions: ${failedEvents.length}`);
    console.log(`⏱️  Test duration: ${Math.round((Date.now() - startTime) / 1000)}s`);
    
    if (completedTransactions === totalTransactions) {
      console.log('🎯 PERFECT: All transactions completed successfully!');
    } else if (completedTransactions > totalTransactions * 0.9) {
      console.log('✅ EXCELLENT: Over 90% of transactions completed!');
    } else if (completedTransactions > totalTransactions * 0.7) {
      console.log('👍 GOOD: Over 70% of transactions completed!');
    } else {
      console.log('⚠️  NEEDS IMPROVEMENT: Less than 70% of transactions completed');
    }
  } catch (error) {
    console.error('Test failed:', error);
  } finally {
    // Clean up
    tracker.close();
    grpcClient.close();
  }
}

if (require.main === module) {
  main().catch(console.error);
}
