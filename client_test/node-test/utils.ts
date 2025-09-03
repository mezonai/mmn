import crypto from 'crypto';
import nacl from 'tweetnacl';
import bs58 from 'bs58';
import {GrpcClient} from './grpc_client';

// Faucet keypair from genesis configuration
export const faucetPrivateKeyHex =
  '302e020100300506032b6570042204208e92cf392cef0388e9855e3375c608b5eb0a71f074827c3d8368fac7d73c30ee';
export const faucetPrivateKeyDer = Buffer.from(faucetPrivateKeyHex, 'hex');
export const faucetSeed = faucetPrivateKeyDer.slice(-32);
export const faucetKeyPair = nacl.sign.keyPair.fromSeed(faucetSeed);
export const faucetPublicKeyBase58 = bs58.encode(faucetKeyPair.publicKey);
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
  extra_info: string
  signature: string;
}

// Test account generation with enhanced metadata
export function generateTestAccount() {
  const keyPair = nacl.sign.keyPair();
  // Keep the field name publicKeyHex for compatibility; store base58 value
  const publicKeyHex = bs58.encode(keyPair.publicKey);
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

// Helper function to get current nonce for an account
export async function getCurrentNonce(grpcClient: GrpcClient, address: string, tag: string = 'pending'): Promise<number> {
  const nonceResponse = await grpcClient.getCurrentNonce(address, tag);
  return parseInt(nonceResponse.nonce);
}

// Build transaction object
export function buildTx(
  sender: string,
  recipient: string,
  amount: number,
  text_data: string,
  nonce: number,
  type: number,
  extra_info?: string
): Tx {
  return {
    type,
    sender,
    recipient,
    amount,
    timestamp: Date.now(),
    text_data,
    nonce,
    extra_info: extra_info || "",
    signature: '',
  };
}

// Serialize transaction for signing
export function serializeTx(tx: Tx): Buffer {
  const data = `${tx.type}|${tx.sender}|${tx.recipient}|${tx.amount}|${tx.text_data}|${tx.nonce}|${tx.extra_info}`;
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
  return bs58.encode(Buffer.from(signature));
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
      extra_info: tx.extra_info
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
export async function fundAccount(grpcClient: GrpcClient, recipientAddress: string, amount: number, testNote: string, maxRetries: number = 5) {
  let lastError: any;
  
  for (let attempt = 1; attempt <= maxRetries; attempt++) {
    try {
      // Get current faucet account nonce using GetCurrentNonce gRPC method
      const currentNonceValue = await getCurrentNonce(grpcClient, faucetPublicKeyBase58, 'pending');
      const nextNonce = currentNonceValue + 1;
      
      console.log(`Fund transaction attempt ${attempt}: Using nonce ${nextNonce} (current nonce from gRPC: ${currentNonceValue})`);
      
      const fundTx = buildTx(faucetPublicKeyBase58, recipientAddress, amount, `Funding account for ${testNote}`, nextNonce, TxTypeTransfer);
      fundTx.signature = signTx(fundTx, faucetPrivateKey);
      
      const response = await sendTxViaGrpc(grpcClient, fundTx);
      
      console.log(`Fund transaction response (attempt ${attempt}):`, response);
      // If successful, wait and verify the balance was updated
      if (response.ok) {
        await waitForTransaction(8000); // ~ 2 slots
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
            // delay should random from 100 to 800
            const delay = Math.floor(Math.random() * (1200 - 100 + 1)) + 400; // Random delay between 400ms and 1200ms
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