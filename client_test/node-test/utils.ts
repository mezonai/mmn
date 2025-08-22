import crypto from 'crypto';
import nacl from 'tweetnacl';
import { GrpcClient } from './grpc_client';

// Faucet keypair from genesis configuration
export const faucetPrivateKeyHex =
  '302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee';
export const faucetPrivateKeyDer = Buffer.from(faucetPrivateKeyHex, 'hex');
export const faucetSeed = faucetPrivateKeyDer.slice(-32);
export const faucetKeyPair = nacl.sign.keyPair.fromSeed(faucetSeed);
export const faucetPublicKeyHex = Buffer.from(faucetKeyPair.publicKey).toString('hex');
export const faucetPrivateKey = crypto.createPrivateKey({
  key: faucetPrivateKeyDer,
  format: 'der',
  type: 'pkcs8',
});

// Transaction type constants
export const TxTypeTransfer = 0;

// Transaction interface
export interface Tx {
  type: number;
  sender: string;
  recipient: string;
  amount: number;
  timestamp: number;// Should remove or not
  text_data: string;
  nonce: number;
  signature: string;
}

// Test account generation with enhanced metadata
export function generateTestAccount() {
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
export async function waitForTransaction(ms: number = 3000): Promise<void> {
  return new Promise(resolve => setTimeout(resolve, ms));
}

// Helper function to get account balance
export async function getAccountBalance(grpcClient: GrpcClient, address: string): Promise<number> {
  const account = await grpcClient.getAccount(address);
  return parseInt(account.balance);
}

// Build transaction object
export function buildTx(
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

// Serialize transaction for signing
export function serializeTx(tx: Tx): Buffer {
  const data = `${tx.type}|${tx.sender}|${tx.recipient}|${tx.amount}|${tx.text_data}|${tx.nonce}`;
  return Buffer.from(data, 'utf8');
}

// Sign transaction with Ed25519
export function signTx(tx: Tx, privateKey: crypto.KeyObject): string {
  const serializedData = serializeTx(tx);
  
  // Extract the Ed25519 seed from the private key for nacl signing
  const privateKeyDer = privateKey.export({ format: 'der', type: 'pkcs8' }) as Buffer;
  const seed = privateKeyDer.slice(-32); // Last 32 bytes are the Ed25519 seed
  const keyPair = nacl.sign.keyPair.fromSeed(seed);
  
  // Sign using Ed25519 (nacl)
  const signature = nacl.sign.detached(serializedData, keyPair.secretKey);
  return Buffer.from(signature).toString('hex');
}

// Send transaction via gRPC
export async function sendTxViaGrpc(grpcClient: GrpcClient, tx: Tx) {
  try {
    const response = await grpcClient.addTransaction({
      type: tx.type,
      sender: tx.sender,
      recipient: tx.recipient,
      amount: tx.amount,
      timestamp: tx.timestamp,
      text_data: tx.text_data,
      nonce: tx.nonce,
    }, tx.signature);
    
    return {
      ok: response.ok,
      tx_hash: response.tx_hash,
      error: response.error || null
    };
    
  } catch (error) {
    console.error('gRPC transaction failed:', error);
    return {
      ok: false,
      tx_hash: null,
      error: error instanceof Error ? error.message : 'Unknown error'
    };
  }
}

// Fund account with tokens (with retry logic for multi-threaded usage)
export async function fundAccount(grpcClient: GrpcClient, recipientAddress: string, amount: number, maxRetries: number = 3) {
  let lastError: any;
  
  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    try {
      await waitForTransaction(800); // ~ 2 slots
      // Get current faucet account nonce dynamically
      const faucetAccount = await grpcClient.getAccount(faucetPublicKeyHex);
      const currentNonce = parseInt(faucetAccount.nonce) + 1;
      
      console.log(`Fund transaction attempt ${attempt}: Using nonce ${currentNonce} (faucet account nonce: ${faucetAccount.nonce})`);
      
      const fundTx = buildTx(faucetPublicKeyHex, recipientAddress, amount, 'Funding account', currentNonce, TxTypeTransfer);
      fundTx.signature = signTx(fundTx, faucetPrivateKey);
      
      const response = await sendTxViaGrpc(grpcClient, fundTx);
      
      console.log(`Fund transaction response (attempt ${attempt}):`, response);
      
      // If successful, wait and verify the balance was updated
      if (response.ok) {
        await waitForTransaction(800); // ~ 2 slots
        try {
          const balance = await getAccountBalance(grpcClient, recipientAddress);
          console.log(`Account ${recipientAddress.substring(0, 8)}... funded with ${amount}, current balance: ${balance}`);
        } catch (error) {
          console.warn('Could not verify balance after funding:', error);
        }
        return response;
      } else {
         // Only retry on invalid nonce errors
         lastError = response.error;
         const isNonceError = response.error && (response.error.includes('nonce') || response.error.includes('invalid nonce'));
         
         if (isNonceError && attempt < maxRetries) {
            const delay = 1000; // Exponential backoff: 1s
            console.warn(`Fund transaction failed due to nonce issue (attempt ${attempt}/${maxRetries}): ${response.error}. Retrying in ${delay}ms...`);
            await new Promise(resolve => setTimeout(resolve, delay));
            continue;
          }
       }
      
      return response;
      
    } catch (error) {
       lastError = error;
       const errorMessage = error instanceof Error ? error.message : String(error);
       const isNonceError = errorMessage.includes('nonce') || errorMessage.includes('invalid nonce');
       
       if (isNonceError && attempt < maxRetries) {
          const delay = Math.pow(2, attempt - 1) * 1000; // Exponential backoff: 1s, 2s, 4s
          console.warn(`Fund transaction error due to nonce issue (attempt ${attempt}/${maxRetries}):`, error, `Retrying in ${delay}ms...`);
          await new Promise(resolve => setTimeout(resolve, delay));
          continue;
        }
     }
  }
  
  // All retries exhausted
  console.error(`Fund transaction failed after ${maxRetries} attempts. Last error:`, lastError);
  return {
    ok: false,
    tx_hash: null,
    error: lastError instanceof Error ? lastError.message : (lastError || 'Unknown error after retries')
  };
}